// Package store contains the small storage experiments used while studying
// Database Internals.
package store

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

const (
	walFileName      = "store.wal"
	snapshotFileName = "store.snapshot"
	maxRecordSize    = 64 << 20

	opSet    = byte(1)
	opDelete = byte(2)
)

var (
	// ErrNotFound indicates that a key does not exist in the store.
	ErrNotFound = errors.New("store: key not found")
	// ErrClosed indicates that the store has already been closed.
	ErrClosed = errors.New("store: closed")
	// ErrInvalidKey indicates that a key is empty.
	ErrInvalidKey = errors.New("store: key must not be empty")
)

// Store is an in-memory key/value store backed by a write-ahead log and a
// checkpoint snapshot.
//
// Set and Delete append and sync a log record before changing the in-memory
// map. Checkpoint writes a complete snapshot first, then truncates the log.
// This ordering makes recovery safe if a process crashes between those steps.
type Store struct {
	mu sync.RWMutex

	data map[string][]byte
	wal  *os.File
	dir  string

	closed bool
}

// Open opens or creates a store in dir. The directory contains the sequential
// write-ahead log (store.wal) and the latest checkpoint (store.snapshot).
func Open(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("store: directory must not be empty")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create store directory: %w", err)
	}

	data := make(map[string][]byte)
	snapshotPath := filepath.Join(dir, snapshotFileName)
	if err := loadSnapshot(snapshotPath, data); err != nil {
		return nil, err
	}

	walPath := filepath.Join(dir, walFileName)
	wal, err := os.OpenFile(walPath, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open write-ahead log: %w", err)
	}
	if err := replayWAL(wal, data); err != nil {
		_ = wal.Close()
		return nil, err
	}

	return &Store{data: data, wal: wal, dir: dir}, nil
}

// Set stores a copy of value under key. The operation is durable when Set
// returns successfully.
func (s *Store) Set(key string, value []byte) error {
	if key == "" {
		return ErrInvalidKey
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}

	if err := s.appendRecord(opSet, key, value); err != nil {
		return err
	}
	s.data[key] = append([]byte(nil), value...)
	return nil
}

// Get returns a copy of the value stored under key.
func (s *Store) Get(key string) ([]byte, error) {
	if key == "" {
		return nil, ErrInvalidKey
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, ErrClosed
	}

	value, ok := s.data[key]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), value...), nil
}

// Delete removes key. The deletion is durable when Delete returns
// successfully. Deleting a missing key is a durable no-op.
func (s *Store) Delete(key string) error {
	if key == "" {
		return ErrInvalidKey
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}

	if err := s.appendRecord(opDelete, key, nil); err != nil {
		return err
	}
	delete(s.data, key)
	return nil
}

// Checkpoint writes the current in-memory state to a new snapshot and then
// removes WAL records covered by that snapshot. It holds the store lock for
// the whole operation, so the snapshot and the log boundary describe one
// consistent state.
func (s *Store) Checkpoint() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}

	snapshotPath := filepath.Join(s.dir, snapshotFileName)
	if err := writeSnapshot(snapshotPath, s.data); err != nil {
		return err
	}

	if err := s.wal.Truncate(0); err != nil {
		return fmt.Errorf("truncate write-ahead log: %w", err)
	}
	if _, err := s.wal.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("seek write-ahead log: %w", err)
	}
	if err := s.wal.Sync(); err != nil {
		return fmt.Errorf("sync truncated write-ahead log: %w", err)
	}
	return nil
}

// Close syncs and closes the write-ahead log. A successful Set or Delete has
// already been synced, but Close still reports a final sync or close failure.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}

	s.closed = true
	syncErr := s.wal.Sync()
	closeErr := s.wal.Close()
	if syncErr != nil {
		return fmt.Errorf("sync write-ahead log: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close write-ahead log: %w", closeErr)
	}
	return nil
}

func (s *Store) appendRecord(operation byte, key string, value []byte) error {
	if uint64(len(key)) > uint64(^uint32(0)) || uint64(len(value)) > uint64(^uint32(0)) {
		return errors.New("store: key or value is too large")
	}
	payloadSize := 1 + 4 + 4 + len(key) + len(value)
	if payloadSize > maxRecordSize {
		return errors.New("store: record is too large")
	}

	record := make([]byte, 4+payloadSize)
	binary.BigEndian.PutUint32(record[:4], uint32(payloadSize))
	record[4] = operation
	binary.BigEndian.PutUint32(record[5:9], uint32(len(key)))
	binary.BigEndian.PutUint32(record[9:13], uint32(len(value)))
	copy(record[13:], key)
	copy(record[13+len(key):], value)

	if err := writeFull(s.wal, record); err != nil {
		return fmt.Errorf("append write-ahead log: %w", err)
	}
	if err := s.wal.Sync(); err != nil {
		return fmt.Errorf("sync write-ahead log: %w", err)
	}
	return nil
}

func writeFull(file *os.File, data []byte) error {
	for len(data) > 0 {
		n, err := file.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

func replayWAL(file *os.File, data map[string][]byte) error {
	contents, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("read write-ahead log: %w", err)
	}

	for offset := 0; offset < len(contents); {
		remaining := len(contents) - offset
		if remaining < 4 {
			// A crash can leave a partial record header at the end.
			break
		}
		payloadSize := int(binary.BigEndian.Uint32(contents[offset : offset+4]))
		if payloadSize > maxRecordSize {
			return fmt.Errorf("corrupt write-ahead log: record at offset %d is too large", offset)
		}
		if remaining-4 < payloadSize {
			// Ignore a torn final record; all complete records are durable.
			break
		}

		payload := contents[offset+4 : offset+4+payloadSize]
		if err := applyRecord(payload, data); err != nil {
			return fmt.Errorf("corrupt write-ahead log at offset %d: %w", offset, err)
		}
		offset += 4 + payloadSize
	}
	return nil
}

func applyRecord(payload []byte, data map[string][]byte) error {
	if len(payload) < 9 {
		return errors.New("record is too short")
	}

	operation := payload[0]
	keySize := int(binary.BigEndian.Uint32(payload[1:5]))
	valueSize := int(binary.BigEndian.Uint32(payload[5:9]))
	if keySize == 0 {
		return errors.New("record has an empty key")
	}
	if keySize > len(payload)-9 || valueSize > len(payload)-9-keySize || 9+keySize+valueSize != len(payload) {
		return errors.New("record has invalid key or value length")
	}

	key := string(payload[9 : 9+keySize])
	switch operation {
	case opSet:
		data[key] = append([]byte(nil), payload[9+keySize:]...)
	case opDelete:
		if valueSize != 0 {
			return errors.New("delete record has a value")
		}
		delete(data, key)
	default:
		return fmt.Errorf("unknown operation %d", operation)
	}
	return nil
}

func loadSnapshot(path string, data map[string][]byte) error {
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read snapshot: %w", err)
	}
	if len(contents) < 20 || string(contents[:8]) != "LDBSNAP1" || binary.BigEndian.Uint32(contents[8:12]) != 1 {
		return errors.New("corrupt snapshot: invalid header")
	}

	count := binary.BigEndian.Uint64(contents[12:20])
	offset := 20
	for i := uint64(0); i < count; i++ {
		if len(contents)-offset < 8 {
			return errors.New("corrupt snapshot: truncated entry header")
		}
		keySize := int(binary.BigEndian.Uint32(contents[offset : offset+4]))
		valueSize := int(binary.BigEndian.Uint32(contents[offset+4 : offset+8]))
		offset += 8
		if keySize == 0 || keySize > len(contents)-offset || valueSize > len(contents)-offset-keySize {
			return errors.New("corrupt snapshot: invalid key or value length")
		}
		key := string(contents[offset : offset+keySize])
		offset += keySize
		data[key] = append([]byte(nil), contents[offset:offset+valueSize]...)
		offset += valueSize
	}
	if offset != len(contents) {
		return errors.New("corrupt snapshot: trailing data")
	}
	return nil
}

func writeSnapshot(path string, data map[string][]byte) error {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	temporaryPath := path + ".tmp"
	file, err := os.OpenFile(temporaryPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create snapshot: %w", err)
	}
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	header := make([]byte, 20)
	copy(header[:8], "LDBSNAP1")
	binary.BigEndian.PutUint32(header[8:12], 1)
	binary.BigEndian.PutUint64(header[12:20], uint64(len(keys)))
	if err := writeFull(file, header); err != nil {
		_ = file.Close()
		return fmt.Errorf("write snapshot header: %w", err)
	}

	for _, key := range keys {
		value := data[key]
		if uint64(len(key)) > uint64(^uint32(0)) || uint64(len(value)) > uint64(^uint32(0)) {
			_ = file.Close()
			return errors.New("store: key or value is too large")
		}
		entryHeader := make([]byte, 8)
		binary.BigEndian.PutUint32(entryHeader[:4], uint32(len(key)))
		binary.BigEndian.PutUint32(entryHeader[4:8], uint32(len(value)))
		if err := writeFull(file, entryHeader); err != nil {
			_ = file.Close()
			return fmt.Errorf("write snapshot entry: %w", err)
		}
		if err := writeFull(file, []byte(key)); err != nil {
			_ = file.Close()
			return fmt.Errorf("write snapshot key: %w", err)
		}
		if err := writeFull(file, value); err != nil {
			_ = file.Close()
			return fmt.Errorf("write snapshot value: %w", err)
		}
	}

	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync snapshot: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close snapshot: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("install snapshot: %w", err)
	}
	removeTemporary = false
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open store directory: %w", err)
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return fmt.Errorf("sync store directory: %w", err)
	}
	if err := directory.Close(); err != nil {
		return fmt.Errorf("close store directory: %w", err)
	}
	return nil
}

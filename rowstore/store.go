// Package rowstore contains a small row-oriented storage experiment.
package rowstore

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

var (
	// ErrInvalidSchema indicates that a schema cannot describe a row store.
	ErrInvalidSchema = errors.New("rowstore: invalid schema")
	// ErrInvalidRow indicates that a row does not match the store schema.
	ErrInvalidRow = errors.New("rowstore: invalid row")
	// ErrDuplicatePrimaryKey indicates that an inserted row has an existing primary key.
	ErrDuplicatePrimaryKey = errors.New("rowstore: duplicate primary key")
	// ErrNotFound indicates that no row exists for a requested primary key.
	ErrNotFound = errors.New("rowstore: row not found")
	// ErrUnknownIndex indicates that a secondary index does not exist.
	ErrUnknownIndex = errors.New("rowstore: unknown secondary index")
)

// Row is one logical record. This first experiment uses string values to keep
// the storage and index mechanics in focus.
type Row map[string]string

// Schema describes a fixed row layout. Each secondary-index name is also the
// name of the column it indexes.
type Schema struct {
	PrimaryKey       string
	Columns          []string
	SecondaryIndexes []string
}

// Store holds rows separately from its indexes. A secondary index maps its
// value to primary keys, never directly to row storage. Lookups therefore use
// primary-key indirection:
//
// secondary value -> primary key -> primary index -> row
//
// That extra primary-index lookup lets rows move without rewriting every
// secondary-index entry that refers to them.
type Store struct {
	mu sync.RWMutex

	schema Schema

	nextRecordID uint64
	rows         map[uint64]Row
	primaryIndex map[string]uint64
	secondary    map[string]map[string]map[string]struct{}
}

// New creates an empty row store with a fixed schema.
func New(schema Schema) (*Store, error) {
	if err := validateSchema(schema); err != nil {
		return nil, err
	}

	secondary := make(map[string]map[string]map[string]struct{}, len(schema.SecondaryIndexes))
	for _, index := range schema.SecondaryIndexes {
		secondary[index] = make(map[string]map[string]struct{})
	}

	return &Store{
		schema:       copySchema(schema),
		rows:         make(map[uint64]Row),
		primaryIndex: make(map[string]uint64),
		secondary:    secondary,
	}, nil
}

// Insert adds row. The primary-key value must be unique.
func (s *Store) Insert(row Row) error {
	if err := s.validateRow(row); err != nil {
		return err
	}

	primaryKey := row[s.schema.PrimaryKey]
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.primaryIndex[primaryKey]; exists {
		return ErrDuplicatePrimaryKey
	}

	s.nextRecordID++
	recordID := s.nextRecordID
	storedRow := copyRow(row)
	s.rows[recordID] = storedRow
	s.primaryIndex[primaryKey] = recordID
	s.addSecondaryEntries(primaryKey, storedRow)
	return nil
}

// Get looks up a row through its primary index and returns a copy.
func (s *Store) Get(primaryKey string) (Row, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.get(primaryKey)
}

// Find looks up rows through a named secondary index. The index returns
// primary keys; each key is resolved through the primary index before its row
// is read. Results are ordered by primary key for deterministic callers.
func (s *Store) Find(indexName, value string) ([]Row, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	index, ok := s.secondary[indexName]
	if !ok {
		return nil, ErrUnknownIndex
	}

	primaryKeys := make([]string, 0, len(index[value]))
	for primaryKey := range index[value] {
		primaryKeys = append(primaryKeys, primaryKey)
	}
	sort.Strings(primaryKeys)

	rows := make([]Row, 0, len(primaryKeys))
	for _, primaryKey := range primaryKeys {
		row, err := s.get(primaryKey)
		if err != nil {
			return nil, fmt.Errorf("resolve secondary index %q: %w", indexName, err)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// Update replaces an existing row and updates secondary-index entries. The
// primary key identifies the row and cannot be changed by this API.
func (s *Store) Update(row Row) error {
	if err := s.validateRow(row); err != nil {
		return err
	}

	primaryKey := row[s.schema.PrimaryKey]
	s.mu.Lock()
	defer s.mu.Unlock()

	recordID, ok := s.primaryIndex[primaryKey]
	if !ok {
		return ErrNotFound
	}
	previous := s.rows[recordID]
	s.removeSecondaryEntries(primaryKey, previous)
	storedRow := copyRow(row)
	s.rows[recordID] = storedRow
	s.addSecondaryEntries(primaryKey, storedRow)
	return nil
}

// Delete removes a row and all of its secondary-index entries.
func (s *Store) Delete(primaryKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	recordID, ok := s.primaryIndex[primaryKey]
	if !ok {
		return ErrNotFound
	}
	row := s.rows[recordID]
	s.removeSecondaryEntries(primaryKey, row)
	delete(s.primaryIndex, primaryKey)
	delete(s.rows, recordID)
	return nil
}

func (s *Store) get(primaryKey string) (Row, error) {
	recordID, ok := s.primaryIndex[primaryKey]
	if !ok {
		return nil, ErrNotFound
	}
	row, ok := s.rows[recordID]
	if !ok {
		return nil, fmt.Errorf("primary index for %q references a missing row", primaryKey)
	}
	return copyRow(row), nil
}

func (s *Store) addSecondaryEntries(primaryKey string, row Row) {
	for _, indexName := range s.schema.SecondaryIndexes {
		value := row[indexName]
		primaryKeys := s.secondary[indexName][value]
		if primaryKeys == nil {
			primaryKeys = make(map[string]struct{})
			s.secondary[indexName][value] = primaryKeys
		}
		primaryKeys[primaryKey] = struct{}{}
	}
}

func (s *Store) removeSecondaryEntries(primaryKey string, row Row) {
	for _, indexName := range s.schema.SecondaryIndexes {
		value := row[indexName]
		primaryKeys := s.secondary[indexName][value]
		delete(primaryKeys, primaryKey)
		if len(primaryKeys) == 0 {
			delete(s.secondary[indexName], value)
		}
	}
}

func (s *Store) validateRow(row Row) error {
	if len(row) != len(s.schema.Columns) {
		return ErrInvalidRow
	}
	for _, column := range s.schema.Columns {
		if _, ok := row[column]; !ok {
			return ErrInvalidRow
		}
	}
	if row[s.schema.PrimaryKey] == "" {
		return ErrInvalidRow
	}
	return nil
}

func validateSchema(schema Schema) error {
	if schema.PrimaryKey == "" || len(schema.Columns) == 0 {
		return ErrInvalidSchema
	}

	columns := make(map[string]struct{}, len(schema.Columns))
	for _, column := range schema.Columns {
		if column == "" {
			return ErrInvalidSchema
		}
		if _, exists := columns[column]; exists {
			return ErrInvalidSchema
		}
		columns[column] = struct{}{}
	}
	if _, exists := columns[schema.PrimaryKey]; !exists {
		return ErrInvalidSchema
	}

	indexes := make(map[string]struct{}, len(schema.SecondaryIndexes))
	for _, index := range schema.SecondaryIndexes {
		if index == "" || index == schema.PrimaryKey {
			return ErrInvalidSchema
		}
		if _, exists := columns[index]; !exists {
			return ErrInvalidSchema
		}
		if _, exists := indexes[index]; exists {
			return ErrInvalidSchema
		}
		indexes[index] = struct{}{}
	}
	return nil
}

func copySchema(schema Schema) Schema {
	return Schema{
		PrimaryKey:       schema.PrimaryKey,
		Columns:          append([]string(nil), schema.Columns...),
		SecondaryIndexes: append([]string(nil), schema.SecondaryIndexes...),
	}
}

func copyRow(row Row) Row {
	copy := make(Row, len(row))
	for column, value := range row {
		copy[column] = value
	}
	return copy
}

package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSetGetAndDelete(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	value := []byte("value")
	if err := db.Set("key", value); err != nil {
		t.Fatal(err)
	}
	value[0] = 'X'

	got, err := db.Get("key")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "value" {
		t.Fatalf("Get(key) = %q, want %q", got, "value")
	}
	got[0] = 'X'
	got, err = db.Get("key")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "value" {
		t.Fatalf("Get(key) after mutating returned value = %q, want %q", got, "value")
	}

	if err := db.Delete("key"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Get("key"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(deleted key) error = %v, want ErrNotFound", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOperationsSurviveReopen(t *testing.T) {
	dir := t.TempDir()
	db := openTestStore(t, dir)
	if err := db.Set("one", []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := db.Set("two", []byte("second")); err != nil {
		t.Fatal(err)
	}
	if err := db.Delete("one"); err != nil {
		t.Fatal(err)
	}
	closeTestStore(t, db)

	db = openTestStore(t, dir)
	defer closeTestStore(t, db)
	if _, err := db.Get("one"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(one) error = %v, want ErrNotFound", err)
	}
	got, err := db.Get("two")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second" {
		t.Fatalf("Get(two) = %q, want %q", got, "second")
	}
}

func TestCheckpointWritesSnapshotAndClearsWAL(t *testing.T) {
	dir := t.TempDir()
	db := openTestStore(t, dir)
	if err := db.Set("name", []byte("Ada")); err != nil {
		t.Fatal(err)
	}
	if err := db.Set("language", []byte("Go")); err != nil {
		t.Fatal(err)
	}
	if err := db.Checkpoint(); err != nil {
		t.Fatal(err)
	}

	walInfo, err := os.Stat(filepath.Join(dir, walFileName))
	if err != nil {
		t.Fatal(err)
	}
	if walInfo.Size() != 0 {
		t.Fatalf("WAL size after checkpoint = %d, want 0", walInfo.Size())
	}
	if _, err := os.Stat(filepath.Join(dir, snapshotFileName)); err != nil {
		t.Fatalf("snapshot missing after checkpoint: %v", err)
	}
	closeTestStore(t, db)

	db = openTestStore(t, dir)
	defer closeTestStore(t, db)
	for key, want := range map[string]string{"name": "Ada", "language": "Go"} {
		got, err := db.Get(key)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("Get(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestCheckpointDoesNotLoseLaterWALOperations(t *testing.T) {
	dir := t.TempDir()
	db := openTestStore(t, dir)
	if err := db.Set("before", []byte("snapshot")); err != nil {
		t.Fatal(err)
	}
	if err := db.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	if err := db.Set("after", []byte("log")); err != nil {
		t.Fatal(err)
	}
	closeTestStore(t, db)

	db = openTestStore(t, dir)
	defer closeTestStore(t, db)
	for key, want := range map[string]string{"before": "snapshot", "after": "log"} {
		got, err := db.Get(key)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("Get(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestRecoveryIgnoresTornFinalWALRecord(t *testing.T) {
	dir := t.TempDir()
	db := openTestStore(t, dir)
	if err := db.Set("complete", []byte("yes")); err != nil {
		t.Fatal(err)
	}
	closeTestStore(t, db)

	wal, err := os.OpenFile(filepath.Join(dir, walFileName), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wal.Write([]byte{0, 0, 0, 20, opSet}); err != nil {
		t.Fatal(err)
	}
	if err := wal.Close(); err != nil {
		t.Fatal(err)
	}

	db = openTestStore(t, dir)
	defer closeTestStore(t, db)
	got, err := db.Get("complete")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "yes" {
		t.Fatalf("Get(complete) = %q, want %q", got, "yes")
	}
}

func TestInvalidKeysAndClosedStore(t *testing.T) {
	db := openTestStore(t, t.TempDir())
	if err := db.Set("", []byte("value")); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("Set(empty key) error = %v, want ErrInvalidKey", err)
	}
	if _, err := db.Get(""); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("Get(empty key) error = %v, want ErrInvalidKey", err)
	}
	if err := db.Delete(""); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("Delete(empty key) error = %v, want ErrInvalidKey", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Set("key", []byte("value")); !errors.Is(err, ErrClosed) {
		t.Fatalf("Set after close error = %v, want ErrClosed", err)
	}
	if _, err := db.Get("key"); !errors.Is(err, ErrClosed) {
		t.Fatalf("Get after close error = %v, want ErrClosed", err)
	}
	if err := db.Delete("key"); !errors.Is(err, ErrClosed) {
		t.Fatalf("Delete after close error = %v, want ErrClosed", err)
	}
	if err := db.Checkpoint(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Checkpoint after close error = %v, want ErrClosed", err)
	}
	if err := db.Close(); !errors.Is(err, ErrClosed) {
		t.Fatalf("second Close error = %v, want ErrClosed", err)
	}
}

func openTestStore(t *testing.T, dir string) *Store {
	t.Helper()
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func closeTestStore(t *testing.T, db *Store) {
	t.Helper()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

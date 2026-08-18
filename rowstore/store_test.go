package rowstore

import (
	"errors"
	"reflect"
	"testing"
)

func TestInsertFindAndGetUsePrimaryKeyIndirection(t *testing.T) {
	store := newTestStore(t)
	ada := Row{"id": "1", "email": "ada@example.com", "name": "Ada"}
	grace := Row{"id": "2", "email": "ada@example.com", "name": "Grace"}
	if err := store.Insert(ada); err != nil {
		t.Fatal(err)
	}
	if err := store.Insert(grace); err != nil {
		t.Fatal(err)
	}

	// The secondary index contains the logical primary key, not a direct row location.
	if _, ok := store.secondary["email"]["ada@example.com"]["1"]; !ok {
		t.Fatal("secondary index does not contain Ada's primary key")
	}
	if _, ok := store.primaryIndex["1"]; !ok {
		t.Fatal("primary index does not resolve Ada's primary key")
	}

	got, err := store.Find("email", "ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	want := []Row{
		{"id": "1", "email": "ada@example.com", "name": "Ada"},
		{"id": "2", "email": "ada@example.com", "name": "Grace"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Find(email, ada@example.com) = %#v, want %#v", got, want)
	}

	byPrimaryKey, err := store.Get("1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(byPrimaryKey, want[0]) {
		t.Fatalf("Get(1) = %#v, want %#v", byPrimaryKey, want[0])
	}
}

func TestUpdateMaintainsSecondaryIndex(t *testing.T) {
	store := newTestStore(t)
	if err := store.Insert(Row{"id": "1", "email": "old@example.com", "name": "Ada"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(Row{"id": "1", "email": "new@example.com", "name": "Ada Lovelace"}); err != nil {
		t.Fatal(err)
	}

	oldRows, err := store.Find("email", "old@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(oldRows) != 0 {
		t.Fatalf("Find on old value = %#v, want no rows", oldRows)
	}
	newRows, err := store.Find("email", "new@example.com")
	if err != nil {
		t.Fatal(err)
	}
	want := []Row{{"id": "1", "email": "new@example.com", "name": "Ada Lovelace"}}
	if !reflect.DeepEqual(newRows, want) {
		t.Fatalf("Find on new value = %#v, want %#v", newRows, want)
	}
}

func TestDeleteRemovesSecondaryIndexEntry(t *testing.T) {
	store := newTestStore(t)
	if err := store.Insert(Row{"id": "1", "email": "ada@example.com", "name": "Ada"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("1"); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Get("1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(deleted row) error = %v, want ErrNotFound", err)
	}
	rows, err := store.Find("email", "ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("Find after Delete = %#v, want no rows", rows)
	}
}

func TestRejectsInvalidSchemaAndRows(t *testing.T) {
	if _, err := New(Schema{}); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("New(empty schema) error = %v, want ErrInvalidSchema", err)
	}
	if _, err := New(Schema{
		PrimaryKey:       "id",
		Columns:          []string{"id", "email"},
		SecondaryIndexes: []string{"missing"},
	}); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("New(index for missing column) error = %v, want ErrInvalidSchema", err)
	}

	store := newTestStore(t)
	if err := store.Insert(Row{"email": "ada@example.com", "name": "Ada"}); !errors.Is(err, ErrInvalidRow) {
		t.Fatalf("Insert(row without primary key) error = %v, want ErrInvalidRow", err)
	}
	if err := store.Insert(Row{"id": "1", "email": "ada@example.com", "extra": "value"}); !errors.Is(err, ErrInvalidRow) {
		t.Fatalf("Insert(row with unknown column) error = %v, want ErrInvalidRow", err)
	}
	if err := store.Insert(Row{"id": "1", "email": "ada@example.com", "name": "Ada"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Insert(Row{"id": "1", "email": "grace@example.com", "name": "Grace"}); !errors.Is(err, ErrDuplicatePrimaryKey) {
		t.Fatalf("Insert(duplicate primary key) error = %v, want ErrDuplicatePrimaryKey", err)
	}
	if err := store.Update(Row{"id": "2", "email": "grace@example.com", "name": "Grace"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update(missing row) error = %v, want ErrNotFound", err)
	}
	if _, err := store.Find("unknown", "value"); !errors.Is(err, ErrUnknownIndex) {
		t.Fatalf("Find(unknown index) error = %v, want ErrUnknownIndex", err)
	}
}

func TestReturnedRowsAreCopies(t *testing.T) {
	store := newTestStore(t)
	row := Row{"id": "1", "email": "ada@example.com", "name": "Ada"}
	if err := store.Insert(row); err != nil {
		t.Fatal(err)
	}
	row["name"] = "Changed"

	got, err := store.Get("1")
	if err != nil {
		t.Fatal(err)
	}
	got["name"] = "Changed again"
	stored, err := store.Get("1")
	if err != nil {
		t.Fatal(err)
	}
	if stored["name"] != "Ada" {
		t.Fatalf("stored row name = %q, want %q", stored["name"], "Ada")
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := New(Schema{
		PrimaryKey:       "id",
		Columns:          []string{"id", "email", "name"},
		SecondaryIndexes: []string{"email"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

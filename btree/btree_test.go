package btree

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"sync"
	"testing"
)

func TestNewRejectsInvalidMinimumDegree(t *testing.T) {
	if _, err := New(1); !errors.Is(err, ErrInvalidMinimumDegree) {
		t.Fatalf("New(1) error = %v, want ErrInvalidMinimumDegree", err)
	}
}

func TestPutGetSplitAndReplace(t *testing.T) {
	tree := newTestTree(t)
	for _, key := range []string{"delta", "bravo", "echo", "alpha", "charlie", "foxtrot", "golf"} {
		if err := tree.Put(key, []byte("value-"+key)); err != nil {
			t.Fatalf("Put(%q): %v", key, err)
		}
	}
	if tree.root.leaf {
		t.Fatal("root is a leaf after inserts that should require a split")
	}
	if got, want := tree.Len(), 7; got != want {
		t.Fatalf("Len() = %d, want %d", got, want)
	}

	for _, key := range []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf"} {
		got, err := tree.Get(key)
		if err != nil {
			t.Fatalf("Get(%q): %v", key, err)
		}
		if want := "value-" + key; string(got) != want {
			t.Errorf("Get(%q) = %q, want %q", key, got, want)
		}
	}

	value := []byte("replacement")
	if err := tree.Put("delta", value); err != nil {
		t.Fatal(err)
	}
	value[0] = 'X'
	got, err := tree.Get("delta")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "replacement" {
		t.Fatalf("Get(delta) after replacement = %q, want replacement", got)
	}
	got[0] = 'X'
	got, err = tree.Get("delta")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "replacement" {
		t.Fatalf("Get(delta) after mutating returned value = %q, want replacement", got)
	}
	if got, want := tree.Len(), 7; got != want {
		t.Fatalf("Len() after replacement = %d, want %d", got, want)
	}
}

func TestRangeReturnsOrderedCopiesWithHalfOpenBounds(t *testing.T) {
	tree := newTestTree(t)
	for _, key := range []string{"delta", "bravo", "echo", "alpha", "charlie", "foxtrot"} {
		if err := tree.Put(key, []byte(key)); err != nil {
			t.Fatal(err)
		}
	}

	got := tree.Range("bravo", "foxtrot")
	want := []Entry{
		{Key: "bravo", Value: []byte("bravo")},
		{Key: "charlie", Value: []byte("charlie")},
		{Key: "delta", Value: []byte("delta")},
		{Key: "echo", Value: []byte("echo")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Range(bravo, foxtrot) = %#v, want %#v", got, want)
	}
	got[0].Value[0] = 'X'
	if result := tree.Range("bravo", "charlie"); string(result[0].Value) != "bravo" {
		t.Fatalf("Range returned an alias of stored data: %q", result[0].Value)
	}

	beforeCharlie := []Entry{
		{Key: "alpha", Value: []byte("alpha")},
		{Key: "bravo", Value: []byte("bravo")},
	}
	if got := tree.Range("", "charlie"); !reflect.DeepEqual(got, beforeCharlie) {
		t.Fatalf("Range(unbounded, charlie) = %#v, want %#v", got, beforeCharlie)
	}
	fromEcho := []Entry{
		{Key: "echo", Value: []byte("echo")},
		{Key: "foxtrot", Value: []byte("foxtrot")},
	}
	if got := tree.Range("echo", ""); !reflect.DeepEqual(got, fromEcho) {
		t.Fatalf("Range(echo, unbounded) = %#v, want %#v", got, fromEcho)
	}
	if got := tree.Range("foxtrot", "bravo"); len(got) != 0 {
		t.Fatalf("Range with reversed bounds = %#v, want empty", got)
	}
}

func TestDeleteRebalancesAndPreservesOtherKeys(t *testing.T) {
	tree := newTestTree(t)
	keys := []string{"01", "02", "03", "04", "05", "06", "07", "08", "09", "10", "11", "12"}
	for _, key := range keys {
		if err := tree.Put(key, []byte(key)); err != nil {
			t.Fatal(err)
		}
	}

	for _, key := range []string{"01", "03", "05", "07", "09", "11", "02", "04", "06", "08", "10", "12"} {
		if err := tree.Delete(key); err != nil {
			t.Fatalf("Delete(%q): %v", key, err)
		}
		if _, err := tree.Get(key); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get(%q) after Delete error = %v, want ErrNotFound", key, err)
		}
	}
	if got := tree.Len(); got != 0 {
		t.Fatalf("Len() after deleting all keys = %d, want 0", got)
	}
	if !tree.root.leaf || len(tree.root.keys) != 0 {
		t.Fatalf("root after deleting all keys = %#v, want an empty leaf", tree.root)
	}
	if err := tree.Delete("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete(missing) error = %v, want ErrNotFound", err)
	}
}

func TestDeleteMissingKeyDoesNotModifyTree(t *testing.T) {
	tree := newTestTree(t)
	for _, key := range []string{"01", "02", "03", "04"} {
		if err := tree.Put(key, []byte(key)); err != nil {
			t.Fatal(err)
		}
	}
	before := tree.Range("", "")
	if err := tree.Delete("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete(missing) error = %v, want ErrNotFound", err)
	}
	if got := tree.Range("", ""); !reflect.DeepEqual(got, before) {
		t.Fatalf("tree after Delete(missing) = %#v, want %#v", got, before)
	}
	assertInvariants(t, tree)
}

func TestConcurrentReadsAndWrites(t *testing.T) {
	tree := newTestTree(t)
	const keyCount = 100

	start := make(chan struct{})
	var writersAndReaders sync.WaitGroup
	writersAndReaders.Add(5)
	for reader := 0; reader < 4; reader++ {
		go func() {
			defer writersAndReaders.Done()
			<-start
			for index := 0; index < keyCount; index++ {
				key := fmt.Sprintf("%03d", index)
				if _, err := tree.Get(key); err != nil && !errors.Is(err, ErrNotFound) {
					t.Errorf("Get(%q): %v", key, err)
				}
				_ = tree.Range("020", "080")
			}
		}()
	}
	go func() {
		defer writersAndReaders.Done()
		<-start
		for index := 0; index < keyCount; index++ {
			key := fmt.Sprintf("%03d", index)
			if err := tree.Put(key, []byte(key)); err != nil {
				t.Errorf("Put(%q): %v", key, err)
			}
		}
	}()
	close(start)
	writersAndReaders.Wait()

	if got := tree.Len(); got != keyCount {
		t.Fatalf("Len() = %d, want %d", got, keyCount)
	}
	assertInvariants(t, tree)
}

func TestRandomInsertsAndDeletesMaintainBTreeInvariants(t *testing.T) {
	tree := newTestTree(t)
	keys := make([]string, 100)
	for index := range keys {
		keys[index] = fmt.Sprintf("%03d", index)
	}

	random := rand.New(rand.NewPCG(1, 2))
	random.Shuffle(len(keys), func(i, j int) { keys[i], keys[j] = keys[j], keys[i] })
	for _, key := range keys {
		if err := tree.Put(key, []byte("value-"+key)); err != nil {
			t.Fatal(err)
		}
	}
	assertInvariants(t, tree)

	random.Shuffle(len(keys), func(i, j int) { keys[i], keys[j] = keys[j], keys[i] })
	for _, key := range keys {
		if err := tree.Delete(key); err != nil {
			t.Fatalf("Delete(%q): %v", key, err)
		}
		assertInvariants(t, tree)
	}
}

func TestRejectsEmptyKeys(t *testing.T) {
	tree := newTestTree(t)
	if err := tree.Put("", []byte("value")); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("Put(empty) error = %v, want ErrInvalidKey", err)
	}
	if _, err := tree.Get(""); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("Get(empty) error = %v, want ErrInvalidKey", err)
	}
	if err := tree.Delete(""); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("Delete(empty) error = %v, want ErrInvalidKey", err)
	}
}

func assertInvariants(t *testing.T, tree *Tree) {
	t.Helper()
	tree.mu.RLock()
	defer tree.mu.RUnlock()

	leafDepth := -1
	count := assertNodeInvariants(t, tree, tree.root, "", "", 0, &leafDepth)
	if count != tree.length {
		t.Fatalf("number of keys in nodes = %d, tree length = %d", count, tree.length)
	}
}

func assertNodeInvariants(t *testing.T, tree *Tree, current *node, lower, upper string, depth int, leafDepth *int) int {
	t.Helper()
	if current != tree.root && (len(current.keys) < tree.minimumDegree-1 || len(current.keys) > tree.maxKeys()) {
		t.Fatalf("node at depth %d has %d keys, want [%d, %d]", depth, len(current.keys), tree.minimumDegree-1, tree.maxKeys())
	}
	if current == tree.root && len(current.keys) > tree.maxKeys() {
		t.Fatalf("root has %d keys, want at most %d", len(current.keys), tree.maxKeys())
	}
	for index, item := range current.keys {
		if index > 0 && current.keys[index-1].key >= item.key {
			t.Fatalf("node keys are not strictly ordered: %q then %q", current.keys[index-1].key, item.key)
		}
		if lower != "" && item.key <= lower {
			t.Fatalf("key %q is not above lower bound %q", item.key, lower)
		}
		if upper != "" && item.key >= upper {
			t.Fatalf("key %q is not below upper bound %q", item.key, upper)
		}
	}
	if current.leaf {
		if len(current.children) != 0 {
			t.Fatalf("leaf at depth %d has children", depth)
		}
		if *leafDepth == -1 {
			*leafDepth = depth
		} else if *leafDepth != depth {
			t.Fatalf("leaf depth = %d, want %d", depth, *leafDepth)
		}
		return len(current.keys)
	}
	if got, want := len(current.children), len(current.keys)+1; got != want {
		t.Fatalf("internal node at depth %d has %d children, want %d", depth, got, want)
	}

	count := len(current.keys)
	for index, child := range current.children {
		childLower, childUpper := lower, upper
		if index > 0 {
			childLower = current.keys[index-1].key
		}
		if index < len(current.keys) {
			childUpper = current.keys[index].key
		}
		count += assertNodeInvariants(t, tree, child, childLower, childUpper, depth+1, leafDepth)
	}
	return count
}

func newTestTree(t *testing.T) *Tree {
	t.Helper()
	tree, err := New(2)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// Package btree implements an in-memory B-Tree for studying ordered storage
// structures.
package btree

import (
	"errors"
	"sort"
	"sync"
)

var (
	// ErrInvalidMinimumDegree indicates that a B-Tree degree is less than two.
	ErrInvalidMinimumDegree = errors.New("btree: minimum degree must be at least 2")
	// ErrInvalidKey indicates that a key is empty.
	ErrInvalidKey = errors.New("btree: key must not be empty")
	// ErrNotFound indicates that a key is absent from the tree.
	ErrNotFound = errors.New("btree: key not found")
)

// Entry is one key/value pair returned by Range. Values are copies and may be
// changed by the caller.
type Entry struct {
	Key   string
	Value []byte
}

type entry struct {
	key   string
	value []byte
}

type node struct {
	leaf     bool
	keys     []entry
	children []*node
}

// Tree is an in-memory B-Tree whose keys are ordered lexicographically. Its
// minimum degree controls its size: every non-root node has between t-1 and
// 2t-1 keys, where t is the minimum degree.
//
// Tree is safe for concurrent readers and writers. It intentionally has no
// persistence, page layout, or recovery layer.
type Tree struct {
	mu sync.RWMutex

	minimumDegree int
	root          *node
	length        int
}

// New creates an empty B-Tree with minimumDegree. The degree must be at least
// two; degree two is the smallest valid B-Tree and is useful for demonstrating
// splits and merges in small tests.
func New(minimumDegree int) (*Tree, error) {
	if minimumDegree < 2 {
		return nil, ErrInvalidMinimumDegree
	}
	return &Tree{
		minimumDegree: minimumDegree,
		root:          &node{leaf: true},
	}, nil
}

// Len returns the number of keys stored in the tree.
func (t *Tree) Len() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.length
}

// Put adds key with a copy of value, or replaces the existing value for key.
func (t *Tree) Put(key string, value []byte) error {
	if key == "" {
		return ErrInvalidKey
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if existing, index, found := find(t.root, key); found {
		existing.keys[index].value = copyValue(value)
		return nil
	}

	if len(t.root.keys) == t.maxKeys() {
		oldRoot := t.root
		t.root = &node{children: []*node{oldRoot}}
		t.splitChild(t.root, 0)
	}
	t.insertNonFull(t.root, entry{key: key, value: copyValue(value)})
	t.length++
	return nil
}

// Get returns a copy of the value stored under key.
func (t *Tree) Get(key string) ([]byte, error) {
	if key == "" {
		return nil, ErrInvalidKey
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	found, index, ok := find(t.root, key)
	if !ok {
		return nil, ErrNotFound
	}
	return copyValue(found.keys[index].value), nil
}

// Delete removes key from the tree. It rebalances nodes by borrowing or
// merging as necessary to preserve B-Tree occupancy rules.
func (t *Tree) Delete(key string) error {
	if key == "" {
		return ErrInvalidKey
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Checking first keeps a failed delete from borrowing or merging nodes on
	// the search path. A missing key is therefore a true no-op.
	if _, _, found := find(t.root, key); !found {
		return ErrNotFound
	}
	t.delete(t.root, key)
	t.length--
	if !t.root.leaf && len(t.root.keys) == 0 {
		t.root = t.root.children[0]
	}
	return nil
}

// Range returns entries with start <= key < end in key order. An empty start
// or end is unbounded on that side. Returned values are copies. If end is not
// empty and end <= start, Range returns no entries.
func (t *Tree) Range(start, end string) []Entry {
	if start != "" && end != "" && start >= end {
		return []Entry{}
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	entries := make([]Entry, 0)
	t.appendRange(t.root, start, end, &entries)
	return entries
}

func (t *Tree) maxKeys() int {
	return 2*t.minimumDegree - 1
}

func (t *Tree) insertNonFull(current *node, newEntry entry) {
	index := sort.Search(len(current.keys), func(i int) bool {
		return current.keys[i].key >= newEntry.key
	})
	if current.leaf {
		current.keys = append(current.keys, entry{})
		copy(current.keys[index+1:], current.keys[index:])
		current.keys[index] = newEntry
		return
	}

	if len(current.children[index].keys) == t.maxKeys() {
		t.splitChild(current, index)
		switch {
		case newEntry.key > current.keys[index].key:
			index++
		case newEntry.key == current.keys[index].key:
			current.keys[index].value = newEntry.value
			return
		}
	}
	t.insertNonFull(current.children[index], newEntry)
}

// splitChild splits parent.children[index], which must contain 2t-1 keys.
func (t *Tree) splitChild(parent *node, index int) {
	full := parent.children[index]
	middle := t.minimumDegree - 1
	right := &node{leaf: full.leaf}
	median := full.keys[middle]
	right.keys = append(right.keys, full.keys[middle+1:]...)
	full.keys = full.keys[:middle]
	if !full.leaf {
		right.children = append(right.children, full.children[t.minimumDegree:]...)
		full.children = full.children[:t.minimumDegree]
	}

	parent.keys = append(parent.keys, entry{})
	copy(parent.keys[index+1:], parent.keys[index:])
	parent.keys[index] = median
	parent.children = append(parent.children, nil)
	copy(parent.children[index+2:], parent.children[index+1:])
	parent.children[index+1] = right
}

func find(current *node, key string) (*node, int, bool) {
	for {
		index := sort.Search(len(current.keys), func(i int) bool {
			return current.keys[i].key >= key
		})
		if index < len(current.keys) && current.keys[index].key == key {
			return current, index, true
		}
		if current.leaf {
			return nil, 0, false
		}
		current = current.children[index]
	}
}

func (t *Tree) delete(current *node, key string) bool {
	index := sort.Search(len(current.keys), func(i int) bool {
		return current.keys[i].key >= key
	})

	if index < len(current.keys) && current.keys[index].key == key {
		if current.leaf {
			current.keys = append(current.keys[:index], current.keys[index+1:]...)
			return true
		}
		return t.deleteFromInternalNode(current, index)
	}
	if current.leaf {
		return false
	}

	if len(current.children[index].keys) == t.minimumDegree-1 {
		if index > 0 && len(current.children[index-1].keys) >= t.minimumDegree {
			t.borrowFromPrevious(current, index)
		} else if index < len(current.children)-1 && len(current.children[index+1].keys) >= t.minimumDegree {
			t.borrowFromNext(current, index)
		} else if index < len(current.children)-1 {
			t.mergeChildren(current, index)
		} else {
			t.mergeChildren(current, index-1)
			index--
		}
	}
	return t.delete(current.children[index], key)
}

func (t *Tree) deleteFromInternalNode(current *node, index int) bool {
	key := current.keys[index].key
	left := current.children[index]
	right := current.children[index+1]

	if len(left.keys) >= t.minimumDegree {
		predecessor := rightmost(left)
		current.keys[index] = predecessor
		return t.delete(left, predecessor.key)
	}
	if len(right.keys) >= t.minimumDegree {
		successor := leftmost(right)
		current.keys[index] = successor
		return t.delete(right, successor.key)
	}
	t.mergeChildren(current, index)
	return t.delete(left, key)
}

func (t *Tree) borrowFromPrevious(parent *node, index int) {
	child := parent.children[index]
	sibling := parent.children[index-1]

	child.keys = append(child.keys, entry{})
	copy(child.keys[1:], child.keys[:len(child.keys)-1])
	child.keys[0] = parent.keys[index-1]
	parent.keys[index-1] = sibling.keys[len(sibling.keys)-1]
	sibling.keys = sibling.keys[:len(sibling.keys)-1]

	if !child.leaf {
		child.children = append(child.children, nil)
		copy(child.children[1:], child.children[:len(child.children)-1])
		child.children[0] = sibling.children[len(sibling.children)-1]
		sibling.children = sibling.children[:len(sibling.children)-1]
	}
}

func (t *Tree) borrowFromNext(parent *node, index int) {
	child := parent.children[index]
	sibling := parent.children[index+1]

	child.keys = append(child.keys, parent.keys[index])
	parent.keys[index] = sibling.keys[0]
	sibling.keys = sibling.keys[1:]

	if !child.leaf {
		child.children = append(child.children, sibling.children[0])
		sibling.children = sibling.children[1:]
	}
}

func (t *Tree) mergeChildren(parent *node, index int) {
	left := parent.children[index]
	right := parent.children[index+1]
	left.keys = append(left.keys, parent.keys[index])
	left.keys = append(left.keys, right.keys...)
	if !left.leaf {
		left.children = append(left.children, right.children...)
	}
	parent.keys = append(parent.keys[:index], parent.keys[index+1:]...)
	parent.children = append(parent.children[:index+1], parent.children[index+2:]...)
}

func leftmost(current *node) entry {
	for !current.leaf {
		current = current.children[0]
	}
	return current.keys[0]
}

func rightmost(current *node) entry {
	for !current.leaf {
		current = current.children[len(current.children)-1]
	}
	return current.keys[len(current.keys)-1]
}

func (t *Tree) appendRange(current *node, start, end string, entries *[]Entry) {
	for index, item := range current.keys {
		if !current.leaf {
			t.appendRange(current.children[index], start, end, entries)
		}
		if (start == "" || item.key >= start) && (end == "" || item.key < end) {
			*entries = append(*entries, Entry{Key: item.key, Value: copyValue(item.value)})
		}
	}
	if !current.leaf {
		t.appendRange(current.children[len(current.children)-1], start, end, entries)
	}
}

func copyValue(value []byte) []byte {
	return append([]byte(nil), value...)
}

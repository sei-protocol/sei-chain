package structures

import (
	"container/list"
	"fmt"
)

// LRUQueue implements a queue-like abstraction with LRU semantics, tracking both the number of
// entries and their aggregate size. Not thread safe.
type LRUQueue struct {
	order     *list.List
	entries   map[string]*list.Element
	totalSize uint64
}

type lruQueueEntry struct {
	key  string
	size uint64
}

// NewLRUQueue creates a new LRU queue.
func NewLRUQueue() *LRUQueue {
	return &LRUQueue{
		order:   list.New(),
		entries: make(map[string]*list.Element),
	}
}

// Push adds a new entry to the LRU queue. Can also be used to update an existing value with a new weight.
func (lru *LRUQueue) Push(
	// the key that was recently interacted with
	key []byte,
	// the size of the key + value
	size uint64,
) {
	// Indexing the map with string(key) does not copy the key; only a key that turns out to be new
	// is converted, which is the one case that has to retain it.
	if elem, ok := lru.entries[string(key)]; ok {
		lru.resize(elem, size)
		return
	}
	lru.insert(string(key), size)
}

// PushString is Push for a caller that already holds the key as a string, which is then retained
// rather than copied.
func (lru *LRUQueue) PushString(
	// the key that was recently interacted with
	key string,
	// the size of the key + value
	size uint64,
) {
	if elem, ok := lru.entries[key]; ok {
		lru.resize(elem, size)
		return
	}
	lru.insert(key, size)
}

// resize updates an existing entry's weight and marks it most recently used.
func (lru *LRUQueue) resize(elem *list.Element, size uint64) {
	entry := elem.Value.(*lruQueueEntry)
	if lru.totalSize < entry.size {
		// should be impossible
		panic(fmt.Errorf("size tracking is corrupted: totalSize %d < entry.size %d",
			lru.totalSize, entry.size))
	}
	lru.totalSize -= entry.size
	lru.totalSize += size
	entry.size = size
	lru.order.MoveToBack(elem)
}

// insert adds a key not already in the queue as the most recently used entry.
func (lru *LRUQueue) insert(key string, size uint64) {
	elem := lru.order.PushBack(&lruQueueEntry{
		key:  key,
		size: size,
	})
	lru.entries[key] = elem
	lru.totalSize += size
}

// Touch signals that an entry has been interacted with, moving it to the back of the queue
// (i.e. making it so it doesn't get popped soon).
func (lru *LRUQueue) Touch(key []byte) {
	elem, ok := lru.entries[string(key)]
	if !ok {
		return
	}
	lru.order.MoveToBack(elem)
}

// TouchString is Touch for a caller that already holds the key as a string.
func (lru *LRUQueue) TouchString(key string) {
	elem, ok := lru.entries[key]
	if !ok {
		return
	}
	lru.order.MoveToBack(elem)
}

// GetTotalSize returns the total size of all entries in the LRU queue.
func (lru *LRUQueue) GetTotalSize() uint64 {
	return lru.totalSize
}

// GetCount returns a count of the number of entries in the LRU queue, where each entry counts for 1
// regardless of size.
func (lru *LRUQueue) GetCount() uint64 {
	return uint64(len(lru.entries))
}

// PopLeastRecentlyUsed pops a single element out of the queue. The element removed is the entry
// least recently passed to Push/Touch. Returns the key in string form to avoid copying the key an
// additional time. Panics if the queue is empty.
func (lru *LRUQueue) PopLeastRecentlyUsed() string {
	elem := lru.order.Front()
	if elem == nil {
		panic("cannot pop from empty LRU queue")
	}

	lru.order.Remove(elem)
	entry := elem.Value.(*lruQueueEntry)
	delete(lru.entries, entry.key)
	if entry.size > lru.totalSize {
		// should be impossible
		panic(fmt.Errorf("size tracking is corrupted: entry.size %d > totalSize %d", entry.size, lru.totalSize))
	}
	lru.totalSize -= entry.size
	return entry.key
}

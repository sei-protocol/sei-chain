package dbcache

import (
	"container/list"
	"fmt"
)

// Implements a queue-like abstraction with LRU semantics. Not thread safe.
type lruQueue struct {
	order     *list.List
	entries   map[string]*list.Element
	totalSize uint64
}

type lruQueueEntry struct {
	key  string
	size uint64
}

// Create a new LRU queue.
func newLRUQueue() *lruQueue {
	return &lruQueue{
		order:   list.New(),
		entries: make(map[string]*list.Element),
	}
}

// Add a new entry to the LRU queue. Can also be used to update an existing value with a new weight.
func (lru *lruQueue) Push(
	// the key in the cache that was recently interacted with
	key []byte,
	// the size of the key + value
	size uint64,
) {
	if elem, ok := lru.entries[string(key)]; ok {
		entry := elem.Value.(*lruQueueEntry)
		if size < entry.size {
			// should be impossible
			panic(fmt.Errorf("size tracking is corrupted: size %d < entry.size %d", size, entry.size))
		}
		lru.totalSize += size - entry.size
		entry.size = size
		lru.order.MoveToBack(elem)
		return
	}

	keyStr := string(key)
	elem := lru.order.PushBack(&lruQueueEntry{
		key:  keyStr,
		size: size,
	})
	lru.entries[keyStr] = elem
	lru.totalSize += size
}

// Signal that an entry has been interacted with, moving it to the back of the queue
// (i.e. making it so it doesn't get popped soon).
func (lru *lruQueue) Touch(key []byte) {
	elem, ok := lru.entries[string(key)]
	if !ok {
		return
	}
	lru.order.MoveToBack(elem)
}

// Returns the total size of all entries in the LRU queue.
func (lru *lruQueue) GetTotalSize() uint64 {
	return lru.totalSize
}

// Returns a count of the number of entries in the LRU queue, where each entry counts for 1 regardless of size.
func (lru *lruQueue) GetCount() uint64 {
	return uint64(len(lru.entries))
}

// Pops a single element out of the queue. The element removed is the entry least recently passed to Update().
// Returns the key in string form to avoid copying the key an additional time.
// Panics if the queue is empty.
func (lru *lruQueue) PopLeastRecentlyUsed() string {
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

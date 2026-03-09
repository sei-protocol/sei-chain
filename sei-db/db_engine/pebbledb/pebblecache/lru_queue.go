package pebblecache

import "container/list"

// Implements a queue-like abstraction with LRU semantics. Not thread safe.
type lruQueue struct {
	order     *list.List
	entries   map[string]*list.Element
	totalSize int
}

type lruQueueEntry struct {
	key  []byte
	size int
}

// Create a new LRU queue.
func NewLRUQueue() *lruQueue {
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
	size int,
) {
	keyString := string(key) // TODO revisit and maybe do unsafe copies
	if elem, ok := lru.entries[keyString]; ok {
		entry := elem.Value.(*lruQueueEntry)
		lru.totalSize += size - entry.size
		entry.size = size
		lru.order.MoveToBack(elem)
		return
	}

	keyCopy := append([]byte(nil), key...) // TODO don't do this
	elem := lru.order.PushBack(&lruQueueEntry{
		key:  keyCopy,
		size: size,
	})
	lru.entries[keyString] = elem
	lru.totalSize += size
}

// Signal that an entry has been interated with, moving it to the to the back of the queue
// (i.e. making it so it doesn't get popped soon).
func (lru *lruQueue) Touch(key []byte) {
	elem, ok := lru.entries[string(key)]
	if !ok {
		return
	}
	lru.order.MoveToBack(elem)
}

// Returns the total size of all entries in the LRU queue.
func (lru *lruQueue) GetTotalSize() int {
	return lru.totalSize
}

// Returns a count of the number of entries in the LRU queue, where each entry counts for 1 regardless of size.
func (lru *lruQueue) GetCount() int {
	return len(lru.entries)
}

// Pops a single element out of the queue. The element removed is the entry least recently passed to Update().
// Panics if the queue is empty.
func (lru *lruQueue) PopLeastRecentlyUsed() []byte {
	elem := lru.order.Front()
	if elem == nil {
		panic("cannot pop from empty LRU queue")
	}

	lru.order.Remove(elem)
	entry := elem.Value.(*lruQueueEntry)
	delete(lru.entries, string(entry.key))
	lru.totalSize -= entry.size
	return entry.key
}

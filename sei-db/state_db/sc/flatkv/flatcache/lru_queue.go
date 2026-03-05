package flatcache

// Implements a queue-like abstraction with LRU semantics. Not thread safe.
type lruQueue struct {
}

// Create a new LRU queue.
func NewLRUQueue() *lruQueue {
	return &lruQueue{}
}

// Add a new entry to the LRU queue. Can also be used to update an existing value with a new weight.
func (lru *lruQueue) Push(
	// the key in the cache that was recently interacted with
	key []byte,
	// the size of the key + value
	size int,
) {

}

// Signal that an entry has been interated with, moving it to the to the back of the queue
// (i.e. making it so it doesn't get popped soon).
func (lru *lruQueue) Touch(key []byte) {

}

// Returns the total size of all entries in the LRU queue.
func (lru *lruQueue) GetTotalSize() int {
	return 0
}

// Returns a count of the number of entries in the LRU queue, where each entry counts for 1 regardless of size.
func (lru *lruQueue) GetCount() int {
	return 0
}

// Pops a single element out of the queue. The element removed is the entry least recently passed to Update().
// Panics if the queue is empty.
func (lru *lruQueue) PopLeastRecentlyUsed() []byte {
	return nil
}

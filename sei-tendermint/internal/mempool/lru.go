package mempool

type listNode[K comparable, V any] struct {
	prev *listNode[K, V]
	next *listNode[K, V]
	k    K
	v    V
}

func (n *listNode[K, V]) remove() {
	n.next.prev = n.prev
	n.prev.next = n.next
}

// lruTxCache maintains a NON-threadsafe lru cache of raw transactions. The cache
// only stores the hash of the raw transaction.
type lruCache[K comparable, V any] struct {
	capacity int
	byKey    map[K]*listNode[K, V]
	end      listNode[K, V]
}

type duplicateCacheKey = string

// newLRUTxCache creates an LRU (Least Recently Used) cache that stores
// transactions by their full SHA-256 hash. When the cache exceeds cacheSize,
// the least recently used entry is evicted.
func newLRUCache[K comparable, V any](capacity int) *lruCache[K, V] {
	c := &lruCache[K, V]{capacity: capacity}
	c.Reset()
	return c
}

func (c *lruCache[K, V]) Has(k K) bool {
	_, ok := c.byKey[k]
	return ok
}

func (c *lruCache[K, V]) Get(k K) (V, bool) {
	if n, ok := c.byKey[k]; ok {
		return n.v, true
	}
	var zero V
	return zero, false
}

func (c *lruCache[K, V]) Reset() {
	c.byKey = make(map[K]*listNode[K, V], c.capacity)
	c.end.next = &c.end
	c.end.prev = &c.end
}

func (c *lruCache[K, V]) pushNode(n *listNode[K, V]) {
	n.next = &c.end
	n.prev = c.end.prev
	n.prev.next = n
	n.next.prev = n
}

// Pushes (k,v) pair to the cache, overwriting the previous value for k if existing.
// Returns true if k was not in the cache.
func (c *lruCache[K, V]) Push(k K, v V) {
	n, ok := c.byKey[k]
	if ok {
		n.remove()
	} else {
		n = &listNode[K, V]{k: k}
		c.byKey[k] = n
	}
	n.v = v
	c.pushNode(n)
	if len(c.byKey) > c.capacity {
		n := c.end.next
		n.remove()
		delete(c.byKey, n.k)
	}
}

func (c *lruCache[K, V]) Remove(k K) {
	if n, ok := c.byKey[k]; ok {
		delete(c.byKey, k)
		n.remove()
	}
}

func (c *lruCache[K, V]) Size() int { return len(c.byKey) }

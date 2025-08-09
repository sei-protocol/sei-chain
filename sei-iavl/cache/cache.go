package cache

import (
	"container/list"

	ibytes "github.com/sei-protocol/sei-chain/sei-iavl/internal/bytes"
)

// Node represents a node eligible for caching.
type Node interface {
	GetCacheKey() []byte
}

// Cache is an in-memory structure to persist nodes for quick access.
// Please see lruCache for more details about why we need a custom
// cache implementation.
type Cache interface {
	// Adds node to cache. If full and had to remove the oldest element,
	// returns the oldest, otherwise nil.
	// CONTRACT: node can never be nil. Otherwise, cache panics.
	Add(node Node) Node

	// Returns Node for the key, if exists. nil otherwise.
	Get(key []byte) Node

	// Has returns true if node with key exists in cache, false otherwise.
	Has(key []byte) bool

	// Remove removes node with key from cache. The removed node is returned.
	// if not in cache, return nil.
	Remove(key []byte) Node

	// Len returns the cache length.
	Len() int
}

// lruCache is an LRU cache implementation.
// The motivation for using a custom cache implementation is to
// allow for a custom max policy.
//
// Currently, the cache maximum is implemented in terms of the
// number of nodes which is not intuitive to configure.
// Instead, we are planning to add a byte maximum.
// The alternative implementations do not allow for
// customization and the ability to estimate the byte
// size of the cache.
type lruCache struct {
	dict            map[string]*list.Element // FastNode cache.
	maxElementCount int                      // FastNode the maximum number of nodes in the cache.
	ll              *list.List               // LRU queue of cache elements. Used for deletion.
}

var _ Cache = (*lruCache)(nil)

func New(maxElementCount int) Cache {
	return &lruCache{
		dict:            make(map[string]*list.Element),
		maxElementCount: maxElementCount,
		ll:              list.New(),
	}
}

func (c *lruCache) Add(node Node) Node {
	keyStr := ibytes.UnsafeBytesToStr(node.GetCacheKey())
	if e, exists := c.dict[keyStr]; exists {
		c.ll.MoveToFront(e)
		old := e.Value
		e.Value = node
		return old.(Node)
	}

	elem := c.ll.PushFront(node)
	c.dict[keyStr] = elem

	if c.ll.Len() > c.maxElementCount {
		oldest := c.ll.Back()
		return c.remove(oldest)
	}
	return nil
}

func (nc *lruCache) Get(key []byte) Node {
	if ele, hit := nc.dict[ibytes.UnsafeBytesToStr(key)]; hit {
		nc.ll.MoveToFront(ele)
		return ele.Value.(Node)
	}
	return nil
}

func (c *lruCache) Has(key []byte) bool {
	_, exists := c.dict[ibytes.UnsafeBytesToStr(key)]
	return exists
}

func (nc *lruCache) Len() int {
	return nc.ll.Len()
}

func (c *lruCache) Remove(key []byte) Node {
	if elem, exists := c.dict[ibytes.UnsafeBytesToStr(key)]; exists {
		return c.remove(elem)
	}
	return nil
}

func (c *lruCache) remove(e *list.Element) Node {
	element := c.ll.Remove(e)
	if element != nil {
		node := element.(Node)
		delete(c.dict, ibytes.UnsafeBytesToStr(node.GetCacheKey()))
		return node
	}
	return nil
}

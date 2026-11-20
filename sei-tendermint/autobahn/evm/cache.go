package evm

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// cache retains the newest two generations of key-value pairs.
type cache[K comparable, V any] struct {
	fresh, stale map[K]V
	// capacity is how many entries a generation holds before it rolls over.
	capacity int
}

// newCache returns a cache with the given capacity per generation.
func newCache[K comparable, V any](capacity int) cache[K, V] {
	return cache[K, V]{fresh: map[K]V{}, capacity: capacity}
}

// push records a key-value pair in the newest generation.
func (c *cache[K, V]) push(key K, value V) {
	// A full fresh generation replaces the stale one.
	if len(c.fresh) >= c.capacity {
		// No size hint: pre-sizing or clearing a generation costs O(capacity) under the lock.
		c.stale, c.fresh = c.fresh, map[K]V{}
	}
	c.fresh[key] = value
}

// get returns the value for key, if present, without removing it.
func (c *cache[K, V]) get(key K) utils.Option[V] {
	for _, gen := range [...]map[K]V{c.fresh, c.stale} {
		if value, ok := gen[key]; ok {
			return utils.Some(value)
		}
	}
	return utils.None[V]()
}

// pop removes key from both generations.
func (c *cache[K, V]) pop(key K) {
	delete(c.fresh, key)
	delete(c.stale, key)
}

package cache

// WeightCalculator is a function that calculates the weight of a key-value pair in a Cache.
// By default, the weight of a key-value pair is 1. Cache capacity is always specified in terms of
// the weight of the key-value pairs it can hold, rather than the number of key-value pairs.
type WeightCalculator[K comparable, V any] func(key K, value V) uint64

// Cache is an interface for a generic cache.
//
// Unless otherwise noted, Cache implementations are not required to be thread safe.
type Cache[K comparable, V any] interface {
	// Get returns the value associated with the key, and a boolean indicating whether the key was found in the cache.
	Get(key K) (V, bool)

	// Put adds a key-value pair to the cache. After this operation, values may be dropped if the total weight
	// exceeds the configured maximum weight. Will ignore the new value if it exceeds the maximum weight
	// of the cache in and of itself.
	Put(key K, value V)

	// Size returns the number of key-value pairs in the cache.
	Size() int

	// Weight returns the total weight of the key-value pairs in the cache.
	Weight() uint64

	// SetMaxWeight sets the maximum weight of the cache. If the current weight exceeds the new capacity,
	// the cache will evict key-value pairs until the weight is less than or equal to the new capacity.
	SetMaxWeight(capacity uint64)
}

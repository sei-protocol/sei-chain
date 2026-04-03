package cache

import "sync"

var _ Cache[string, string] = &threadSafeCache[string, string]{}

// threadSafeCache is a thread-safe wrapper around a Cache.
type threadSafeCache[K comparable, V any] struct {
	cache Cache[K, V]
	lock  sync.RWMutex
}

// NewThreadSafeCache wraps a Cache in a thread-safe wrapper.
func NewThreadSafeCache[K comparable, V any](cache Cache[K, V]) Cache[K, V] {
	return &threadSafeCache[K, V]{
		cache: cache,
	}
}

func (t *threadSafeCache[K, V]) Get(key K) (V, bool) {
	t.lock.RLock()
	defer t.lock.RUnlock()
	return t.cache.Get(key)
}

func (t *threadSafeCache[K, V]) Put(key K, value V) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.cache.Put(key, value)
}

func (t *threadSafeCache[K, V]) Size() int {
	t.lock.RLock()
	defer t.lock.RUnlock()
	return t.cache.Size()
}

func (t *threadSafeCache[K, V]) Weight() uint64 {
	t.lock.RLock()
	defer t.lock.RUnlock()
	return t.cache.Weight()
}

func (t *threadSafeCache[K, V]) SetMaxWeight(capacity uint64) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.cache.SetMaxWeight(capacity)
}

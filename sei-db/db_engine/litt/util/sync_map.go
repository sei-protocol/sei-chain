package util

import "sync"

// SyncMap is a generic thread-safe map backed by a plain map and a sync.RWMutex.
// It is optimized for workloads with batch writes and concurrent reads, where
// sync.Map's slow path for unique-key insertions becomes a bottleneck.
type SyncMap[K comparable, V any] struct {
	mu sync.RWMutex
	m  map[K]V
}

// NewSyncMap creates a new SyncMap.
func NewSyncMap[K comparable, V any]() *SyncMap[K, V] {
	return &SyncMap[K, V]{
		m: make(map[K]V),
	}
}

// Get returns the value for the given key and whether it was found.
func (s *SyncMap[K, V]) Get(key K) (V, bool) {
	s.mu.RLock()
	v, ok := s.m[key]
	s.mu.RUnlock()
	return v, ok
}

// Set sets a single key-value pair.
func (s *SyncMap[K, V]) Set(key K, value V) {
	s.mu.Lock()
	s.m[key] = value
	s.mu.Unlock()
}

// PutBatch inserts all entries from the provided map in a single critical section.
func (s *SyncMap[K, V]) PutBatch(entries map[K]V) {
	s.mu.Lock()
	for k, v := range entries {
		s.m[k] = v
	}
	s.mu.Unlock()
}

// Delete removes a single key.
func (s *SyncMap[K, V]) Delete(key K) {
	s.mu.Lock()
	delete(s.m, key)
	s.mu.Unlock()
}

// DeleteBatch removes all keys in the slice in a single critical section.
func (s *SyncMap[K, V]) DeleteBatch(keys []K) {
	s.mu.Lock()
	for _, k := range keys {
		delete(s.m, k)
	}
	s.mu.Unlock()
}

// Len returns the number of entries in the map.
func (s *SyncMap[K, V]) Len() int {
	s.mu.RLock()
	n := len(s.m)
	s.mu.RUnlock()
	return n
}

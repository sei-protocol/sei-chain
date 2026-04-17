package cryptosim

import (
	"iter"
	"sync"
)

// A thread safe map-like data structure. Unlike sync.Map, supports generics.
type SyncMap[K comparable, V any] struct {
	base sync.Map
}

// NewSyncMap returns a new empty SyncMap.
func NewSyncMap[K comparable, V any]() *SyncMap[K, V] {
	return &SyncMap[K, V]{}
}

// Put stores the key-value pair in the map.
func (m *SyncMap[K, V]) Put(key K, value V) {
	m.base.Store(key, value)
}

// Clear removes all key-value pairs from the map.
func (m *SyncMap[K, V]) Clear() {
	m.base.Clear()
}

// Get returns the value for key and true if present, or the zero value of V and false otherwise.
func (m *SyncMap[K, V]) Get(key K) (V, bool) {
	val, ok := m.base.Load(key)
	if !ok {
		var zero V
		return zero, false
	}
	return val.(V), true
}

// All returns an iterator over the map's key-value pairs for use with range.
func (m *SyncMap[K, V]) Iterator() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		m.base.Range(func(key, value any) bool {
			return yield(key.(K), value.(V))
		})
	}
}

package datastructures

import (
	"sort"
	"sync"

	"golang.org/x/exp/constraints"
)

// A map-like data structure that is guaranteed to be data race free during write
// operations. It is a typed wrapper over the builtin typeless `sync.Map`. The
// CRUD interface is exactly the same as those of `sync.Map`.
type TypedSyncMap[K constraints.Ordered, V any] struct {
	internal *sync.Map
}

func NewTypedSyncMap[K constraints.Ordered, V any]() *TypedSyncMap[K, V] {
	return &TypedSyncMap[K, V]{
		internal: &sync.Map{},
	}
}

func (m *TypedSyncMap[K, V]) Load(key K) (value V, ok bool) {
	untypedVal, ok := m.internal.Load(key)
	value, _ = untypedVal.(V)
	return
}

func (m *TypedSyncMap[K, V]) Store(key K, value V) {
	m.internal.Store(key, value)
}

func (m *TypedSyncMap[K, V]) LoadOrStore(key K, value V) (actual V, loaded bool) {
	untypedVal, loaded := m.internal.LoadOrStore(key, value)
	actual, _ = untypedVal.(V)
	return
}

func (m *TypedSyncMap[K, V]) Delete(key K) {
	m.internal.Delete(key)
}

func (m *TypedSyncMap[K, V]) Range(f func(K, V) bool) {
	// All map iterations should be deterministic, so we apply f in sorted order to avoid nondeterminism
	var keys []K
	m.internal.Range(func(key, val any) bool {
		keys = append(keys, key.(K))
		return true
	})
	sort.Slice(keys, func(i, j int) bool {
		return keys[i] < keys[j]
	})

	for _, key := range keys {
		val, _ := m.internal.Load(key)
		typedVal := val.(V)
		f(key, typedVal)
	}
}

func (m *TypedSyncMap[K, V]) Len() int {
	length := 0
	m.Range(func(_ K, _ V) bool {
		length++
		return true
	})
	return length
}

func (m *TypedSyncMap[K, V]) DeepCopy(copier func(V) V) *TypedSyncMap[K, V] {
	mapcopy := NewTypedSyncMap[K, V]()
	m.Range(func(key K, val V) bool {
		mapcopy.Store(key, copier(val))
		return true
	})
	return mapcopy
}

func (m *TypedSyncMap[K, V]) DeepApply(toApply func(V)) {
	m.Range(func(_ K, val V) bool {
		toApply(val)
		return true
	})
}

// A nested map data structure that is guaranteed to be data race free during write
// operations. It is the synchronous equivalent of type map[K1]map[K2]V. Besides
// `sync.Map`'s existing interfaces, it also provides convenient methods to read/write
// nested values directly. For example, to set value `v` for outer key `k1` and inner
// key `k2`, one can simply call StoreNested(k1, k2, v), without worrying about creating
// the inner map if it doesn't exist.
type TypedNestedSyncMap[K1 constraints.Ordered, K2 constraints.Ordered, V any] struct {
	*TypedSyncMap[K1, *TypedSyncMap[K2, V]]
	mu *sync.Mutex // XXXNested methods have write operations outside sync.Map
}

func NewTypedNestedSyncMap[K1 constraints.Ordered, K2 constraints.Ordered, V any]() *TypedNestedSyncMap[K1, K2, V] {
	return &TypedNestedSyncMap[K1, K2, V]{
		TypedSyncMap: NewTypedSyncMap[K1, *TypedSyncMap[K2, V]](),
		mu:           &sync.Mutex{},
	}
}

func (m *TypedNestedSyncMap[K1, K2, V]) LoadNested(key1 K1, key2 K2) (value V, ok bool) {
	nestedMap, ok := m.TypedSyncMap.Load(key1)
	if !ok {
		return
	}
	value, ok = nestedMap.Load(key2)
	return
}

func (m *TypedNestedSyncMap[K1, K2, V]) StoreNested(key1 K1, key2 K2, value V) {
	m.mu.Lock()
	defer m.mu.Unlock()
	nestedMap, _ := m.TypedSyncMap.LoadOrStore(key1, NewTypedSyncMap[K2, V]())
	nestedMap.Store(key2, value)
}

func (m *TypedNestedSyncMap[K1, K2, V]) LoadOrStoreNested(key1 K1, key2 K2, value V) (actual V, loaded bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	nestedMap, _ := m.TypedSyncMap.LoadOrStore(key1, NewTypedSyncMap[K2, V]())
	actual, loaded = nestedMap.LoadOrStore(key2, value)
	return
}

func (m *TypedNestedSyncMap[K1, K2, V]) DeleteNested(key1 K1, key2 K2) {
	m.mu.Lock()
	defer m.mu.Unlock()
	nestedMap, ok := m.TypedSyncMap.Load(key1)
	if !ok {
		return
	}
	nestedMap.Delete(key2)
	if nestedMap.Len() == 0 {
		m.TypedSyncMap.Delete(key1)
	}
}

func (m *TypedNestedSyncMap[K1, K2, V]) DeepCopy(copier func(V) V) *TypedNestedSyncMap[K1, K2, V] {
	m.mu.Lock()
	defer m.mu.Unlock()
	mapcopy := NewTypedNestedSyncMap[K1, K2, V]()
	m.Range(func(key K1, val *TypedSyncMap[K2, V]) bool {
		mapcopy.Store(key, val.DeepCopy(copier))
		return true
	})
	return mapcopy
}

func (m *TypedNestedSyncMap[K1, K2, V]) DeepApply(toApply func(V)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Range(func(_ K1, val *TypedSyncMap[K2, V]) bool {
		val.DeepApply(toApply)
		return true
	})
}

package im

import (
	"github.com/benbjohnson/immutable"
	"hash/maphash"
)

type Map[K comparable, V any] struct { m *immutable.Map[K, V] }

type hasher[K comparable] struct { seed maphash.Seed }

func (h hasher[K]) Hash(key K) uint32 {
	return uint32(maphash.Comparable(h.seed,key))
}

func (h hasher[K]) Equal(a, b K) bool {
	return a == b
}

func NewMap[K comparable, V any]() Map[K, V] {
	return Map[K, V]{ immutable.NewMap[K, V](hasher[K]{maphash.MakeSeed()}) }
}

func (m Map[K, V]) Get(key K) (V, bool) {
	return m.m.Get(key)
}

func (m Map[K, V]) Set(key K, value V) Map[K, V] {
	return Map[K, V]{ m.m.Set(key, value) }
}

func (m Map[K, V]) Delete(key K) Map[K, V] {
	return Map[K, V]{ m.m.Delete(key) }
}

func (m Map[K, V]) Len() int {
	return m.m.Len()
}

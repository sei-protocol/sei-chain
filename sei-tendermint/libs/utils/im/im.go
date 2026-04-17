package im

import (
	"cmp"
	"hash/maphash"
	"iter"

	"github.com/benbjohnson/immutable"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

type Map[K comparable, V any] struct{ m *immutable.Map[K, V] }

type hasher[K comparable] struct{ seed maphash.Seed }

func (h hasher[K]) Hash(key K) uint32 {
	return uint32(maphash.Comparable(h.seed, key)) //nolint:gosec // intentional truncation; only lower 32 bits needed for hash bucketing
}

func (h hasher[K]) Equal(a, b K) bool {
	return a == b
}

func NewMap[K comparable, V any]() Map[K, V] {
	return Map[K, V]{immutable.NewMap[K, V](hasher[K]{maphash.MakeSeed()})}
}

func (m Map[K, V]) Get(key K) (V, bool) {
	return m.m.Get(key)
}

// GetOpt returns the value under key, or None, if key is missing.
func (m Map[K, V]) GetOpt(key K) utils.Option[V] {
	if v, ok := m.Get(key); ok {
		return utils.Some(v)
	}
	return utils.None[V]()
}

func (m Map[K, V]) Set(key K, value V) Map[K, V] {
	return Map[K, V]{m.m.Set(key, value)}
}

// SetOpt sets key to the given value, or deletes the key if mvalue is None.
func (m Map[K, V]) SetOpt(key K, mvalue utils.Option[V]) Map[K, V] {
	if value, ok := mvalue.Get(); ok {
		return m.Set(key, value)
	} else {
		return m.Delete(key)
	}
}

func (m Map[K, V]) Delete(key K) Map[K, V] {
	return Map[K, V]{m.m.Delete(key)}
}

func (m Map[K, V]) Len() int {
	return m.m.Len()
}

func (m Map[K, V]) All() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for it := m.m.Iterator(); !it.Done(); {
			k, v, _ := it.Next()
			if !yield(k, v) {
				break
			}
		}
	}
}

type comparer[K any] func(K, K) int

func (c comparer[K]) Compare(a, b K) int { return c(a, b) }

type SortedMap[K, V any] struct{ m *immutable.SortedMap[K, V] }

func NewSortedMap[K, V any](cmp func(K, K) int) SortedMap[K, V] {
	return SortedMap[K, V]{immutable.NewSortedMap[K, V](comparer[K](cmp))}
}

func NewOrderedMap[K cmp.Ordered, V any]() SortedMap[K, V] {
	return NewSortedMap[K, V](cmp.Compare[K])
}

func (m SortedMap[K, V]) Get(key K) (V, bool) {
	return m.m.Get(key)
}

func (m SortedMap[K, V]) Set(key K, value V) SortedMap[K, V] {
	return SortedMap[K, V]{m.m.Set(key, value)}
}

func (m SortedMap[K, V]) Delete(key K) SortedMap[K, V] {
	return SortedMap[K, V]{m.m.Delete(key)}
}

func (m SortedMap[K, V]) Len() int {
	return m.m.Len()
}

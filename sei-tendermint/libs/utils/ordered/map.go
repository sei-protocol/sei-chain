package ordered

import (
	"iter"
	"github.com/tidwall/btree"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

type Ordered[T any] interface {
	Less(T) bool
}

type mapEntry[K Ordered[K],V any] struct {
	k K
	v V
}

func unpack[K Ordered[K],V any](e mapEntry[K,V], ok bool) (K,V,bool) { return e.k,e.v,ok }
func unpackV[K Ordered[K],V any](e mapEntry[K,V], ok bool) (V,bool) { return e.v,ok }

type Map[K Ordered[K], V any] struct { m *btree.BTreeG[mapEntry[K,V]] }

func (m Map[K,V]) Get(k K) (V,bool) { return unpackV(m.m.Get(mapEntry[K,V]{k:k})) }
func (m Map[K,V]) GetAt(i int) (K,V,bool) { return unpack(m.m.GetAt(i)) }
func (m Map[K,V]) Set(k K, v V) (V,bool) { return unpackV(m.m.Set(mapEntry[K,V]{k:k,v:v})) }
func (m Map[K,V]) Delete(k K) (V,bool) { return unpackV(m.m.Delete(mapEntry[K,V]{k:k})) }
func (m Map[K,V]) Min() (K,V,bool) { return unpack(m.m.Min()) }
func (m Map[K,V]) Max() (K,V,bool) { return unpack(m.m.Max()) }
func (m Map[K,V]) PopMin() (K,V,bool) { return unpack(m.m.PopMin()) }
func (m Map[K,V]) PopMax() (K,V,bool) { return unpack(m.m.PopMax()) }
func (m Map[K,V]) Len() int { return m.m.Len() }
func (m Map[K,V]) Clear() { m.m.Clear() }

func (m Map[K,V]) GetOpt(k K) utils.Option[V] {
	if v,ok := m.Get(k); ok { return utils.Some(v) }
	return utils.None[V]()
}

func (m Map[K,V]) All() iter.Seq2[K,V] {
	return func(yield func(K, V) bool) {
		m.m.Scan(func(e mapEntry[K,V]) bool { return yield(e.k,e.v) })
	}
}

func NewMap[K Ordered[K], V any]() Map[K,V] {
	return Map[K,V]{btree.NewBTreeGOptions(
		func(a,b mapEntry[K,V]) bool { return a.k.Less(b.k) },
		btree.Options{NoLocks:true},
	)}
}

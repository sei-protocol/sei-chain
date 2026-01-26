package giga

import (
	"context"
	"github.com/google/btree"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
)

type poolEntry[V any] struct {
	idx     uint64
	deleted utils.AtomicSend[bool]
	val     V
}

func (e *poolEntry[V]) Run(ctx context.Context, task func(context.Context) error) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return task(ctx) })
		_, err := e.deleted.Wait(ctx, func(d bool) bool { return d })
		return err
	})
}

type poolInner[K comparable, V any] struct {
	nextIdx uint64
	byIdx   *btree.BTreeG[*poolEntry[V]]
	byKey   map[K]*poolEntry[V]
}

type Pool[K comparable, V any] struct {
	inner utils.Watch[*poolInner[K, V]]
}

func NewPool[K comparable, V any]() *Pool[K, V] {
	return &Pool[K, V]{utils.NewWatch(&poolInner[K, V]{
		byIdx: btree.NewG(2, func(a, b *poolEntry[V]) bool { return a.idx < b.idx }),
		byKey: map[K]*poolEntry[V]{},
	})}
}

func (p *Pool[K, V]) InsertAndRun(ctx context.Context, key K, val V, task func(context.Context) error) error {
	e := &poolEntry[V]{
		deleted: utils.NewAtomicSend(false),
		val:     val,
	}
	for inner, ctrl := range p.inner.Lock() {
		ctrl.Updated()
		e.idx = inner.nextIdx
		inner.nextIdx += 1
		inner.byIdx.ReplaceOrInsert(e)
		old, ok := inner.byKey[key]
		if ok {
			old.deleted.Store(true)
		}
		inner.byKey[key] = e
	}
	err := e.Run(ctx, task)
	e.deleted.Store(true)
	for inner := range p.inner.Lock() {
		inner.byIdx.Delete(e)
		if inner.byKey[key] == e {
			delete(inner.byKey, key)
		}
	}
	return err
}

func (p *Pool[K, V]) RunForEach(ctx context.Context, task func(context.Context, V) error) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		var next poolEntry[V]
		for ctx.Err() == nil {
			for inner, ctrl := range p.inner.Lock() {
				inner.byIdx.AscendGreaterOrEqual(&next, func(e *poolEntry[V]) bool {
					s.Spawn(func() error {
						return utils.IgnoreCancel(e.Run(ctx, func(ctx context.Context) error { return task(ctx, e.val) }))
					})
					next.idx = e.idx + 1
					return true
				})
				if err := ctrl.Wait(ctx); err != nil {
					return err
				}
			}
		}
		return ctx.Err()
	})
}

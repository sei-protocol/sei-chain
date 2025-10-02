package p2p

import (
	"container/heap"
	"context"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/tendermint/tendermint/libs/utils"
)

type ord[T any] interface {
	Less(T) bool
}

type withIdx[T any] struct {
	v      T
	minIdx int // index in byMin
	maxIdx int // index in byMax
}

func newWithIdx[T any](v T) *withIdx[T] {
	return &withIdx[T]{v: v}
}

// Heap returning minimal elements.
type byMin[T ord[T]] struct{ a []*withIdx[T] }

func newByMin[T ord[T]](capacity int) byMin[T] { return byMin[T]{make([]*withIdx[T], 0, capacity)} }
func (x *byMin[T]) Less(i, j int) bool         { return x.a[i].v.Less(x.a[j].v) }
func (x *byMin[T]) Len() int                   { return len(x.a) }
func (x *byMin[T]) Swap(i, j int) {
	x.a[i], x.a[j] = x.a[j], x.a[i]
	x.a[i].minIdx = i
	x.a[j].minIdx = j
}
func (x *byMin[T]) Push(v any) {
	w := v.(*withIdx[T])
	w.minIdx = len(x.a)
	x.a = append(x.a, w)
}
func (x *byMin[T]) Pop() any {
	n := len(x.a) - 1
	w := x.a[n]
	x.a = x.a[:n]
	return w
}

// Heap returning maximal elements.
type byMax[T ord[T]] struct{ a []*withIdx[T] }

func newByMax[T ord[T]](capacity int) byMax[T] { return byMax[T]{make([]*withIdx[T], 0, capacity)} }
func (x *byMax[T]) Less(i, j int) bool         { return x.a[j].v.Less(x.a[i].v) }
func (x *byMax[T]) Len() int                   { return len(x.a) }
func (x *byMax[T]) Swap(i, j int) {
	x.a[i], x.a[j] = x.a[j], x.a[i]
	x.a[i].maxIdx = i
	x.a[j].maxIdx = j
}
func (x *byMax[T]) Push(v any) {
	w := v.(*withIdx[T])
	w.maxIdx = len(x.a)
	x.a = append(x.a, w)
}
func (x *byMax[T]) Pop() any {
	n := len(x.a) - 1
	w := x.a[n]
	x.a = x.a[:n]
	return w
}

// pqEnvelope defines a wrapper around an Envelope with priority to be inserted
// into a priority Queue used for Envelope scheduling.
type pqEnvelope struct {
	envelope  Envelope
	priority  int
	size      int
	timestamp time.Time
}

// true <=> a has higher priority than b
func (a *pqEnvelope) Less(b *pqEnvelope) bool {
	// higher base priority wins
	if a, b := a.priority, b.priority; a != b {
		return a > b
	}
	// newer timestamp wins
	if a, b := a.timestamp, b.timestamp; a.Sub(b).Abs() >= 10*time.Millisecond {
		return a.After(b)
	}
	// larger first
	return a.size > b.size
}

type inner struct {
	capacity int
	byMin    byMin[*pqEnvelope]
	byMax    byMax[*pqEnvelope]
}

func newInner(capacity int) *inner {
	return &inner{
		capacity: capacity,
		// We prune the maximal elements whenever capacity is exceeded.
		// Therefore to avoid reallocation we need the heaps to have capacity+1.
		byMin: newByMin[*pqEnvelope](capacity + 1),
		byMax: newByMax[*pqEnvelope](capacity + 1),
	}
}

func (i *inner) Len() int { return i.byMin.Len() }

func (i *inner) Push(e *pqEnvelope) utils.Option[Envelope] {
	w := newWithIdx(e)
	heap.Push(&i.byMin, w)
	heap.Push(&i.byMax, w)
	if i.byMin.Len() > i.capacity {
		w := heap.Pop(&i.byMax).(*withIdx[*pqEnvelope])
		heap.Remove(&i.byMin, w.minIdx)
		return utils.Some(w.v.envelope)
	}
	return utils.None[Envelope]()
}

func (i *inner) Pop() *pqEnvelope {
	w := heap.Pop(&i.byMin).(*withIdx[*pqEnvelope])
	heap.Remove(&i.byMax, w.maxIdx)
	return w.v
}

type Queue struct{ inner utils.Watch[*inner] }

func NewQueue(size int) *Queue {
	if size <= 0 {
		// prevent caller from shooting self in the foot.
		size = 1
	}
	return &Queue{inner: utils.NewWatch(newInner(size))}
}

func (q *Queue) Len() int {
	for inner := range q.inner.Lock() {
		return inner.Len()
	}
	panic("unreachable")
}

// Non-blocking send.
// Returns the pruned message if any.
func (q *Queue) Send(e Envelope, priority int) utils.Option[Envelope] {
	// We construct the pqEnvelope without holding the lock to avoid contention.
	pqe := &pqEnvelope{
		envelope:  e,
		size:      proto.Size(e.Message),
		priority:  priority,
		timestamp: time.Now().UTC(),
	}
	for inner, ctrl := range q.inner.Lock() {
		pruned := inner.Push(pqe)
		ctrl.Updated()
		return pruned
	}
	panic("unreachable")
}

// Blocking recv.
func (q *Queue) Recv(ctx context.Context) (Envelope, error) {
	for inner, ctrl := range q.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return inner.Len() > 0 }); err != nil {
			return Envelope{}, err
		}
		return inner.Pop().envelope, nil
	}
	panic("unreachable")
}

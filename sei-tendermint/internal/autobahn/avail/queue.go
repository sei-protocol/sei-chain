package avail

import (
	"golang.org/x/exp/constraints"
)

// queue is a collection of objects of type T, indexed by type I in range [first,next).
// Supports pushing new items to the back and popping items from the front.
type queue[I constraints.Integer, T any] struct {
	q     map[I]T
	first I
	next  I
}

func newQueue[I ~uint64, T any]() *queue[I, T] {
	return &queue[I, T]{q: map[I]T{}, first: 0, next: 0}
}

func (q *queue[I, T]) First() I { return q.first }
func (q *queue[I, T]) Next() I { return q.next }
func (q *queue[I, T]) Get(i I) T { return q.q[i] }
func (q *queue[I, T]) Len() I { return q.next - q.first }

func (q *queue[I, T]) PushBack(t T) {
	q.q[q.next] = t
	q.next += 1
}

func (q *queue[I, T]) Prune(newFirst I) {
	if newFirst <= q.first {
		return
	}
	for i, n := q.first, min(q.next, newFirst); i < n; i += 1 {
		delete(q.q, i)
	}
	q.first = newFirst
	q.next = max(q.next, q.first)
}

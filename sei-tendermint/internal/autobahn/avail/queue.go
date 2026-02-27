package avail

// queue is a collection of objects of type T, indexed by type I in range [first,next).
// Supports pushing new items to the back and popping items from the front.
type queue[I ~uint64, T any] struct {
	q     map[I]T
	first I
	next  I
}

func newQueue[I ~uint64, T any]() *queue[I, T] {
	return &queue[I, T]{q: map[I]T{}, first: 0, next: 0}
}

func (q *queue[I, T]) Len() uint64 {
	return uint64(q.next) - uint64(q.first)
}

// reset sets the starting position of an empty queue.
func (q *queue[I, T]) reset(start I) {
	q.first = start
	q.next = start
}

func (q *queue[I, T]) pushBack(t T) {
	q.q[q.next] = t
	q.next += 1
}

func (q *queue[I, T]) prune(newFirst I) {
	if newFirst <= q.first {
		return
	}
	for i, n := q.first, min(q.next, newFirst); i < n; i += 1 {
		delete(q.q, i)
	}
	q.first = newFirst
	q.next = max(q.next, q.first)
}

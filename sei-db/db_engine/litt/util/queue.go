package util

import "fmt"

// A standard generic queue.
//
// This struct is not thread safe.
type Queue[T any] struct {
	// The underlying data
	data *RandomAccessDeque[T]
}

// Creates a new Queue with the given initial capacity.
func NewQueue[T any](initialCapacity uint64) *Queue[T] {
	return &Queue[T]{
		data: NewRandomAccessDeque[T](initialCapacity),
	}
}

// Push an item onto the queue.
func (q *Queue[T]) Push(item T) {
	q.data.PushBack(item)
}

// Pop an item off the queue. Panics if the queue is empty.
func (q *Queue[T]) Pop() T {
	return q.data.PopFront()
}

// TryPop tries to pop an item off the queue. Returns the item and true if successful, or the zero value
// and false if the queue is empty.
func (q *Queue[T]) TryPop() (item T, ok bool) {
	return q.data.TryPopFront()
}

// Peek at the item at the front of the queue without removing it. Panics if the queue is empty.
func (q *Queue[T]) Peek() T {
	return q.data.PeekFront()
}

// TryPeek tries to peek at the item at the front of the queue without removing it. Returns the item and true
// if successful, or the zero value and false if the queue is empty.
func (q *Queue[T]) TryPeek() (item T, ok bool) {
	return q.data.TryPeekFront()
}

// Returns the number of items in the queue.
func (q *Queue[T]) Size() uint64 {
	return uint64(q.data.Len()) //nolint:gosec // length is non-negative
}

// Returns true if the queue is empty.
func (q *Queue[T]) IsEmpty() bool {
	return q.data.IsEmpty()
}

// Clears all items from the queue.
func (q *Queue[T]) Clear() {
	q.data.Clear()
}

// Get an iterator over the elements in the queue.
func (q *Queue[T]) Iterator() func(yield func(uint64, T) bool) {
	return func(yield func(uint64, T) bool) {
		for i, v := range q.data.Forward() {
			if !yield(uint64(i), v) { //nolint:gosec // index is non-negative
				return
			}
		}
	}
}

// Get an item at the given index in the queue. Panics if the index is out of bounds.
func (q *Queue[T]) Get(index uint64) T {
	q.checkBounds(index)
	return q.data.Get(int(index)) //nolint:gosec // bounds-checked above, fits int
}

// Set the item at the given index in the queue. Panics if the index is out of bounds.
func (q *Queue[T]) Set(index uint64, value T) (previousValue T) {
	q.checkBounds(index)
	i := int(index) //nolint:gosec // bounds-checked above, fits int
	previousValue = q.data.Get(i)
	q.data.Set(i, value)
	return previousValue
}

// checkBounds panics if index is outside the queue. Guarding before converting to int matters: a
// huge index (e.g. from wrapped uint64 arithmetic) would convert to a negative int and resolve
// through the deque's Python-style negative indexing to the wrong element instead of failing fast.
func (q *Queue[T]) checkBounds(index uint64) {
	if index >= q.Size() {
		panic(fmt.Sprintf("queue index %d out of range for queue of size %d", index, q.Size()))
	}
}

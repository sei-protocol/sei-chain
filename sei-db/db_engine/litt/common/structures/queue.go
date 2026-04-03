package structures

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
	return q.data.Size()
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
	return q.data.Iterator()
}

// Get an item at the given index in the queue. Panics if the index is out of bounds.
func (q *Queue[T]) Get(index uint64) T {
	return q.data.Get(index)
}

// Set the item at the given index in the queue. Panics if the index is out of bounds.
func (q *Queue[T]) Set(index uint64, value T) (previousValue T) {
	return q.data.Set(index, value)
}

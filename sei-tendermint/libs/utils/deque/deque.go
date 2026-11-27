package deque

import "github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/im"

// Deque is an append/prune queue with read-only snapshots. Its zero value is empty.
// It must not be copied after first use; access to the deque requires external synchronization.
// Len and At are O(1); PushBack and PopFront are amortized O(1).
//
// Growth reserves twice the live element count, with a minimum capacity of one.
// Across all reachable snapshots of one deque, backing storage is O(newest-oldest),
// where [oldest, newest) spans their accessible logical insertion positions.
// Snapshots require O(1) additional space each and may be read concurrently with mutations.
//
// Popping does not clear slots. Like inaccessible slice elements that have not
// been cleared, popped elements keep any referenced objects alive while their
// backing allocation remains reachable, even if no snapshot can access them.
type Deque[T any] struct {
	// deque elements are data[head:]
	data []T
	head int
}

// Len returns the number of elements.
func (q *Deque[T]) Len() int { return len(q.data) - q.head }

// At returns the element at i. It panics if i is outside [0, Len()).
func (q *Deque[T]) At(i int) T { return q.data[q.head:][i] }

// Snapshot returns a read-only view of the current elements in O(1) time.
func (q *Deque[T]) Snapshot() im.Slice[T] { return im.NewSlice(q.data[q.head:]) }

// PushBack appends value.
func (q *Deque[T]) PushBack(value T) {
	if len(q.data) == cap(q.data) {
		n := q.Len()
		// Allocate fresh storage so existing snapshots remain unchanged.
		data := make([]T, n, max(1, 2*n))
		copy(data, q.data[q.head:])
		*q = Deque[T]{data: data}
	}
	tail := len(q.data)
	q.data = q.data[:tail+1]
	q.data[tail] = value
}

// PopFront removes and returns the first element. It panics if the deque is empty.
func (q *Deque[T]) PopFront() T {
	// An empty deque has no buffer, so this read also checks for emptiness.
	value := q.data[q.head]
	q.head++
	if n := len(q.data) - q.head; q.head >= n {
		if n == 0 {
			*q = Deque[T]{}
			return value
		}
		// Copy into a new allocation: snapshots still read the discarded prefix.
		// Leave headroom so the next push does not immediately allocate again.
		data := make([]T, n, n+max(1, n/4))
		copy(data, q.data[q.head:])
		*q = Deque[T]{data: data}
	}
	return value
}

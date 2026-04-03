package structures

import (
	"math"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/common/enforce"
)

// The minimum initial capacity of a RandomAccessDeque.
const minimumInitialCapacity = 32

// A double-ended queue (deque) that supports O(1) lookup by index.
//
// - Insertion time: O(1) average, O(n) worst-case (when resizing is needed)
// - Deletion time: O(1) average, array space is not reclaimed
// - Lookup time by index: O(1)
// - Iteration: O(1) to build iterator, O(1) per step
//
// This data structure is not thread safe.
type RandomAccessDeque[T any] struct {
	// The current number of elements in the deque.
	size uint64
	// Underlying data storage
	data []T
	// The index in data that corresponds to the logical start of the deque.
	startIndex uint64
	// The index in data that corresponds to the logical end of the deque (one past the last element).
	endIndex uint64
	// The initial capacity of the deque. Used when calling Clear().
	initialCapacity uint64
}

// Create a new RandomAccessDeque with the specified initial capacity. Queue can grow beyond this capacity if needed.
func NewRandomAccessDeque[T any](initialCapacity uint64) *RandomAccessDeque[T] {

	if initialCapacity < minimumInitialCapacity {
		initialCapacity = minimumInitialCapacity
	}

	return &RandomAccessDeque[T]{
		data:            make([]T, initialCapacity),
		initialCapacity: initialCapacity,
	}
}

// Get the number of elements in the deque.
//
// O(1)
func (s *RandomAccessDeque[T]) Size() uint64 {
	return s.size
}

// Syntactic sugar for Size() == 0
//
// O(1)
func (s *RandomAccessDeque[T]) IsEmpty() bool {
	return s.size == 0
}

// Insert a value at the front of the deque. This value will have index 0 after insertion, and all other values will
// have their indices increased by 1.
//
// O(1) average, O(n) worst-case (when resizing is needed)
func (s *RandomAccessDeque[T]) PushFront(value T) {
	s.resizeForInsertion()

	if s.startIndex == 0 {
		// wrap around
		s.startIndex = uint64(len(s.data)) - 1
	} else {
		s.startIndex--
	}

	s.data[s.startIndex] = value
	s.size++
}

// Return the value at the front of the deque without removing it. Panics if the deque is empty.
//
// O(1)
func (s *RandomAccessDeque[T]) PeekFront() T {
	value, ok := s.TryPeekFront()
	enforce.True(ok, "cannot peek front: deque is empty")
	return value
}

// Return the value at the front of the deque without removing it. If the deque is empty, returns ok==false.
//
// O(1)
func (s *RandomAccessDeque[T]) TryPeekFront() (value T, ok bool) {
	return s.TryGet(0)
}

// Remove and return the value at the front of the deque. Panics if the deque is empty.
//
// O(1)
func (s *RandomAccessDeque[T]) PopFront() T {
	value, ok := s.TryPopFront()
	enforce.True(ok, "cannot pop front: deque is empty")
	return value
}

// Remove and return the value at the front of the deque. If the deque is empty, returns ok==false.
//
// O(1)
func (s *RandomAccessDeque[T]) TryPopFront() (value T, ok bool) {
	if s.IsEmpty() {
		var zero T
		return zero, false
	}

	value = s.data[s.startIndex]

	var zero T
	s.data[s.startIndex] = zero

	if s.startIndex == uint64(len(s.data)-1) {
		// wrap around
		s.startIndex = 0
	} else {
		s.startIndex++
	}

	s.size--

	return value, true
}

// Insert a value at the back of the deque. This value will have index Size()-1 after insertion.
//
// O(1) average, O(n) worst-case (when resizing is needed)
func (s *RandomAccessDeque[T]) PushBack(value T) {
	s.resizeForInsertion()

	s.data[s.endIndex] = value

	if s.endIndex == uint64(len(s.data)-1) {
		// wrap around
		s.endIndex = 0
	} else {
		s.endIndex++
	}

	s.size++
}

// Return the value at the back of the deque without removing it. Panics if the deque is empty.
//
// O(1)
func (s *RandomAccessDeque[T]) PeekBack() T {
	value, ok := s.TryPeekBack()
	enforce.True(ok, "cannot peek back: deque is empty")
	return value
}

// Return the value at the back of the deque without removing it. If the deque is empty, returns ok==false.
//
// O(1)
func (s *RandomAccessDeque[T]) TryPeekBack() (value T, ok bool) {
	if s.IsEmpty() {
		var zero T
		return zero, false
	}
	return s.TryGet(s.size - 1)
}

// Remove and return the value at the back of the deque. Panics if the deque is empty.
//
// O(1)
func (s *RandomAccessDeque[T]) PopBack() T {
	value, ok := s.TryPopBack()
	enforce.True(ok, "cannot pop back: deque is empty")
	return value
}

// Remove and return the value at the back of the deque. If the deque is empty, returns ok==false.
//
// O(1)
func (s *RandomAccessDeque[T]) TryPopBack() (value T, ok bool) {
	if s.IsEmpty() {
		var zero T
		return zero, false
	}

	var backIndex uint64
	if s.endIndex == 0 {
		backIndex = uint64(len(s.data)) - 1
	} else {
		backIndex = s.endIndex - 1
	}

	value = s.data[backIndex]

	var zero T
	s.data[backIndex] = zero

	s.endIndex = backIndex

	s.size--

	return value, true
}

// Get the value at the specified index. Panics if the index is out of bounds.
//
// O(1)
func (s *RandomAccessDeque[T]) Get(index uint64) T {
	value, ok := s.TryGet(index)
	enforce.True(ok, "index %d out of bounds (size %d)", index, s.size)
	return value
}

// Get the value at the specified index. If the index is out of bounds, returns ok==false.
//
// O(1)
func (s *RandomAccessDeque[T]) TryGet(index uint64) (value T, ok bool) {
	if index >= s.size {
		var zero T
		return zero, false
	}

	realIndex := (s.startIndex + index) % uint64(len(s.data))
	return s.data[realIndex], true
}

// Get an element indexed from the last thing in the deque. Equivalent to Get(Size() - 1 - index).
// Panics if the index is out of bounds.
//
// O(1)
func (s *RandomAccessDeque[T]) GetFromBack(index uint64) T {
	value, ok := s.TryGetFromBack(index)
	enforce.True(ok, "index %d out of bounds (size %d)", index, s.size)
	return value
}

// Get an element indexed from the last thing in the deque. Equivalent to TryGet(Size() - 1 - index).
// If the index is out of bounds, returns ok==false.
//
// O(1)
func (s *RandomAccessDeque[T]) TryGetFromBack(index uint64) (value T, ok bool) {
	if index >= s.size {
		var zero T
		return zero, false
	}
	return s.TryGet(s.size - 1 - index)
}

// Set the value at the specified index, replacing the existing value, which is returned.
// Panics if the index is out of bounds.
//
// O(1)
func (s *RandomAccessDeque[T]) Set(index uint64, value T) T {
	previousValue, ok := s.TrySet(index, value)
	enforce.True(ok, "index %d out of bounds (size %d)", index, s.size)
	return previousValue
}

// Set the value at the specified index, replacing the existing value, which is returned.
// If the index is out of bounds, returns ok==false.
//
// O(1)
func (s *RandomAccessDeque[T]) TrySet(index uint64, value T) (previousValue T, ok bool) {
	if index >= s.size {
		var zero T
		return zero, false
	}

	realIndex := (s.startIndex + index) % uint64(len(s.data))
	previousValue = s.data[realIndex]
	s.data[realIndex] = value
	return previousValue, true
}

// Set an element indexed from the last thing in the deque, replacing the existing value, which is returned.
// Equivalent to Set(Size() - 1 - index, value).
// Panics if the index is out of bounds.
//
// O(1)
func (s *RandomAccessDeque[T]) SetFromBack(index uint64, value T) T {
	previousValue, ok := s.TrySetFromBack(index, value)
	enforce.True(ok, "index %d out of bounds (size %d)", index, s.size)
	return previousValue
}

// Set an element indexed from the last thing in the deque, replacing the existing value, which is returned.
// Equivalent to TrySet(Size() - 1 - index, value).
// If the index is out of bounds, returns ok==false.
//
// O(1)
func (s *RandomAccessDeque[T]) TrySetFromBack(index uint64, value T) (previousValue T, ok bool) {
	if index >= s.size {
		var zero T
		return zero, false
	}
	return s.TrySet(s.size-1-index, value)
}

// Clear all elements from the deque. Reclaims space in the underlying array.
//
// O(1)
func (s *RandomAccessDeque[T]) Clear() {
	s.startIndex = 0
	s.endIndex = 0
	s.size = 0
	// Reset the underlying array to allow garbage collection of contained elements.
	s.data = make([]T, s.initialCapacity)
}

// Get an iterator over the elements in the deque, from front to back. It is not safe to get an iterator,
// modify the deque, and then use the iterator again.
//
// O(1) to call this method, O(1) per iteration step.
func (s *RandomAccessDeque[T]) Iterator() func(yield func(uint64, T) bool) {
	if s.size == 0 {
		return func(yield func(uint64, T) bool) {
			// no-op
		}
	}

	return s.IteratorFrom(0)
}

// Get an iterator over the elements in the deque, from the specified index to back. It is not safe to get an iterator,
// modify the deque, and then use the iterator again.
// Panics if the index is out of bounds.
//
// O(1) to call this method, O(1) per iteration step.
func (s *RandomAccessDeque[T]) IteratorFrom(index uint64) func(yield func(uint64, T) bool) {
	iterator, ok := s.TryIteratorFrom(index)
	enforce.True(ok, "index %d out of bounds (size %d)", index, s.size)
	return iterator
}

// Get an iterator over the elements in the deque, from the specified index to back. It is not safe to get an iterator,
// modify the deque, and then use the iterator again.
// If the index is out of bounds, returns ok==false.
//
// O(1) to call this method, O(1) per iteration step.
func (s *RandomAccessDeque[T]) TryIteratorFrom(index uint64) (func(yield func(uint64, T) bool), bool) {
	if index >= s.size {
		return nil, false
	}

	return func(yield func(uint64, T) bool) {
		for i := index; i < s.size; i++ {
			if !yield(i, s.Get(i)) {
				return
			}
		}
	}, true
}

// Get an iterator over the elements in the deque, from back to front. It is not safe to get an iterator,
// modify the deque, and then use the iterator again.
//
// O(1) to call this method, O(1) per iteration step.
func (s *RandomAccessDeque[T]) ReverseIterator() func(yield func(uint64, T) bool) {
	if s.size == 0 {
		return func(yield func(uint64, T) bool) {
			// no-op
		}
	}

	return s.ReverseIteratorFrom(s.size - 1)
}

// Get an iterator over the elements in the deque, from the specified index to front. It is not safe to get an iterator,
// modify the deque, and then use the iterator again.
// Panics if the index is out of bounds.
//
// O(1) to call this method, O(1) per iteration step.
func (s *RandomAccessDeque[T]) ReverseIteratorFrom(index uint64) func(yield func(uint64, T) bool) {
	iterator, ok := s.TryReverseIteratorFrom(index)
	enforce.True(ok, "index %d out of bounds (size %d)", index, s.size)
	return iterator
}

// Get an iterator over the elements in the deque, from the specified index to front. It is not safe to get an iterator,
// modify the deque, and then use the iterator again.
// If the index is out of bounds, returns ok==false.
//
// O(1) to call this method, O(1) per iteration step.
func (s *RandomAccessDeque[T]) TryReverseIteratorFrom(index uint64) (func(yield func(uint64, T) bool), bool) {
	if index >= s.size {
		return nil, false
	}

	return func(yield func(uint64, T) bool) {
		for i := index; i != math.MaxUint64; i-- {
			if !yield(i, s.Get(i)) {
				return
			}
		}
	}, true
}

// Resize the underlying array to accommodate at least one more insertion. Preserves existing elements.
// If no resizing is needed, this is a no-op.
func (s *RandomAccessDeque[T]) resizeForInsertion() {
	remainingCapacity := uint64(len(s.data)) - s.size

	if remainingCapacity > 0 {
		return
	}

	newData := make([]T, len(s.data)*2)

	for index, value := range s.Iterator() {
		newData[index] = value
	}

	s.data = newData
	s.startIndex = 0
	s.endIndex = s.size
}

// Perform a binary search in the deque for an element matching the compare function. Assumes that
// the deque is sorted according to the same compare function. If an exact match can't be found,
// returns the index of the location where the value would be inserted if it were inserted in the proper location.
//
// The compare function `compare(a V, b T) int` should return:
//   - negative value if a < b
//   - zero if a == b
//   - positive value if a > b
//
// If the deque is not sorted or if the ordering is not a total ordering, the return value is undefined. This function
// is not defined as a method on RandomAccessDeque due to this fact. Not all RandomAccessDeque instances will be sorted,
// and so this function is not always valid to call.
func BinarySearchInOrderedDeque[V any, T any](
	deque *RandomAccessDeque[T],
	value V,
	compare func(a V, b T) int) (index uint64, exact bool) {

	if deque.size == 0 {
		return 0, false
	}

	// Index is the external index in the deque, from 0 to size-1, not indices as they
	// appear in the underlying array.
	left := uint64(0)
	right := deque.size - 1
	var targetIndex uint64

	for left < right {
		targetIndex = left + (right-left)/2
		target := deque.Get(targetIndex)

		cmp := compare(value, target)

		if cmp == 0 {
			// We've found an exact match.
			return targetIndex, true
		} else if cmp < 0 {
			// value < target, search left half
			//
			//      value is here
			//  |-----------------------|-----------------------|
			// left                   target                  right
			if targetIndex == 0 {
				right = 0
			} else {
				right = targetIndex - 1
			}
		} else {
			// value > target, search right half
			//
			//                               value is here
			//  |-----------------------|-----------------------|
			// left                   target                  right
			left = targetIndex + 1
		}
	}

	element := deque.Get(left)
	cmp := compare(value, element)
	if cmp == 0 {
		// We've found an exact match.
		return left, true
	} else if cmp < 0 {
		// value < element, so missing value should go to the left of it
		return left, false
	}
	// value > element, so missing value should go to the right of it
	return left + 1, false
}

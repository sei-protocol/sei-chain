package dbcache

import (
	"fmt"
	"iter"
	"math/bits"
)

const minDequeCapacity = 8

type Deque[T any] struct {
	data []T
	// len(data) - 1; Used to do a cheap modulo since capacity is always a power of 2.
	mask       int
	firstIndex int
	size       int
}

func NewDeque[T any]() *Deque[T] {
	return NewDequeWithCapacity[T](minDequeCapacity)
}

func NewDequeWithCapacity[T any](capacity int) *Deque[T] {
	c := nextPowerOf2(capacity)
	if c < minDequeCapacity {
		c = minDequeCapacity
	}
	return &Deque[T]{
		data: make([]T, c),
		mask: c - 1,
	}
}

// Push a value onto the front of the deque. This value will have index 0.
func (r *Deque[T]) PushFront(value T) {
	r.growIfFull()
	r.firstIndex = (r.firstIndex - 1) & r.mask
	r.data[r.firstIndex] = value
	r.size++
}

// Push a value onto the back of the deque. This value will have index Len() - 1, or -1 if using negative indexing.
func (r *Deque[T]) PushBack(value T) {
	r.growIfFull()
	r.data[(r.firstIndex+r.size)&r.mask] = value
	r.size++
}

// PopFront pops a value off the front of the deque. Panics if the deque is empty.
func (r *Deque[T]) PopFront() T {
	if r.size == 0 {
		panic("PopFront called on empty deque")
	}
	var zero T
	value := r.data[r.firstIndex]
	r.data[r.firstIndex] = zero
	r.firstIndex = (r.firstIndex + 1) & r.mask
	r.size--
	return value
}

// TryPopFront pops a value off the front of the deque. Returns the value and true if the deque
// is not empty, otherwise returns the zero value and false.
func (r *Deque[T]) TryPopFront() (T, bool) {
	if r.size == 0 {
		var zero T
		return zero, false
	}
	return r.PopFront(), true
}

// PopBack pops a value off the back of the deque. Panics if the deque is empty.
func (r *Deque[T]) PopBack() T {
	if r.size == 0 {
		panic("PopBack called on empty deque")
	}
	var zero T
	backIdx := (r.firstIndex + r.size - 1) & r.mask
	value := r.data[backIdx]
	r.data[backIdx] = zero
	r.size--
	return value
}

// TryPopBack pops a value off the back of the deque. Returns the value and true if the deque
// is not empty, otherwise returns the zero value and false.
func (r *Deque[T]) TryPopBack() (T, bool) {
	if r.size == 0 {
		var zero T
		return zero, false
	}
	return r.PopBack(), true
}

// PeekFront returns the value at the front of the deque. Panics if the deque is empty.
func (r *Deque[T]) PeekFront() T {
	if r.size == 0 {
		panic("PeekFront called on empty deque")
	}
	return r.data[r.firstIndex]
}

// TryPeekFront returns the value at the front of the deque and true if the deque is not empty,
// otherwise returns the zero value and false.
func (r *Deque[T]) TryPeekFront() (T, bool) {
	if r.size == 0 {
		var zero T
		return zero, false
	}
	return r.data[r.firstIndex], true
}

// PeekBack returns the value at the back of the deque. Panics if the deque is empty.
func (r *Deque[T]) PeekBack() T {
	if r.size == 0 {
		panic("PeekBack called on empty deque")
	}
	return r.data[(r.firstIndex+r.size-1)&r.mask]
}

// TryPeekBack returns the value at the back of the deque and true if the deque is not empty,
// otherwise returns the zero value and false.
func (r *Deque[T]) TryPeekBack() (T, bool) {
	if r.size == 0 {
		var zero T
		return zero, false
	}
	return r.data[(r.firstIndex+r.size-1)&r.mask], true
}

// Get the length of the deque.
func (r *Deque[T]) Len() int {
	return r.size
}

// Check if the deque is empty.
func (r *Deque[T]) IsEmpty() bool {
	return r.size == 0
}

// Clear the deque.
func (r *Deque[T]) Clear() {
	if r.size == 0 {
		return
	}
	end := r.firstIndex + r.size
	if end <= len(r.data) {
		clear(r.data[r.firstIndex:end])
	} else {
		clear(r.data[r.firstIndex:])
		clear(r.data[:end-len(r.data)])
	}
	r.firstIndex = 0
	r.size = 0
}

// Get the value at the given index. Panics if the index is out of range.
//
// Positive indices are relative to the front of the deque, while negative indices are relative to the back
// (similar to python list semantics).
func (r *Deque[T]) Get(index int) T {
	return r.data[r.resolveIndex(index)]
}

// Set the value at the given index. Panics if the index is out of range.
//
// Positive indices are relative to the front of the deque, while negative indices are relative to the back
// (similar to python list semantics).
func (r *Deque[T]) Set(index int, value T) {
	r.data[r.resolveIndex(index)] = value
}

// Forward returns an iterator that yields (index, value) pairs from front to back.
func (r *Deque[T]) Forward() iter.Seq2[int, T] {
	return func(yield func(int, T) bool) {
		for i := 0; i < r.size; i++ {
			if !yield(i, r.data[(r.firstIndex+i)&r.mask]) {
				return
			}
		}
	}
}

// Backward returns an iterator that yields (index, value) pairs from back to front.
// The index reflects position from front (i.e. Len()-1 down to 0).
func (r *Deque[T]) Backward() iter.Seq2[int, T] {
	return func(yield func(int, T) bool) {
		for i := r.size - 1; i >= 0; i-- {
			if !yield(i, r.data[(r.firstIndex+i)&r.mask]) {
				return
			}
		}
	}
}

func (r *Deque[T]) resolveIndex(index int) int {
	original := index
	if index < 0 {
		index += r.size
	}
	if index < 0 || index >= r.size {
		if r.size == 0 {
			panic(fmt.Sprintf("deque index %d out of range for empty deque", original))
		}
		panic(fmt.Sprintf("deque index %d out of range [%d, %d]", original, -r.size, r.size-1))
	}
	return (r.firstIndex + index) & r.mask
}

func (r *Deque[T]) growIfFull() {
	if r.size < len(r.data) {
		return
	}
	newCap := len(r.data) * 2
	newData := make([]T, newCap)
	n := copy(newData, r.data[r.firstIndex:])
	copy(newData[n:], r.data[:r.firstIndex])
	r.data = newData
	r.mask = newCap - 1
	r.firstIndex = 0
}

func nextPowerOf2(n int) int {
	if n <= 1 {
		return 1
	}
	// n is >= 2 here, so n-1 is >= 1 and safe to convert to uint.
	return 1 << bits.Len(uint(n-1)) //nolint:gosec
}

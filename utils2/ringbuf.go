package utils

import (
	"iter"
)

// RingBuf is a ring buffer.
// NOT thread-safe.
type RingBuf[T any] struct {
	first int
	len   int
	buf   []T
}

// NewRingBuf creates a new ring buffer with the given capacity.
func NewRingBuf[T any](capacity int) RingBuf[T] {
	return RingBuf[T]{first: 0, len: 0, buf: make([]T, capacity)}
}

// Len returns the number of elements in the ring buffer.
func (r *RingBuf[T]) Len() int {
	return r.len
}

// Full returns true if the ring buffer is full.
func (r *RingBuf[T]) Full() bool {
	return r.len == len(r.buf)
}

// Get returns the i-th element of the ring buffer.
// Panics if i is out of range.
func (r *RingBuf[T]) Get(i int) T {
	if i < 0 || i >= r.len {
		panic("index out of range")
	}
	return r.buf[(r.first+i)%len(r.buf)]
}

// TryGet returns the i-th element of the ring buffer.
func (r *RingBuf[T]) TryGet(i int) (T, bool) {
	if i < 0 || i >= r.len {
		return Zero[T](), false
	}
	return r.buf[(r.first+i)%len(r.buf)], true
}

// Last returns the last element of the ring buffer.
func (r *RingBuf[T]) Last() (T, bool) {
	return r.TryGet(r.len - 1)
}

// PushBack adds an element to the back of the ring buffer.
// Panics if the ring buffer is full.
func (r *RingBuf[T]) PushBack(x T) {
	if r.len == len(r.buf) {
		panic("ring buffer full")
	}
	r.buf[(r.first+r.len)%len(r.buf)] = x
	r.len += 1
}

// PopFront removes and returns the first element of the ring buffer.
// Panics if the ring buffer is empty.
func (r *RingBuf[T]) PopFront() T {
	if r.len == 0 {
		panic("ring buffer empty")
	}
	x := r.buf[r.first]
	r.first = (r.first + 1) % len(r.buf)
	r.len -= 1
	return x
}

// All iterates over all the elements in the ring buffer.
func (r *RingBuf[T]) All() iter.Seq[T] {
	return func(y func(T) bool) {
		for i := range r.len {
			if !y(r.Get(i)) {
				break
			}
		}
	}
}

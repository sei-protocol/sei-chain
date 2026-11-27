package im

import (
	"iter"
	"slices"
)

// Slice is a read-only slice. Its zero value is empty.
type Slice[T any] struct{ s []T }

// NewSlice returns a read-only view of s without copying its elements.
// The elements of s must remain unchanged while the view is in use.
func NewSlice[T any](s []T) Slice[T] { return Slice[T]{s: s} }

func (s Slice[T]) Len() int         { return len(s.s) }
func (s Slice[T]) At(i int) T       { return s.s[i] }
func (s Slice[T]) All() iter.Seq[T] { return slices.Values(s.s) }

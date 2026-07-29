package util

import "github.com/sei-protocol/sei-chain/sei-db/common/structures"

// RandomAccessDeque is a backward-compatibility alias for structures.Deque. The deque
// implementation moved to the shared structures package; this alias is retained so existing
// callers compile unchanged. Prefer structures.Deque directly in new code.
type RandomAccessDeque[T any] = structures.Deque[T]

// NewRandomAccessDeque creates a structures.Deque with the given initial capacity. Retained as a
// compatibility shim for the former util deque constructor.
func NewRandomAccessDeque[T any](initialCapacity uint64) *structures.Deque[T] {
	// Callers pass lengths or constants. A value that did overflow int would only cost the capacity
	// hint: the deque falls back to its minimum size and grows on demand.
	return structures.NewDequeWithCapacity[T](int(initialCapacity)) //nolint:gosec
}

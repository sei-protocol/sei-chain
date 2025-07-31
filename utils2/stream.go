package utils

import (
	"context"
)

// streamState holds the state of a stream.
// Note that the underlying slice is append-only and only
// modifiable by the StreamSend.
// The copies of streamState are never leaving StreamRecv methods,
// so even if the slice is copied (during append in Send)
// the memory overhead is temporary.
type streamState[T any] struct {
	first int
	values []T
}

// StreamSend is the sending end of a stream.
// NON-thread-safe.
type StreamSend[T any] struct {
	a AtomicSend[streamState[T]]
}

func NewStreamSend[T any]() StreamSend[T] {
	return StreamSend[T]{a:NewAtomicSend(streamState[T]{first:0,values:nil})}
}

// StreamRecv is the receiving end of a stream.
// NON-thread-safe.
type StreamRecv[T any] struct {
	_ NoCopy
	next int
	a AtomicRecv[streamState[T]]
}

// Send sends a message to the stream.
// NOTE: rembember to call Reset() whenever old messages are not needed anymore.
func (b *StreamSend[T]) Send(value T) {
	b.a.Update(func(state streamState[T]) (streamState[T],bool) {
		state.values = append(state.values, value)
		return state,true
	})
}

// Reset prunes all the messages in the stream.
// Receivers will skip the pruned messages.
func (s *StreamSend[T]) Reset() {
	s.a.Update(func(state streamState[T]) (streamState[T],bool) {
		state.first += len(state.values)
		state.values = nil
		return state,true
	})
}

// Subscribe subscribes to the stream.
func (s *StreamSend[T]) Subscribe() *StreamRecv[T] {
	return &StreamRecv[T]{
		next: 0,
		a: s.a.Subscribe(),
	}
}

// Resets the receiver to the beginning of the stream.
func (r *StreamRecv[T]) Reset() {
	r.next = 0
}

// Returns the next message available in the stream.
// All sent messages will be returned in order, except for those
// pruned by the sender Reset() method.
func (r *StreamRecv[T]) Recv(ctx context.Context) (T, error) {
	state, err := r.a.Wait(ctx, func(state streamState[T]) bool {
		return 0<len(state.values) && r.next < state.first+len(state.values)
	})
	if err != nil {
		return Zero[T](), err
	}
	if r.next < state.first {
		r.next = state.first
	}
	value := state.values[r.next-state.first]
	r.next += 1
	return value, nil
} 


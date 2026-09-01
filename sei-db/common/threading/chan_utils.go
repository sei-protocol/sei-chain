package threading

import (
	"context"
	"fmt"
)

// Push to a channel, returning an error if the context is cancelled before the value is pushed.
func InterruptiblePush[T any](ctx context.Context, ch chan T, value T) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("context cancelled: %w", ctx.Err())
	case ch <- value:
		return nil
	}
}

// Wait for a channel to be closed (or to yield a value), returning an error if the context is
// cancelled first. Unlike InterruptiblePull, a closed channel is the success case, so this suits a
// channel used purely to broadcast readiness.
func InterruptibleWait(ctx context.Context, ch <-chan struct{}) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("context cancelled: %w", ctx.Err())
	case <-ch:
		return nil
	}
}

// Pull from a channel, returning an error if the context is cancelled before the value is pulled.
func InterruptiblePull[T any](ctx context.Context, ch <-chan T) (T, error) {
	var zero T
	select {
	case <-ctx.Done():
		return zero, fmt.Errorf("context cancelled: %w", ctx.Err())
	case value, ok := <-ch:
		if !ok {
			return zero, fmt.Errorf("channel closed")
		}
		return value, nil
	}
}

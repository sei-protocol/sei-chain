package utils

import (
	"context"
	"fmt"
)

// TODO unit test before merge

// Push to a channel, returning an error if the context is cancelled before the value is pushed.
func InterruptiblePush[T any](ctx context.Context, ch chan T, value T) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("context cancelled: %w", ctx.Err())
	case ch <- value:
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

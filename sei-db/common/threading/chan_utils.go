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

// Pull from a channel, returning an error if the context is cancelled before a value is available or the channel
// is closed. A value already waiting is returned even after cancellation, so it suits one-shot reply channels
// rather than loops over a channel that is never empty, which would never observe the cancellation.
func InterruptiblePull[T any](ctx context.Context, ch <-chan T) (T, error) {
	var zero T
	select {
	case <-ctx.Done():
		// Both cases may be ready and select picks at random, so prefer a value sent before the cancellation.
		select {
		case value, ok := <-ch:
			if ok {
				return value, nil
			}
		default:
		}
		return zero, fmt.Errorf("context cancelled: %w", ctx.Err())
	case value, ok := <-ch:
		if !ok {
			return zero, fmt.Errorf("channel closed")
		}
		return value, nil
	}
}

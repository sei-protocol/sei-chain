package utils

import (
	"context"

	"github.com/pkg/errors"
)

// Recv receives a value from a channel or returns an error if the context is canceled.
func Recv[T any](ctx context.Context, ch <-chan T) (zero T, err error) {
	select {
	case v, ok := <-ch:
		if ok {
			return v, nil
		}
		// We are not interested in channel closing,
		// patiently wait for the context to be done instead.
		<-ctx.Done()
		return zero, ctx.Err()
	case <-ctx.Done():
		return zero, ctx.Err()
	}
}

// RecvOrClosed receives a value from a channel, returns false if channel got closed,
// or returns an error if the context is canceled.
func RecvOrClosed[T any](ctx context.Context, ch <-chan T) (T, bool, error) {
	select {
	case v, ok := <-ch:
		return v, ok, nil
	case <-ctx.Done():
		var zero T
		return zero, false, ctx.Err()
	}
}

// Send a value to channel or returns an error if the context is canceled.
func Send[T any](ctx context.Context, ch chan<- T, v T) error {
	select {
	case ch <- v:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SendOrDrop send a value to channel if not full or drop the item if the channel is full.
func SendOrDrop[T any](ch chan<- T, v T) error {
	select {
	case ch <- v:
		return nil
	default:
		// drop the item
		return nil
	}
}

// ForEach is a helper function that reads from a channel and calls a handler for each item.
// this avoids needing a lot of for/select boilerplate everywhere.
func ForEach[T any](ctx context.Context, ch <-chan T, handler func(T) error) error {
	for {
		select {
		case <-ctx.Done():
			return errors.WithStack(ctx.Err())
		case item, ok := <-ch:
			if !ok {
				return nil // Channel closed
			}
			if err := handler(item); err != nil {
				return err // Stop on error
			}
		}
	}
}

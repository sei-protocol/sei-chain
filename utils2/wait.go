package utils

import (
	"context"
	"encoding"
	"errors"
	"time"
)

// IgnoreCancel returns nil if the error is context.Canceled, err otherwise.
func IgnoreCancel(err error) error {
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// WithTimeout executes a function with a timeout.
func WithTimeout(ctx context.Context, d time.Duration, f func(ctx context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	return f(ctx)
}

// WithTimeout1 executes a function with a timeout.
func WithTimeout1[R any](ctx context.Context, d time.Duration, f func(ctx context.Context) (R, error)) (R, error) {
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	return f(ctx)
}

// Sleep sleeps for a duration or until the context is canceled.
func Sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// SleepUntil sleeps until deadline t or until the context is canceled.
func SleepUntil(ctx context.Context, t time.Time) error {
	return Sleep(ctx, time.Until(t))
}

// WaitFor polls a check function until it returns true or the context is canceled.
func WaitFor(ctx context.Context, interval time.Duration, check func() bool) error {
	if check() {
		return nil
	}
	ticker := time.NewTicker(interval)
	for {
		if check() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// WaitForWithTimeout polls a check function until it returns true, the context is canceled, or the timeout is reached.
func WaitForWithTimeout(ctx context.Context, interval, timeout time.Duration, check func() bool) error {
	if check() {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if check() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// Duration is a wrapper type around time.Duration that supports JSON marshaling/unmarshaling.
// nolint:recvcheck
type Duration time.Duration

// MarshalText implements json.TextMarshaler interface to convert Duration to JSON string.
func (d Duration) MarshalText() ([]byte, error) {
	return []byte(time.Duration(d).String()), nil
}

// UnmarshalText implements json.TextUnmarshaler.
func (d *Duration) UnmarshalText(b []byte) error {
	tmp, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	*d = Duration(tmp)
	return nil
}

var _ encoding.TextMarshaler = Zero[Duration]()
var _ encoding.TextUnmarshaler = (*Duration)(nil)

// Duration returns the underlying time.Duration value.
func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

// Seconds returns the underlying time.Duration value in seconds.
func (d Duration) Seconds() float64 {
	return time.Duration(d).Seconds()
}

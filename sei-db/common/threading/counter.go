package threading

import (
	"context"
	"fmt"
	"math"
	"sync"
)

var _ ReadOnlyCounter = (*Counter)(nil)

// Counter is a thread safe, monotonically increasing uint64 that goroutines can block on until it reaches a
// target value. Once sealed, the counter never changes again.
type Counter struct {
	lock sync.Mutex

	// The current value of the counter.
	value uint64

	// Whether Seal() has been called.
	sealed bool

	// Closed to wake waiters whenever the counter changes. Replaced on every increment, and closed for good
	// when the counter is sealed.
	changed chan struct{}
}

// ReadOnlyCounter is a view of a Counter that can observe and wait on its value but cannot change it.
type ReadOnlyCounter interface {
	// Get returns the current value of the counter.
	Get() uint64

	// Await blocks until the counter reaches or exceeds target, or is sealed below it, returning immediately if
	// either has already happened. It returns an error only if the context is cancelled first.
	Await(
		ctx context.Context,
		// The value to wait for.
		target uint64,
	) (
		// True if the counter reached target, false if it was sealed below target and never will reach.
		reached bool,
		// The counter's value when Await returned: at or above target if reached, below it otherwise.
		value uint64,
		err error,
	)
}

// NewCounter creates a counter that starts at initialValue.
func NewCounter(initialValue uint64) *Counter {
	return &Counter{
		value:   initialValue,
		changed: make(chan struct{}),
	}
}

// Increment adds one to the value of the counter and returns the new value. It returns an error if the counter is
// sealed or already at its maximum value, in which case the value is unchanged.
func (c *Counter) Increment() (uint64, error) {
	return c.IncrementN(1)
}

// IncrementN adds count to the value of the counter and returns the new value. It returns an error if the counter
// is sealed or the addition would overflow, in which case the value is unchanged.
func (c *Counter) IncrementN(count uint64) (uint64, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	if c.sealed {
		return 0, fmt.Errorf("cannot increment sealed counter at value %d", c.value)
	}
	if count > math.MaxUint64-c.value {
		return 0, fmt.Errorf("incrementing counter at value %d by %d would overflow", c.value, count)
	}
	if count == 0 {
		return c.value, nil
	}

	c.value += count
	close(c.changed)
	c.changed = make(chan struct{})
	return c.value, nil
}

// Seal declares that the counter will never be incremented again. Afterwards, Increment() fails, and Await()
// reports any target above the current value as never reached, including to callers already blocked. It returns
// an error if the counter is already sealed.
func (c *Counter) Seal() error {
	c.lock.Lock()
	defer c.lock.Unlock()

	if c.sealed {
		return fmt.Errorf("counter already sealed at value %d", c.value)
	}

	c.sealed = true
	close(c.changed)
	return nil
}

// Get returns the current value of the counter.
func (c *Counter) Get() uint64 {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.value
}

// Await blocks until the counter reaches or exceeds target, or is sealed below it, returning immediately if either
// has already happened. It returns an error only if the context is cancelled first.
func (c *Counter) Await(ctx context.Context, target uint64) (bool, uint64, error) {
	for {
		c.lock.Lock()
		value := c.value
		sealed := c.sealed
		changed := c.changed
		c.lock.Unlock()

		if value >= target {
			return true, value, nil
		}
		if sealed {
			return false, value, nil
		}

		select {
		case <-ctx.Done():
			return false, value, fmt.Errorf("await counter value %d: %w", target, ctx.Err())
		case <-changed:
		}
	}
}

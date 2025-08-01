package utils

import (
	"context"
)

// Semaphore provides a way to bound concurrenct access to a resource.
type Semaphore struct {
	ch chan struct{}
}

// NewSemaphore constructs a new semaphore with n permits.
func NewSemaphore(n int) *Semaphore {
	return &Semaphore{ch: make(chan struct{}, n)}
}

// Acquire acquires a permit from the semaphore.
// Blocks until a permit is available.
func (s *Semaphore) Acquire(ctx context.Context) (relase func(), err error) {
	if err := Send(ctx, s.ch, struct{}{}); err != nil {
		return nil, err
	}
	return func() { <-s.ch }, nil
}

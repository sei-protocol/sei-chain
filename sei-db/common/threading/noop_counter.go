package threading

import (
	"context"
	"math"
)

var _ ReadOnlyCounter = NoOpCounter{}

// NoOpCounter is a ReadOnlyCounter that has already reached every target.
type NoOpCounter struct{}

// Get returns math.MaxUint64.
func (NoOpCounter) Get() uint64 {
	return math.MaxUint64
}

// Await returns immediately, reporting target as reached at math.MaxUint64.
func (NoOpCounter) Await(context.Context, uint64) (bool, uint64, error) {
	return true, math.MaxUint64, nil
}

package test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// AssertEventuallyTrue asserts that a condition is true within a given duration. Repeatably checks the condition.
func AssertEventuallyTrue(t *testing.T, condition func() bool, duration time.Duration, debugInfo ...any) {
	if len(debugInfo) == 0 {
		debugInfo = []any{"Condition did not become true within the given duration"}
	}

	ticker := time.NewTicker(1 * time.Millisecond)
	defer ticker.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	for {
		select {
		case <-ticker.C:
			if condition() {
				return
			}
		case <-ctx.Done():
			require.True(t, condition(), debugInfo...)
			return
		}
	}
}

// AssertEventuallyEquals asserts that a getter function returns the expected value within a given duration.
// Repeatedly checks the getter until it returns the expected value or the duration expires.
func AssertEventuallyEquals[T comparable](
	t *testing.T,
	expected T,
	actual func() T,
	duration time.Duration,
	debugInfo ...any,
) {
	ticker := time.NewTicker(1 * time.Millisecond)
	defer ticker.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	// keep track of the actual value, so we can report it
	var finalActual T

	for {
		select {
		case <-ticker.C:
			finalActual = actual()
			if finalActual == expected {
				return
			}
		case <-ctx.Done():
			if len(debugInfo) == 0 {
				debugInfo = []any{"Value did not equal expected within the given duration"}
			}
			require.Equal(t, expected, finalActual, debugInfo...)
			return
		}
	}
}

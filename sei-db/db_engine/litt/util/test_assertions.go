//go:build littdb_wip

package util

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

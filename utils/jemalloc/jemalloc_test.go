package jemalloc_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/utils/jemalloc"
)

// TestLinkedAllocatorAnswers checks that the reported state matches the build:
// with the jemalloc tag the linked library must identify itself and be serving
// live allocations; without it every accessor must be inert.
func TestLinkedAllocatorAnswers(t *testing.T) {
	if !jemalloc.Enabled {
		require.Empty(t, jemalloc.Version())
		require.Zero(t, jemalloc.Allocated())
		return
	}
	require.Regexp(t, `^5\.`, jemalloc.Version())
	// The Go runtime's cgo thread bootstrap and the test binary itself have
	// already gone through malloc by the time a test runs, so a jemalloc that
	// is linked but not actually serving malloc would report zero here.
	require.Positive(t, jemalloc.Allocated())
}

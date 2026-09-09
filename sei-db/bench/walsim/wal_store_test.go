package walsim

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// newTestShim opens a legacy shim backed by a temp dir with synchronous writes (so appends and
// truncations are observable deterministically) and the given prune relaxation factor.
func newTestShim(t *testing.T, pruneRelaxationFactor uint64) *legacyWALShim {
	t.Helper()
	config := DefaultWalsimConfig()
	config.Backend = "legacy"
	config.DataDir = t.TempDir()
	config.Legacy.WriteBufferSize = 0 // synchronous writes
	config.Legacy.WriteBatchSize = 1  // no batching
	config.PruneRelaxationFactor = pruneRelaxationFactor

	shim, err := newLegacyWALShim(context.Background(), config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, shim.Close()) })
	return shim
}

func appendN(t *testing.T, shim *legacyWALShim, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		// The index argument is ignored by the legacy shim; the legacy WAL assigns its own.
		require.NoError(t, shim.Append(uint64(i+1), []byte("payload")))
	}
}

func firstIndex(t *testing.T, shim *legacyWALShim) uint64 {
	t.Helper()
	ok, first, _, err := shim.Bounds()
	require.NoError(t, err)
	require.True(t, ok)
	return first
}

func TestLegacyShimBoundsEmptyThenPopulated(t *testing.T) {
	shim := newTestShim(t, 1)

	ok, _, _, err := shim.Bounds()
	require.NoError(t, err)
	require.False(t, ok, "a fresh legacy WAL must report no bounds")

	appendN(t, shim, 5)

	ok, first, last, err := shim.Bounds()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), first, "legacy WAL indices are 1-based")
	require.Equal(t, uint64(5), last)
}

func TestLegacyShimCoalescesPruneRequests(t *testing.T) {
	const relaxation = 3
	shim := newTestShim(t, relaxation)
	appendN(t, shim, 10)

	require.Equal(t, uint64(1), firstIndex(t, shim))

	// The first two prune requests are swallowed; the underlying WAL is untouched.
	require.NoError(t, shim.PruneBefore(2))
	require.Equal(t, uint64(1), firstIndex(t, shim))
	require.NoError(t, shim.PruneBefore(3))
	require.Equal(t, uint64(1), firstIndex(t, shim))

	// The third request (a multiple of the relaxation factor) is forwarded, using its own
	// lowestIndexToKeep.
	require.NoError(t, shim.PruneBefore(4))
	require.Equal(t, uint64(4), firstIndex(t, shim))

	// The cycle repeats: two more swallowed, then one forwarded.
	require.NoError(t, shim.PruneBefore(5))
	require.NoError(t, shim.PruneBefore(6))
	require.Equal(t, uint64(4), firstIndex(t, shim))
	require.NoError(t, shim.PruneBefore(7))
	require.Equal(t, uint64(7), firstIndex(t, shim))
}

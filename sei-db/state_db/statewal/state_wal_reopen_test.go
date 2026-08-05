package statewal

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFreshWALAcceptsFarAheadFirstBlock models the state-sync restore case (D7): a WAL opened on an empty
// directory accepts a first write at an arbitrary height with no contiguity error, which is what lets a node
// restored from a snapshot start writing at the restored height rather than block 1.
func TestFreshWALAcceptsFarAheadFirstBlock(t *testing.T) {
	cfg := testConfig(t.TempDir())
	w := openWAL(t, cfg)
	defer func() { require.NoError(t, w.Close()) }()

	const restoredHeight = uint64(1000)
	writeBlock(t, w, restoredHeight)
	require.NoError(t, w.Flush())

	ok, start, end, err := w.GetStoredRange()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, restoredHeight, start)
	require.Equal(t, restoredHeight, end)
}

// TestClosePruneAfterReopenResetsHead models the rollback case (D3): close, offline PruneAfter to the
// target, reopen — the stored range is truncated to the target and the write head resets, so the next write
// is contiguous at target+1.
func TestClosePruneAfterReopenResetsHead(t *testing.T) {
	cfg := testConfig(t.TempDir())
	w := openWAL(t, cfg)
	for block := uint64(1); block <= 5; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())
	require.NoError(t, w.Close())

	require.NoError(t, PruneAfter(cfg, 3))

	w2 := openWAL(t, cfg)
	defer func() { require.NoError(t, w2.Close()) }()

	ok, start, end, err := w2.GetStoredRange()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), start)
	require.Equal(t, uint64(3), end)

	writeBlock(t, w2, 4) // contiguous with the rolled-back head
	require.NoError(t, w2.Flush())
	require.Equal(t, []uint64{1, 2, 3, 4}, collectBlocks(t, w2, 1, 4))
}

// TestReopenEnforcesContiguity verifies the contiguity rule survives close and reopen: the reopened WAL
// recovers its write head from the highest stored block, so a forward jump is still rejected. Only the
// positive direction (resuming at head+1) was covered before.
func TestReopenEnforcesContiguity(t *testing.T) {
	cfg := testConfig(t.TempDir())
	w := openWAL(t, cfg)
	for block := uint64(1); block <= 3; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())
	require.NoError(t, w.Close())

	w2 := openWAL(t, cfg)
	defer func() { require.NoError(t, w2.Close()) }()

	require.Error(t, w2.Write(5, nil), "a forward jump must be rejected after reopen, not just in-session")
	writeBlock(t, w2, 4)
	require.NoError(t, w2.Flush())
	require.Equal(t, []uint64{1, 2, 3, 4}, collectBlocks(t, w2, 1, 4))
}

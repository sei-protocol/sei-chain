package statewal

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	commonerrors "github.com/sei-protocol/sei-chain/sei-db/common/errors"
)

// TestDeleteRemovesWALDirectory verifies Delete removes the WAL directory outright and that New recreates it,
// yielding a fresh, empty WAL.
func TestDeleteRemovesWALDirectory(t *testing.T) {
	cfg := testConfig(t.TempDir())
	w := openWAL(t, cfg)
	for block := uint64(1); block <= 3; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())
	require.NoError(t, w.Close())

	require.NoError(t, Delete(cfg))
	require.NoDirExists(t, cfg.Path)

	w2 := openWAL(t, cfg)
	defer func() { require.NoError(t, w2.Close()) }()
	require.DirExists(t, cfg.Path, "New must recreate the directory Delete removed")
	ok, _, _, err := w2.GetStoredRange()
	require.NoError(t, err)
	require.False(t, ok, "WAL should be empty after Delete")
}

// TestDeleteMissingDirIsNoop verifies Delete on a directory that was never created is a clean no-op that does
// not create the directory.
func TestDeleteMissingDirIsNoop(t *testing.T) {
	cfg := testConfig(filepath.Join(t.TempDir(), "never-created"))
	require.NoError(t, Delete(cfg))
	require.NoDirExists(t, cfg.Path)
}

// TestDeleteRejectedWhileWALOpen verifies Delete fails fast rather than wiping the directory out from under a
// live StateWAL, which would leave the writer appending to unlinked files.
func TestDeleteRejectedWhileWALOpen(t *testing.T) {
	cfg := testConfig(t.TempDir())
	w := openWAL(t, cfg)
	defer func() { require.NoError(t, w.Close()) }()

	writeBlock(t, w, 1)
	require.NoError(t, w.Flush())

	err := Delete(cfg)
	require.ErrorIs(t, err, commonerrors.ErrFileLockUnavailable)
	require.DirExists(t, cfg.Path)

	// The live WAL is untouched and still usable.
	writeBlock(t, w, 2)
	require.NoError(t, w.Flush())
	ok, first, last, err := w.GetStoredRange()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), first)
	require.Equal(t, uint64(2), last)
}

// TestCloseDeleteReopenAcceptsFarAheadBlock models the state-sync restore case (D7): after wiping a WAL that
// held an old chain's blocks, the reopened WAL accepts a first write at a far-ahead height with no
// contiguity error — this is why restore must wipe rather than splice.
func TestCloseDeleteReopenAcceptsFarAheadBlock(t *testing.T) {
	cfg := testConfig(t.TempDir())
	w := openWAL(t, cfg)
	for block := uint64(1); block <= 5; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())
	require.NoError(t, w.Close())

	require.NoError(t, Delete(cfg))

	w2 := openWAL(t, cfg)
	defer func() { require.NoError(t, w2.Close()) }()

	const restoredHeight = uint64(1000)
	writeBlock(t, w2, restoredHeight) // fresh WAL accepts any first block
	require.NoError(t, w2.Flush())

	ok, start, end, err := w2.GetStoredRange()
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

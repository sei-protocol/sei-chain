package giga_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
)

// Registration has to reach the layer that hashes blocks. A StateDB that answered for itself would
// hand back a hash nothing produced, and a listener that was never called.
func TestRegisterHashListenerReachesTheLiveStateDB(t *testing.T) {
	stateDB, _, liveStateDB := newTestStateDB(t)

	var seen []int64
	mostRecent, err := stateDB.RegisterHashListener(
		func(_ context.Context, blockNumber int64, _ *lthash.BlockHash) error {
			seen = append(seen, blockNumber)
			return nil
		})
	require.NoError(t, err)
	require.Equal(t, int64(0), mostRecent.BlockNumber, "a fresh store has hashed nothing")

	require.NoError(t, stateDB.CommitStateChanges(1, changeset("key", "one")))
	require.NoError(t, stateDB.CommitStateChanges(2, changeset("key", "two")))
	require.NoError(t, liveStateDB.FlushHashes())

	require.Equal(t, []int64{1, 2}, seen)
}

// The hash logger is wired in as a listener, and this is that wiring end to end: blocks committed
// through the StateDB have to come back out of an archive on disk.
func TestCommittedBlockHashesReachARealHashLogArchive(t *testing.T) {
	const blocks = 2

	stateDB, _, liveStateDB := newTestStateDB(t)

	// The columns are declared here, at construction, which is the only place they are registered.
	archiveDir := t.TempDir()
	cfg := hashlog.DefaultHashLoggerConfig(archiveDir, "giga-archive-test")
	cfg.HashTypes = flatkv.HashTypes()
	hl, err := hashlog.NewHashLogger(cfg)
	require.NoError(t, err)

	_, err = stateDB.RegisterHashListener(hl.HashListener)
	require.NoError(t, err)

	for height := int64(1); height <= blocks; height++ {
		cs := changeset("key", "value")
		require.NoError(t, stateDB.CommitStateChanges(height, cs))

		// The changeset column is the logger's own and only the caller can supply it. Without it no
		// block is ever complete and none reaches disk. The executor plays this part in production.
		hl.ReportChangeset(uint64(height), cs)
	}

	// Hashing runs off the commit path, and the archive is only sealed by Close.
	require.NoError(t, liveStateDB.FlushHashes())
	require.NoError(t, hl.Close())

	for height := uint64(1); height <= blocks; height++ {
		reports, err := hashlog.ReadHashForBlock(archiveDir, height)
		require.NoError(t, err)
		require.Len(t, reports, 1, "block %d should appear exactly once in the archive", height)

		for _, hashType := range flatkv.HashTypes() {
			require.NotEmpty(t, reports[0].Hashes[hashType],
				"block %d recorded no %s hash", height, hashType)
		}
	}
}

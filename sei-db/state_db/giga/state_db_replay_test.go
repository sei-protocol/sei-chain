package giga

import (
	"os"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
	"github.com/stretchr/testify/require"
)

// A node that keeps no EVM state store never reaches it, so nothing probes a store it does not have.
// The directory is one an earlier run with SS on could have left, and the WAL reaches block 1, so a
// rollback that read it would come back with a rewind to run.
func TestDiscardStateAboveLeavesANodeThatKeepsNoEVMStoreAlone(t *testing.T) {
	const target = int64(7)
	dir := t.TempDir()
	s := &StateDB{
		flatkvCfg: flatkvconfig.DefaultTestConfig(t),
		ssCfg:     config.StateStoreConfig{Enable: false, EVMDBDirectory: dir},
	}

	require.NoError(t, s.discardStateAbove(storedWALRange{first: 1, last: 9}, target))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries,
		"a rollback must not write to the directory of a store this node has not opened")
}

// gapWAL reports a stored range beginning above the block a replay has to start from, which is what a
// WAL pruned past a store's head looks like.
type gapWAL struct {
	statewal.StateWAL
	first, last uint64
}

func (w *gapWAL) GetStoredRange() (bool, uint64, uint64, error) { return true, w.first, w.last, nil }

// A WAL pruned past the live state DB has dropped blocks it still needs. Applying only the blocks the WAL
// happens to hold and then reporting the target as reached is silent divergence, so the replay refuses
// instead.
func TestCatchUpRefusesAWALMissingTheBlocksTheLiveStateDBNeeds(t *testing.T) {
	_, _, sc := newTestStateDB(t)
	s := &StateDB{wal: &gapWAL{first: 3, last: 4}, sc: sc}

	require.ErrorContains(t, s.catchUpTo(t.Context(), 4), "missing (data loss or corruption)")
}

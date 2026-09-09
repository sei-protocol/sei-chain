package giga

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
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

// An interrupted rewind of a separate-DB SS leaves some of its databases holding blocks above the
// target and the rest empty. The head is the lowest of them, so the store reads as below the target and
// stopping there would leave those blocks for a replay that cannot delete them. The rewind has to be
// seen through instead, which for a store with no snapshot means emptying it.
func TestDiscardStateAboveSeesAnInterruptedRewindThrough(t *testing.T) {
	const target = int64(3)
	ssCfg := config.DefaultStateStoreConfig()
	ssCfg.EVMDBDirectory = filepath.Join(t.TempDir(), "ss")
	ssCfg.SeparateEVMSubDBs = true
	writeTornSS(t, ssCfg, 5)
	root := utils.GetStateStoreSnapshotsSiblingPath(ssCfg.EVMDBDirectory)

	landsOn, err := evm.DiscardStateAbove(ssCfg, root, target, 1)
	require.NoError(t, err)

	require.Zero(t, landsOn, "the databases still holding block 5 have to be emptied, not left for the replay")
	_, highest, err := evm.StoredVersions(ssCfg)
	require.NoError(t, err)
	require.Zero(t, highest, "no database may still record a block above the target")
}

// writeTornSS leaves a separate-DB EVM state store with its databases disagreeing, as an interrupted
// rewind between two of them does: all but the first record version, and that one reopens empty.
func writeTornSS(t *testing.T, ssCfg config.StateStoreConfig, version int64) {
	t.Helper()
	ss, err := evm.NewEVMStateStore(ssCfg.EVMDBDirectory, ssCfg)
	require.NoError(t, err)
	require.NoError(t, ss.SetLatestVersion(version))
	require.NoError(t, ss.Close())
	require.NoError(t, os.RemoveAll(
		filepath.Join(ssCfg.EVMDBDirectory, evm.StoreTypeName(evm.AllEVMStoreTypes()[0]))))
}

// gapWAL reports a stored range beginning above the block a replay has to start from, which is what a
// WAL pruned past a store's head looks like.
type gapWAL struct {
	statewal.StateWAL
	first, last uint64
}

func (w *gapWAL) GetStoredRange() (bool, uint64, uint64, error) { return true, w.first, w.last, nil }

// A WAL pruned past a store has dropped blocks that store still needs. Applying only the blocks the WAL
// happens to hold and then reporting the target as reached is silent divergence, so the replay refuses
// instead.
//
// SC and SS both have to reach this check, which is why they replay through one function rather than
// each walking the WAL: the store that goes around it is the one that diverges quietly.
func TestCatchUpRefusesAWALMissingTheBlocksAStoreNeeds(t *testing.T) {
	const missingBlocks = "missing (data loss or corruption)"

	t.Run("the state commit store", func(t *testing.T) {
		_, _, sc := newTestStateDB(t)
		s := &StateDB{wal: &gapWAL{first: 3, last: 4}, sc: sc}

		require.ErrorContains(t, s.catchUpTo(4), missingBlocks)
	})

	t.Run("the EVM state store", func(t *testing.T) {
		_, _, sc := newTestStateDB(t)
		s := &StateDB{wal: &gapWAL{first: 3, last: 4}, sc: sc, ss: &evm.EVMStateStore{}}

		// The store holds nothing, so the gap is its whole history rather than a hole in it. Refusing
		// here would report data loss for a store that is merely new, and would do it on every node
		// past its first retention cut, so it is left out of the replay to fill forward from the target.
		_, replays, err := s.ssReplayStart(4)

		require.NoError(t, err)
		require.False(t, replays)
		require.Zero(t, s.ss.GetLatestVersion())
	})
}

// A store left to fill forward is not held to the target afterwards. Holding it there would fail the
// rollback over exactly the state the catch-up had just decided was the right outcome.
func TestMatchHeightExcusesAStoreLeftToFillForward(t *testing.T) {
	_, _, sc := newTestStateDB(t)
	for block := int64(1); block <= 4; block++ {
		require.NoError(t, sc.CommitStateChanges(block, changeset("k", "v")))
	}
	s := &StateDB{wal: &gapWAL{first: 3, last: 4}, sc: sc, ss: &evm.EVMStateStore{}}

	require.NoError(t, s.matchHeight(4))
}

// An empty SS is only excused when the WAL cannot rebuild it. Excusing every version-0 store would
// treat a wiped history as a brand-new one whenever the WAL still reaches block 1.
func TestMatchHeightDoesNotExcuseAnEmptyStoreTheWALCanRebuild(t *testing.T) {
	_, _, sc := newTestStateDB(t)
	for block := int64(1); block <= 4; block++ {
		require.NoError(t, sc.CommitStateChanges(block, changeset("k", "v")))
	}
	s := &StateDB{wal: &gapWAL{first: 1, last: 4}, sc: sc, ss: &evm.EVMStateStore{}}

	err := s.matchHeight(4)

	require.ErrorContains(t, err, "EVM state store")
	// Both the open and a rollback converge here, so the height belongs to whichever asked. Naming a
	// rollback would send an operator whose node will not start looking for one nobody ran.
	require.NotContains(t, err.Error(), "roll back")
	require.NotContains(t, err.Error(), "4")
}

// A store that holds nothing is only left empty when the WAL cannot rebuild it. One the WAL still
// reaches back far enough for comes out of recovery holding real history, which is strictly better, and
// is how a store that lagged the WAL is populated on restart.
func TestCatchUpRebuildsAnEmptyStoreTheWALStillCovers(t *testing.T) {
	_, _, sc := newTestStateDB(t)
	s := &StateDB{wal: &gapWAL{first: 1, last: 4}, sc: sc, ss: &evm.EVMStateStore{}}

	fillForward, err := s.ssFillsForward()
	require.NoError(t, err)
	require.False(t, fillForward, "a WAL starting at block 1 can rebuild an empty store")
}

package flatkv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/controller"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
)

// rollbackTestCheckpointConfig is the snapshot schedule the live state DBs in these tests commit and roll back on.
var rollbackTestCheckpointConfig = seidbconfig.CheckpointConfig{BlockInterval: 1_000}

var _ gigatypes.StateView = emptyStateView{}

// emptyStateView is a StateView holding no keys. Only the raw reads are supported.
type emptyStateView struct {
	// Left nil, so every read other than the raw reads panics.
	gigatypes.StateView

	// The block height the view reports.
	height int64
}

func (v emptyStateView) GetBlockHeight() int64 { return v.height }

func (v emptyStateView) BatchGetRaw([][]byte) map[string][]byte { return map[string][]byte{} }

func (v emptyStateView) Close() {}

// openLiveStateDBOwningTheWAL opens cfg's live state DB with the state WAL as its own, so each commit writes both,
// as a running node's commits do.
func openLiveStateDBOwningTheWAL(t *testing.T, cfg *config.Config) *CommitStore {
	t.Helper()
	liveStateDB, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, liveStateDB.LoadLatest())
	liveStateDB.SetCheckpointScheduler(controller.NewCheckpointScheduler(rollbackTestCheckpointConfig))
	return liveStateDB
}

// reopenWithoutWAL opens cfg's live state DB with no WAL, as a rollback leaves it, and closes it when the test
// ends.
func reopenWithoutWAL(t *testing.T, cfg *config.Config) *CommitStore {
	t.Helper()
	liveStateDB, err := OpenLiveStateDB(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, liveStateDB.Close()) })
	return liveStateDB
}

// nonceMutations returns block's mutations setting the nonce of the account addr names, converted against prior.
func nonceMutations(t *testing.T, block uint64, addr byte, prior gigatypes.StateView) []mutation.Mutation {
	t.Helper()
	changes := &gigatypes.StateChangeSet{Accounts: []gigatypes.AccountChange{{
		Address: gigatypes.Address{addr},
		Account: gigatypes.AccountState{Nonce: uint64(addr)},
	}}}
	mutations, err := MutationsFromStateChangeSet(changes, block, prior, config.DefaultConfig())
	require.NoError(t, err)
	return mutations
}

// commitNonceBlocks commits blocks from through through to liveStateDB, each setting a new account's nonce.
func commitNonceBlocks(t *testing.T, liveStateDB *CommitStore, from uint64, through uint64) {
	t.Helper()
	for block := from; block <= through; block++ {
		prior := liveStateDB.OpenView()
		mutations := nonceMutations(t, block, byte(block), prior)
		prior.Close()
		require.NoError(t, liveStateDB.CommitStateChanges(liveStateVersion(block), mutations))
	}
}

// snapshotAt commits block height to liveStateDB so it snapshots it, and waits for the snapshot.
func snapshotAt(t *testing.T, liveStateDB *CommitStore, height uint64) {
	t.Helper()
	require.NoError(t, liveStateDB.FlushSnapshots())
	liveStateDB.SetCheckpointScheduler(controller.NewCheckpointScheduler(
		seidbconfig.CheckpointConfig{BlockInterval: 1}))
	commitNonceBlocks(t, liveStateDB, height, height)
	require.NoError(t, liveStateDB.FlushSnapshots())
}

// writeWALOnly writes blocks from through through to cfg's state WAL alone, each setting a new account's nonce.
// The live state DB must be closed.
func writeWALOnly(t *testing.T, cfg *config.Config, from uint64, through uint64) {
	t.Helper()
	wal, err := OpenStateWAL(cfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, wal.Close()) }()
	for block := from; block <= through; block++ {
		prior := emptyStateView{height: liveStateVersion(block) - 1}
		require.NoError(t, wal.Write(block, nonceMutations(t, block, byte(block), prior)))
	}
	require.NoError(t, wal.Flush())
}

// truncateWAL drops every block above last from cfg's state WAL, as losing its unflushed tail does.
func truncateWAL(t *testing.T, cfg *config.Config, last uint64) {
	t.Helper()
	require.NoError(t, statewal.PruneAfter(StateWALConfig(cfg.DataDir), last))
}

// rollBack rolls cfg's live state DB back to target.
func rollBack(t *testing.T, cfg *config.Config, target uint64) error {
	t.Helper()
	return RollBackLiveStateDB(cfg, target)
}

// rollBackAndCatchUp rolls cfg's live state DB back to target and replays the state WAL into it up to target, as
// startup does: the WAL is cut to target, and the live state DB is opened and caught up to it.
func rollBackAndCatchUp(t *testing.T, cfg *config.Config, target uint64) {
	t.Helper()
	require.NoError(t, rollBack(t, cfg, target))
	truncateWAL(t, cfg, target)
	liveStateDB, err := OpenLiveStateDB(t.Context(), cfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, liveStateDB.Close()) }()
	wal, err := OpenStateWAL(cfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, wal.Close()) }()
	liveStateDB.SetCheckpointScheduler(controller.NewCheckpointScheduler(rollbackTestCheckpointConfig))
	require.NoError(t, CatchUpToStateWAL(t.Context(), liveStateDB, wal))
}

// A target above the WAL's head is a target no replay reaches, and it is refused.
func TestRollBackLiveStateDBRefusesATargetAboveTheWALHead(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	liveStateDB := openLiveStateDBOwningTheWAL(t, cfg)
	commitNonceBlocks(t, liveStateDB, 1, 3)
	require.NoError(t, liveStateDB.Close())

	require.ErrorContains(t, rollBack(t, cfg, 5), "the state WAL ends at 3")
}

// A target of 0 means no state, so there is nothing to roll back to.
func TestRollBackLiveStateDBRefusesAZeroTarget(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	writeWALOnly(t, cfg, 1, 1)

	require.ErrorContains(t, rollBack(t, cfg, 0), "nothing to roll back to")
}

// A target above where the live state DB sits is not a rollback for it: it replays forward from the height it
// already holds. Its snapshot is far below and stays untouched, since landing on it would discard everything
// committed since and replay the lot back.
func TestRollBackLiveStateDBReplaysForwardFromItsOwnHeight(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	liveStateDB := openLiveStateDBOwningTheWAL(t, cfg)
	snapshotAt(t, liveStateDB, 1)
	liveStateDB.SetCheckpointScheduler(controller.NewCheckpointScheduler(
		seidbconfig.CheckpointConfig{BlockInterval: 1_000_000}))
	commitNonceBlocks(t, liveStateDB, 2, 7)
	require.NoError(t, liveStateDB.Close())
	// Block 8 goes to the live state DB under an address the WAL's own block 8 does not carry. It marks the
	// working copy: a rebuild from the snapshot replays the WAL's block 8 instead and the address is gone.
	withoutWAL, err := OpenLiveStateDB(t.Context(), cfg)
	require.NoError(t, err)
	prior := withoutWAL.OpenView()
	marked := nonceMutations(t, 8, 108, prior)
	prior.Close()
	require.NoError(t, withoutWAL.CommitStateChanges(8, marked))
	require.NoError(t, withoutWAL.Close())
	writeWALOnly(t, cfg, 8, 10)

	rollBackAndCatchUp(t, cfg, 9)

	rolledBack := reopenWithoutWAL(t, cfg)
	require.Equal(t, int64(9), rolledBack.Version())
	require.DirExists(t, filepath.Join(cfg.DataDir, snapshotName(1)))
	view := rolledBack.OpenView()
	defer view.Close()
	got, ok := view.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, gigatypes.Address{108}.Bytes()))
	require.True(t, ok, "the live state DB replayed forward from 8, so the working copy it held there is kept")
	require.NotEmpty(t, got)
}

// The live state DB comes up on the version its own files hold, which a commit whose WAL write outlived the crash
// that stopped its state write leaves one block back. A rollback to the WAL's head replays it there.
func TestRollBackLiveStateDBReplaysUpToTheWALHead(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	liveStateDB := openLiveStateDBOwningTheWAL(t, cfg)
	commitNonceBlocks(t, liveStateDB, 1, 2)
	require.NoError(t, liveStateDB.Close())
	writeWALOnly(t, cfg, 3, 3)

	rollBackAndCatchUp(t, cfg, 3)

	require.Equal(t, int64(3), reopenWithoutWAL(t, cfg).Version())
}

// A working copy holding blocks the WAL no longer has is rebuilt from the snapshot and replayed back up. Those
// blocks were never servable, so the state above the WAL's head goes.
func TestRollBackLiveStateDBRebuildsAWorkingCopyAboveTheWALHead(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	liveStateDB := openLiveStateDBOwningTheWAL(t, cfg)
	commitNonceBlocks(t, liveStateDB, 1, 3)
	require.NoError(t, liveStateDB.Close())
	truncateWAL(t, cfg, 2)

	rollBackAndCatchUp(t, cfg, 2)

	require.Equal(t, int64(2), reopenWithoutWAL(t, cfg).Version())
}

// The WAL is written unflushed, so a crash can lose its tail while a snapshot published above that tail survives.
// Rebuilding the working copy does not reach that: the current link names the version above, so the store opens
// there however often it is rebuilt.
func TestRollBackLiveStateDBRewindsFromASnapshotAboveTheTarget(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	liveStateDB := openLiveStateDBOwningTheWAL(t, cfg)
	commitNonceBlocks(t, liveStateDB, 1, 4)
	snapshotAt(t, liveStateDB, 5)
	require.Equal(t, int64(5), liveStateDB.Version())
	require.NoError(t, liveStateDB.Close())
	truncateWAL(t, cfg, 3)

	rollBackAndCatchUp(t, cfg, 3)

	require.Equal(t, int64(3), reopenWithoutWAL(t, cfg).Version())
}

// A live state DB above the target with no snapshot at or below it cannot be rewound, and the rollback is refused.
func TestRollBackLiveStateDBWithoutASnapshotBelowTheTargetIsRefused(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	liveStateDB := openLiveStateDBOwningTheWAL(t, cfg)
	commitNonceBlocks(t, liveStateDB, 1, 3)
	require.NoError(t, liveStateDB.Close())
	require.NoError(t, os.RemoveAll(filepath.Join(cfg.DataDir, snapshotName(0))))
	truncateWAL(t, cfg, 2)

	err := rollBack(t, cfg, 2)

	require.ErrorContains(t, err, "no snapshot")
	require.ErrorContains(t, err, "cannot roll back to 2")
}

// gapWAL reports a stored range beginning above the block a replay has to start from, which is what a WAL pruned
// past the live state DB's head looks like.
type gapWAL struct {
	statewal.StateWAL
	first uint64
	last  uint64
}

func (w *gapWAL) GetStoredRange() (bool, uint64, uint64, error) { return true, w.first, w.last, nil }

// A WAL pruned past the live state DB has dropped blocks it still needs. Applying only the blocks the WAL happens
// to hold and then reporting the target as reached is silent divergence, so the replay refuses instead.
func TestCatchUpRefusesAWALMissingTheBlocksTheLiveStateDBNeeds(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	liveStateDB := reopenWithoutWAL(t, cfg)

	require.ErrorContains(t, CatchUpToStateWAL(t.Context(), liveStateDB, &gapWAL{first: 3, last: 4}),
		"missing (data loss or corruption)")
}

// commitFlushedNonceBlocks commits blocks from through through to liveStateDB and waits until every DB has
// written the last of them to disk, so closing it leaves all four DBs at through.
func commitFlushedNonceBlocks(t *testing.T, liveStateDB *CommitStore, from uint64, through uint64) {
	t.Helper()
	commitNonceBlocks(t, liveStateDB, from, through)
	require.NoError(t, liveStateDB.flushLatestVersion())
}

// leaveCodeDBABlockBehind commits blocks 1-3 to a live state DB under cfg and closes it with its code DB put back
// to block 2 and its account DB at block 3, as a crash between the two DBs' flushes leaves them. It returns the
// state hash of block 3.
func leaveCodeDBABlockBehind(t *testing.T, cfg *config.Config) []byte {
	t.Helper()
	liveStateDB := openLiveStateDBOwningTheWAL(t, cfg)
	commitFlushedNonceBlocks(t, liveStateDB, 1, 2)
	require.NoError(t, liveStateDB.Close())
	codeAtBlock2 := copyDataDB(t, cfg, codeDBDir)

	liveStateDB = openLiveStateDBOwningTheWAL(t, cfg)
	// Block 3 changes an account, so only the account DB's rows and hash move.
	commitFlushedNonceBlocks(t, liveStateDB, 3, 3)
	hash := rootHash(liveStateDB)
	require.NoError(t, liveStateDB.Close())
	restoreDataDB(t, cfg, codeDBDir, codeAtBlock2)
	accountVersion, err := readCommittedVersion(filepath.Join(cfg.DataDir, workingDirName, accountDBDir))
	require.NoError(t, err)
	require.Equal(t, int64(3), accountVersion)
	codeVersion, err := readCommittedVersion(filepath.Join(cfg.DataDir, workingDirName, codeDBDir))
	require.NoError(t, err)
	require.Equal(t, int64(2), codeVersion)
	return hash
}

// Catching up replays a block into the DBs missing it and leaves the one already holding it alone, so the state
// hash comes out as it was before the crash.
func TestCatchUpSkipsADataDBAlreadyHoldingTheBlock(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	wantHash := leaveCodeDBABlockBehind(t, cfg)

	caughtUp := reopenWithoutWAL(t, cfg)
	wal, err := OpenStateWAL(cfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, wal.Close()) }()
	caughtUp.SetCheckpointScheduler(controller.NewCheckpointScheduler(rollbackTestCheckpointConfig))
	require.NoError(t, CatchUpToStateWAL(t.Context(), caughtUp, wal))

	gotHash, gotVersion := rootHashAndVersion(caughtUp)
	require.Equal(t, int64(3), gotVersion)
	require.Equal(t, wantHash, gotHash)
}

// Outside replay, a block committed to a DB that already holds it is an error.
func TestCommittingABlockADataDBAlreadyHoldsIsRejected(t *testing.T) {
	cfg := config.DefaultTestConfig(t)
	leaveCodeDBABlockBehind(t, cfg)

	liveStateDB := reopenWithoutWAL(t, cfg)
	require.Equal(t, int64(2), liveStateDB.Version())
	prior := liveStateDB.OpenView()
	mutations := nonceMutations(t, 3, 3, prior)
	prior.Close()
	require.ErrorContains(t, liveStateDB.CommitStateChanges(3, mutations),
		"cannot commit block 3: the account DB's next block is 4")
}

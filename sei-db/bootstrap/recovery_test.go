package bootstrap

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/controller"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
)

func evmNonceKey(addr byte) []byte {
	return append([]byte{0x0a}, append([]byte{addr}, make([]byte, 19)...)...)
}

func evmNonce(value byte) []byte {
	return append(make([]byte, 7), value)
}

func evmBlock(addr, nonce byte) []*proto.NamedChangeSet {
	return []*proto.NamedChangeSet{{
		Name: evm.EVMStoreKey,
		Changeset: proto.ChangeSet{
			Pairs: []*proto.KVPair{{Key: evmNonceKey(addr), Value: evmNonce(nonce)}},
		},
	}}
}

func commitBlocks(t *testing.T, manager *GigaStorageManager, through byte) {
	t.Helper()
	for block := byte(1); block <= through; block++ {
		require.NoError(t, manager.StateDB().CommitStateChanges(int64(block), evmBlock(block, block)))
	}
}

func writeWALOnly(t *testing.T, wal statewal.StateWAL, block uint64, changesets []*proto.NamedChangeSet) {
	t.Helper()
	require.NoError(t, wal.Write(block, changesets))
	require.NoError(t, wal.SignalEndOfBlock())
	require.NoError(t, wal.Flush())
}

func applySSThrough(t *testing.T, manager *GigaStorageManager, through byte) {
	t.Helper()
	for block := byte(1); block <= through; block++ {
		require.NoError(t, manager.SS().ApplyChangesetSync(int64(block), evmBlock(block, block)))
	}
}

// snapshotSSAt commits block height through SS's commit path so the checkpoint schedule snapshots it.
// CommitBlock is what offers a version to the schedule — the apply methods are raw writes that take no
// snapshot — and a BlockInterval of 1 makes every offered version a boundary. Publication happens off
// the commit path, so the snapshot has to be waited for.
func snapshotSSAt(t *testing.T, manager *GigaStorageManager, height byte) {
	t.Helper()
	manager.SS().SetCheckpointScheduler(controller.NewCheckpointScheduler(config.CheckpointConfig{BlockInterval: 1}))
	require.NoError(t, manager.SS().CommitBlock(int64(height), evmBlock(height, height)))
	require.Eventually(t, func() bool { return manager.SS().Snapshots().Newest() >= int64(height) },
		10*time.Second, 10*time.Millisecond, "the snapshot a rollback restores from must be published")
}

// reconverge re-runs what a restart does to the two halves of state: it closes them, opens a StateDB
// again — which converges them onto the WAL's head — and rolls that down to target.
func reconverge(t *testing.T, manager *GigaStorageManager, target int64) {
	t.Helper()
	// Closing first is what a restart does, and it is also required: the stores the new StateDB opens
	// take file locks the old ones still hold.
	closeStateDB(t, manager)
	require.NoError(t, manager.openStateDB(t.Context()))
	require.NoError(t, manager.recoverState(target))
}

// closeStateDB closes the two halves of state and their WAL and drops them from the manager, leaving it
// as it was before the StateDB opened. Manager.Close tolerates that, so a test may still defer it.
func closeStateDB(t *testing.T, manager *GigaStorageManager) {
	t.Helper()
	require.NoError(t, manager.StateDB().Close())
	manager.stateDB = nil
}

// snapshotSCAt commits block height so SC snapshots it, leaving a snapshot a later rollback can rewind
// to. A BlockInterval of 1 makes every offered version a boundary, and the snapshot is written off the
// commit path, so it has to be waited for.
func snapshotSCAt(t *testing.T, manager *GigaStorageManager, height byte) {
	t.Helper()
	manager.SC().SetCheckpointScheduler(controller.NewCheckpointScheduler(config.CheckpointConfig{BlockInterval: 1}))
	require.NoError(t, manager.StateDB().CommitStateChanges(int64(height), evmBlock(height, height)))
	require.NoError(t, manager.SC().FlushSnapshots())
}

func requireWALTail(t *testing.T, manager *GigaStorageManager, want uint64) {
	t.Helper()
	stored, _, last, err := manager.StateWAL().GetStoredRange()
	require.NoError(t, err)
	if want == 0 {
		require.False(t, stored)
		return
	}
	require.True(t, stored)
	require.Equal(t, want, last)
}

func TestFindTargetRecoveryHeightIsZeroWithoutABlockLedger(t *testing.T) {
	manager, _ := openManager(t, nil)
	commitBlocks(t, manager, 3)
	require.NoError(t, manager.ReceiptDB().SetLatestVersion(3))
	// findTargetRecoveryHeight reads the WAL directory offline, so the handle has to be closed.
	closeStateDB(t, manager)

	got, err := manager.findTargetRecoveryHeight()
	require.NoError(t, err)
	require.Equal(t, int64(0), got)
}

// Recovering to a target below the WAL head drops every block above it, so the write head resumes at
// the target. Committing the block after it is what proves the truncation: an untruncated WAL still
// holds that block and refuses to write it a second time.
func TestRecoverStateDropsWALBlocksAboveTheTarget(t *testing.T) {
	manager, _ := openManager(t, nil)
	commitBlocks(t, manager, 5)

	reconverge(t, manager, 3)

	requireWALTail(t, manager, 3)
	require.NoError(t, manager.StateDB().CommitStateChanges(4, evmBlock(4, 4)))
}

func TestRecoverSCReplaysAMissedWALBlock(t *testing.T) {
	manager, _ := openManager(t, nil)
	commitBlocks(t, manager, 2)
	writeWALOnly(t, manager.StateWAL(), 3, evmBlock(3, 3))
	require.Equal(t, int64(2), manager.SC().Version())

	reconverge(t, manager, 3)

	require.Equal(t, int64(3), manager.SC().Version())
}

func TestRecoverSCRollsBackToTheTarget(t *testing.T) {
	manager, _ := openManager(t, nil)
	commitBlocks(t, manager, 3)

	reconverge(t, manager, 2)

	require.Equal(t, int64(2), manager.SC().Version())
}

// A commit store held above the WAL head by a snapshot of its own is rewound to a snapshot boundary at
// or below the target and replayed back up to it, rather than left where it is. Only a snapshot can put
// SC above the head, since a truncated WAL is otherwise what its load lands on; taking one above the
// height the WAL is truncated to is what reaches this.
//
// Committing afterwards is the check that the rewind left both the store and the WAL writable at the
// height it converged on, not merely reporting that height.
func TestRecoverSCAboveTheWALHeadRewindsToASnapshotAndReplays(t *testing.T) {
	manager, _ := openManager(t, nil)
	commitBlocks(t, manager, 2)
	snapshotSCAt(t, manager, 3)

	reconverge(t, manager, 2)

	require.Equal(t, int64(2), manager.SC().Version())
	requireWALTail(t, manager, 2)
	require.NoError(t, manager.StateDB().CommitStateChanges(3, evmBlock(3, 3)))
}

func TestRecoverSSReplaysEVMChangesets(t *testing.T) {
	manager, _ := openManager(t, nil)
	commitBlocks(t, manager, 2)
	require.Zero(t, manager.SS().GetLatestVersion())

	reconverge(t, manager, 2)

	require.Equal(t, int64(2), manager.SS().GetLatestVersion())
	value, err := manager.SS().Get(evm.EVMStoreKey, 2, evmNonceKey(2))
	require.NoError(t, err)
	require.Equal(t, evmNonce(2), value)
}

func TestRecoverSSRollsBackToTheTarget(t *testing.T) {
	manager, _ := openManager(t, nil)
	commitBlocks(t, manager, 3)
	snapshotSSAt(t, manager, 1)
	applySSThrough(t, manager, 3)

	reconverge(t, manager, 2)

	require.Equal(t, int64(2), manager.SS().GetLatestVersion())
	above, err := manager.SS().Get(evm.EVMStoreKey, 3, evmNonceKey(3))
	require.NoError(t, err)
	require.Nil(t, above)
}

// RollbackTo on a live StateDB rewinds both halves of state and the WAL that feeds them, so the write
// head lands on the target. Committing the block after the target is what proves the WAL was truncated
// rather than only the stores rewound: a WAL still holding that block refuses to write it a second time.
//
// Nothing here reads the manager's WAL reference, because the truncation replaced the handle it holds.
func TestStateDBRollbackToRewindsBothHalvesAndTheWAL(t *testing.T) {
	manager, _ := openManager(t, nil)
	commitBlocks(t, manager, 5)

	require.NoError(t, manager.StateDB().RollbackTo(2))

	require.Equal(t, int64(2), manager.SC().Version())
	require.Equal(t, int64(2), manager.SS().GetLatestVersion())

	require.NoError(t, manager.StateDB().CommitStateChanges(3, evmBlock(3, 3)))
	require.Equal(t, int64(3), manager.SC().Version())
}

func TestRecoverReceiptRewindsTheHead(t *testing.T) {
	manager, _ := openManager(t, nil)
	require.NoError(t, manager.ReceiptDB().SetLatestVersion(5))

	require.NoError(t, manager.recoverReceipt(3))

	require.Equal(t, int64(3), manager.ReceiptDB().LatestVersion())
}

func TestOpenDBWithoutRecoveryOnAFreshHome(t *testing.T) {
	manager, _ := openManager(t, nil)

	require.Zero(t, manager.SC().Version())
	require.Zero(t, manager.SS().GetLatestVersion())
	require.Zero(t, manager.ReceiptDB().LatestVersion())
}

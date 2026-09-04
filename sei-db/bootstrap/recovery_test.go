package bootstrap

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/controller"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
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

// writeReceipts writes one receipt per block into the receipt store, for blocks 1 through through.
//
// A rollback refuses a store whose index promises receipts its bodies do not have, so a test that rolls
// receipts back has to write bodies rather than only stamp a head with SetLatestVersion.
func writeReceipts(t *testing.T, manager *GigaStorageManager, through uint64) {
	t.Helper()
	storeKey := storetypes.NewKVStoreKey("evm")
	ctx := testutil.DefaultContext(storeKey, storetypes.NewTransientStoreKey("evm_transient"))
	for block := uint64(1); block <= through; block++ {
		txHash := common.BigToHash(new(big.Int).SetUint64(block))
		records := []receipt.ReceiptRecord{{
			TxHash: txHash,
			Receipt: &evmtypes.Receipt{
				TxHashHex:   txHash.Hex(),
				BlockNumber: block,
				GasUsed:     21000,
			},
		}}
		//nolint:gosec // small test heights
		require.NoError(t, manager.ReceiptDB().SetReceipts(ctx.WithBlockHeight(int64(block)), records))
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

// reconverge re-runs what a restart does: it closes every store recovery touches, opens a StateDB
// again, recovers every store onto target, and reopens the receipt store on the far side.
func reconverge(t *testing.T, manager *GigaStorageManager, target int64) {
	t.Helper()
	// Closing first is what a restart does, and it is also required: recovery takes file locks the
	// open stores hold — the StateDB's for the state it opens, the receipt store's for the rollback
	// that runs against its files.
	closeStateDB(t, manager)
	closeReceiptDB(t, manager)
	require.NoError(t, manager.openStateDB(t.Context()))
	require.NoError(t, manager.recoverStores(target))
	require.NoError(t, manager.openReceiptStore())
}

// closeStateDB closes the two halves of state and their WAL and drops them from the manager, leaving it
// as it was before the StateDB opened. Manager.Close tolerates that, so a test may still defer it.
func closeStateDB(t *testing.T, manager *GigaStorageManager) {
	t.Helper()
	require.NoError(t, manager.StateDB().Close())
	manager.stateDB = nil
}

// closeReceiptDB closes the receipt store and drops it from the manager, leaving it as it was before
// openReceiptStore ran. Manager.Close tolerates that, so a test may still defer it.
func closeReceiptDB(t *testing.T, manager *GigaStorageManager) {
	t.Helper()
	if manager.ReceiptDB() == nil {
		return
	}
	require.NoError(t, manager.ReceiptDB().Close())
	manager.receiptDB = nil
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

func TestRecoveryTarget(t *testing.T) {
	for _, tc := range []struct {
		name                                  string
		blockHeight, stateHeight, receiptHead uint64
		want                                  uint64
	}{
		{name: "a fresh node has no height to converge on"},
		{name: "the lowest head wins", blockHeight: 7, stateHeight: 5, receiptHead: 6, want: 5},
		{name: "receipts can be the lowest", blockHeight: 7, stateHeight: 6, receiptHead: 4, want: 4},
		// The regression: receipts newly enabled, or a receipt directory recreated after corruption,
		// leave a head of 0 alongside real block and state history. Folding that 0 into the minimum
		// collapses the target and skips recovery for the stores that do have history.
		{name: "an empty receipt store does not collapse the target", blockHeight: 7, stateHeight: 5, want: 5},
		{name: "a disabled receipt store reads the same as an empty one", blockHeight: 4, stateHeight: 4, want: 4},
		{name: "an empty state WAL yields no target", blockHeight: 7, receiptHead: 7},
		{name: "an empty block store yields no target", stateHeight: 7, receiptHead: 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, recoveryTarget(tc.blockHeight, tc.stateHeight, tc.receiptHead))
		})
	}
}

// A target of 0 is no height to converge on, and rolling back to it would drop every receipt the node
// holds. The guard has to cover receipts and not just state: recoverReceipt rewinds the head, range-
// deletes the whole tag index and drops every body, none of which a replay can put back.
func TestRecoverStoresAtAZeroTargetLeavesReceiptsAlone(t *testing.T) {
	manager, _ := openManager(t, nil)
	commitBlocks(t, manager, 3)
	writeReceipts(t, manager, 5)
	closeReceiptDB(t, manager)

	require.NoError(t, manager.recoverStores(0))

	require.NoError(t, manager.openReceiptStore())
	require.Equal(t, int64(5), manager.ReceiptDB().LatestVersion(),
		"a zero target must leave the receipt store where it was found")
}

func TestFindTargetRecoveryHeightIsZeroWithoutABlockLedger(t *testing.T) {
	manager, _ := openManager(t, nil)
	commitBlocks(t, manager, 3)
	require.NoError(t, manager.ReceiptDB().SetLatestVersion(3))
	// findTargetRecoveryHeight reads the state and receipt directories offline, so both stores have
	// to be closed for it.
	closeStateDB(t, manager)
	closeReceiptDB(t, manager)

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

// A rollback discards the history a snapshot above the target was taken from. Leaving that snapshot
// behind lets a later rollback to its height restore it as authoritative with no replay over it, and
// leaves the retention arithmetic reading a newest version the node has rejected.
func TestRecoverSSRemovesSnapshotsAboveTheTarget(t *testing.T) {
	manager, _ := openManager(t, nil)
	commitBlocks(t, manager, 3)
	snapshotSSAt(t, manager, 1)
	snapshotSSAt(t, manager, 3)
	require.Equal(t, int64(3), manager.SS().Snapshots().Newest())

	reconverge(t, manager, 2)

	require.Equal(t, int64(1), manager.SS().Snapshots().Newest(),
		"a snapshot above the target must not survive the rollback")
	require.Equal(t, int64(2), manager.SS().GetLatestVersion())
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

// A target above the WAL's head is a target no replay reaches: the WAL runs out and both halves stop
// below it with every step reporting success. Answering yes there hands callers a state DB they believe
// is on a height it is not, so the landing height is checked rather than assumed.
func TestStateDBRollbackToAboveTheWALHeadFails(t *testing.T) {
	manager, _ := openManager(t, nil)
	commitBlocks(t, manager, 3)

	require.ErrorContains(t, manager.StateDB().RollbackTo(5), "left the state commit store on 3")
}

// A target of 0 has to be refused by RollbackTo itself, because neither rewind it delegates to is
// reached: each skips a store already at or below the target, and every store is at or below 0.
//
// The fixture is the state NewStateDB leaves — SC opened on snapshot 0, replaying nothing, with the WAL
// holding blocks — so every step between the two rewinds runs. Ungated, the WAL prune empties the WAL,
// the snapshot removal takes every snapshot, and the landing check passes because both halves really
// are on 0. The only thing standing between that and a wipe is recoverStores' own target check, and
// RollbackTo is exported on the StateDB contract.
func TestStateDBRollbackToZeroIsRefused(t *testing.T) {
	manager, _ := openManager(t, nil)
	writeWALOnly(t, manager.StateWAL(), 1, evmBlock(1, 1))
	require.Zero(t, manager.SC().Version(), "fixture precondition: SC must read as 0 under a populated WAL")

	require.ErrorContains(t, manager.StateDB().RollbackTo(0), "nothing to roll back to")

	requireWALTail(t, manager, 1)
}

// recoverReceipt rolls the store back through its files, so it runs while the store is closed and a
// store opened on the far side is what reports the result.
func TestRecoverReceiptRewindsTheHead(t *testing.T) {
	manager, _ := openManager(t, nil)
	writeReceipts(t, manager, 5)
	closeReceiptDB(t, manager)

	require.NoError(t, manager.recoverReceipt(3))

	require.NoError(t, manager.openReceiptStore())
	require.Equal(t, int64(3), manager.ReceiptDB().LatestVersion())
}

func TestOpenDBWithoutRecoveryOnAFreshHome(t *testing.T) {
	manager, _ := openManager(t, nil)

	require.Zero(t, manager.SC().Version())
	require.Zero(t, manager.SS().GetLatestVersion())
	require.Zero(t, manager.ReceiptDB().LatestVersion())
}

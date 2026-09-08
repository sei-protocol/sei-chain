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
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
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

// reconverge re-runs what a restart does: it closes every store recovery touches, recovers them onto
// target — which is what opens the state DB again — and reopens the receipt store on the far side.
func reconverge(t *testing.T, manager *GigaStorageManager, target int64) {
	t.Helper()
	require.NoError(t, reconvergeErr(t, manager, target))
	require.NoError(t, manager.openReceiptStore())
}

// reconvergeErr is reconverge up to the point recovery can fail, for a test that expects it to. The
// receipt store is left closed, since a failed recovery leaves the manager with no state DB.
func reconvergeErr(t *testing.T, manager *GigaStorageManager, target int64) error {
	t.Helper()
	// Closing first is what a restart does, and it is also required: recovery takes file locks the
	// open stores hold — the state WAL's directory lock for the reads and the tail cut that precede
	// opening it, the receipt store's for the rollback that runs against its files.
	closeStateDB(t, manager)
	closeReceiptDB(t, manager)
	return manager.recoverStores(t.Context(), target)
}

// closeStateDB closes the stores the StateDB owns and drops it from the manager, leaving the manager as
// it was before the StateDB opened. Manager.Close tolerates that, so a test may still defer it.
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

	reconverge(t, manager, 0)

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

// A plain open replays SC up to the WAL's head. SC comes up on the version its own files hold, which a
// commit whose WAL write outlived the crash that stopped its state write leaves one block back, and
// committing from behind the WAL is rejected outright: that block is already written.
func TestOpenReplaysSCUpToTheWALHead(t *testing.T) {
	manager, _ := openManager(t, nil)
	commitBlocks(t, manager, 2)
	writeWALOnly(t, manager.StateWAL(), 3, evmBlock(3, 3))
	closeStateDB(t, manager)

	require.NoError(t, manager.openStateDB(t.Context()))

	require.Equal(t, int64(3), manager.SC().Version())
	require.NoError(t, manager.StateDB().CommitStateChanges(4, evmBlock(4, 4)))
}

// A plain open discards a working copy holding blocks the WAL no longer has and rebuilds it from the
// snapshot, then replays back up. Those blocks were never servable, so the WAL's head is the height the
// store comes up on, and the state above it goes.
func TestOpenRebuildsSCAboveTheWALHead(t *testing.T) {
	manager, cfg := openManager(t, nil)
	commitBlocks(t, manager, 3)
	closeStateDB(t, manager)
	require.NoError(t, statewal.PruneAfter(flatkv.StateWALConfig(cfg.FlatKVConfig.DataDir), 2))

	require.NoError(t, manager.openStateDB(t.Context()))

	require.Equal(t, int64(2), manager.SC().Version())
	require.NoError(t, manager.StateDB().CommitStateChanges(3, evmBlock(3, 3)))
}

// The WAL is written unflushed, so a crash can lose its tail while a snapshot published above that tail
// survives. Rebuilding the working copy does not reach that: the current link names the version above,
// so the store opens there however often it is rebuilt, and the blocks it holds are ones the WAL can
// no longer replay to.
func TestOpenRewindsSCFromASnapshotAboveTheWALHead(t *testing.T) {
	manager, cfg := openManager(t, nil)
	commitBlocks(t, manager, 4)
	snapshotSCAt(t, manager, 5)
	require.Equal(t, int64(5), manager.SC().Version())
	closeStateDB(t, manager)
	require.NoError(t, statewal.PruneAfter(flatkv.StateWALConfig(cfg.FlatKVConfig.DataDir), 3))

	require.NoError(t, manager.openStateDB(t.Context()))

	require.Equal(t, int64(3), manager.SC().Version())
	require.NoError(t, manager.StateDB().CommitStateChanges(4, evmBlock(4, 4)))
}

// SS reaches the same place through its databases rather than a snapshot, since it keeps no working
// copy to rebuild.
func TestOpenRewindsSSAboveTheWALHead(t *testing.T) {
	manager, cfg := openManager(t, nil)
	commitBlocks(t, manager, 3)
	snapshotSSAt(t, manager, 1)
	applySSThrough(t, manager, 3)
	require.Equal(t, int64(3), manager.SS().GetLatestVersion())
	closeStateDB(t, manager)
	require.NoError(t, statewal.PruneAfter(flatkv.StateWALConfig(cfg.FlatKVConfig.DataDir), 2))

	require.NoError(t, manager.openStateDB(t.Context()))

	require.Equal(t, int64(2), manager.SS().GetLatestVersion())
	require.NoError(t, manager.StateDB().CommitStateChanges(3, evmBlock(3, 3)))
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

// Opening at a target rewinds SC, SS and the WAL that feeds them, so the write head lands on the
// target. Committing the block after the target is what proves the WAL was truncated rather than only
// the stores rewound: a WAL still holding that block refuses to write it a second time.
func TestOpenAtATargetRewindsEveryStoreAndTheWAL(t *testing.T) {
	manager, _ := openManager(t, nil)
	commitBlocks(t, manager, 5)

	reconverge(t, manager, 2)

	require.Equal(t, int64(2), manager.SC().Version())
	require.Equal(t, int64(2), manager.SS().GetLatestVersion())
	requireWALTail(t, manager, 2)

	require.NoError(t, manager.StateDB().CommitStateChanges(3, evmBlock(3, 3)))
	require.Equal(t, int64(3), manager.SC().Version())
}

// A target above the WAL's head is a target no replay reaches, and it is refused before the rollback
// moves anything. Every step of a rollback is irreversible while the replay that needs the blocks runs
// last, so a shortfall found there would have already cut the WAL and dropped the snapshots that a
// second attempt at a reachable height would need.
func TestOpenAtATargetAboveTheWALHeadFails(t *testing.T) {
	manager, _ := openManager(t, nil)
	commitBlocks(t, manager, 3)

	require.ErrorContains(t, reconvergeErr(t, manager, 5), "the state WAL ends at 3")

	// A failed open leaves nothing behind to read the result through, so the check is what a plain
	// open finds: the stores as they were.
	require.NoError(t, manager.openStateDB(t.Context()))
	require.Equal(t, int64(3), manager.SC().Version(), "a refused rollback must not have moved anything")
	requireWALTail(t, manager, 3)
}

// A store with no snapshot at or below the target is rewound to empty and rebuilt from block 1, rather
// than refused. SC and SS are both derived from the WAL, so a WAL that still reaches back that far can
// supply the whole of it, and the rollback lands SS on the target holding real state.
func TestOpenAtATargetRebuildsSSFromTheWAL(t *testing.T) {
	manager, _ := openManager(t, nil)
	commitBlocks(t, manager, 3)
	applySSThrough(t, manager, 3)

	reconverge(t, manager, 2)

	require.Equal(t, int64(2), manager.SS().GetLatestVersion())
	require.Equal(t, int64(2), manager.SC().Version())
}

// A target of 0 has to be refused by the constructor that takes one, and refused there rather than by a
// rewind deep inside it.
//
// This fixture's WAL holds a block, so ungated the rollback would reach rewindSC and fail on that
// function's own refusal to rewind to 0. On an empty WAL nothing would refuse it at all: rewindTo reads
// a head of 0 as nothing to rewind, and the caller would get a plain open. Recovery routes a target of 0
// to the plain open and never reaches this, so the guard is what covers a caller naming 0 outright.
func TestOpenAtAZeroTargetIsRefused(t *testing.T) {
	manager, _ := openManager(t, nil)
	writeWALOnly(t, manager.StateWAL(), 1, evmBlock(1, 1))
	require.Zero(t, manager.SC().Version(), "fixture precondition: SC must read as 0 under a populated WAL")
	closeStateDB(t, manager)

	require.ErrorContains(t, manager.openStateDBAt(t.Context(), 0), "nothing to roll back to")

	require.NoError(t, manager.openStateDB(t.Context()))
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

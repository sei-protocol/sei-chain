package bootstrap

import (
	"fmt"
	"math/big"
	"os"
	"path/filepath"
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

// waitSSWrites blocks until SS has applied every block committed so far. A commit hands SS its block
// asynchronously and does not wait, so a test reading SS straight after one waits here instead.
func waitSSWrites(manager *GigaStorageManager) {
	manager.SS().WaitForPendingWrites()
}

// snapshotSSEveryBlock puts SS on its own every-block schedule so a later rollback has a snapshot
// to land on. The node-wide schedule is shared with SC, and a snapshot still publishing there
// turns later heights down.
func snapshotSSEveryBlock(manager *GigaStorageManager) {
	manager.SS().SetCheckpointScheduler(controller.NewCheckpointScheduler(config.CheckpointConfig{BlockInterval: 1}))
}

// waitSSSnapshot waits until SS has published a snapshot at or above height.
func waitSSSnapshot(t *testing.T, manager *GigaStorageManager, height int64) {
	t.Helper()
	require.Eventually(t, func() bool { return manager.SS().Snapshots().Newest() >= height },
		10*time.Second, 10*time.Millisecond, "the snapshot a rollback restores from must be published")
	// Newest moves before the schedule is told the height is done; the next commit must not offer
	// until that report lands, or the height is turned down.
	time.Sleep(20 * time.Millisecond)
}

// commitBlocksWithSSSnapshots commits blocks 1 through through and waits for an SS snapshot at each,
// so a later rollback has a boundary to land on.
func commitBlocksWithSSSnapshots(t *testing.T, manager *GigaStorageManager, through byte) {
	t.Helper()
	snapshotSSEveryBlock(manager)
	for block := byte(1); block <= through; block++ {
		require.NoError(t, manager.StateDB().CommitStateChanges(int64(block), evmBlock(block, block)))
		waitSSSnapshot(t, manager, int64(block))
	}
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
	if manager.StateDB() == nil {
		return
	}
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

// disableSS turns the EVM state store off.
func disableSS(cfg *config.GigaStorageConfig) {
	cfg.SSConfig.Enable = false
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

// replaceWALWithBlocks empties the state WAL and writes blocks from through through, so the WAL's first
// block can sit above 1. The StateDB must already be closed.
func replaceWALWithBlocks(t *testing.T, cfg *config.GigaStorageConfig, from, through byte) {
	t.Helper()
	require.NoError(t, statewal.PruneAfter(flatkv.StateWALConfig(cfg.FlatKVConfig.DataDir), 0))
	wal, err := flatkv.OpenStateWAL(cfg.FlatKVConfig)
	require.NoError(t, err)
	defer func() { require.NoError(t, wal.Close()) }()
	for block := from; block <= through; block++ {
		writeWALOnly(t, wal, uint64(block), evmBlock(block, block))
	}
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
	manager, _ := openManager(t, disableSS)
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
	manager, cfg := openManager(t, disableSS)
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
	manager, cfg := openManager(t, disableSS)
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
	commitBlocksWithSSSnapshots(t, manager, 3)
	require.Equal(t, int64(3), manager.SS().GetLatestVersion())
	closeStateDB(t, manager)
	require.NoError(t, statewal.PruneAfter(flatkv.StateWALConfig(cfg.FlatKVConfig.DataDir), 2))

	require.NoError(t, manager.openStateDB(t.Context()))

	require.Equal(t, int64(2), manager.SS().GetLatestVersion())
	require.NoError(t, manager.StateDB().CommitStateChanges(3, evmBlock(3, 3)))
}

// An SS above the WAL head with no snapshot at or below it is emptied and replayed from block 1 rather
// than refused. SS takes its snapshots from the shared checkpoint schedule, whose default is a ten
// minute interval, so a node has none at all until its first checkpoint lands; refusing inside that
// window is a node that will not start on any restart until an operator deletes the EVM directory by
// hand. The WAL still holds every block, so the replay reconstructs the store exactly.
func TestOpenSSAboveTheWALHeadRebuildsItFromBlockOne(t *testing.T) {
	manager, cfg := openManager(t, nil)
	commitBlocks(t, manager, 3)
	waitSSWrites(manager)
	require.Equal(t, int64(3), manager.SS().GetLatestVersion())
	require.Zero(t, manager.SS().Snapshots().Newest(), "fixture precondition: SS has no snapshot to land on")
	closeStateDB(t, manager)
	require.NoError(t, statewal.PruneAfter(flatkv.StateWALConfig(cfg.FlatKVConfig.DataDir), 2))

	require.NoError(t, manager.openStateDB(t.Context()))

	require.Equal(t, int64(2), manager.SS().GetLatestVersion())
	rebuilt, err := manager.SS().Get(evm.EVMStoreKey, 2, evmNonceKey(2))
	require.NoError(t, err)
	require.Equal(t, evmNonce(2), rebuilt, "the replay must have put the blocks below the head back")
	above, err := manager.SS().Get(evm.EVMStoreKey, 2, evmNonceKey(3))
	require.NoError(t, err)
	require.Nil(t, above, "the block above the head must not have survived")
	require.NoError(t, manager.StateDB().CommitStateChanges(3, evmBlock(3, 3)))
}

// An SC above the WAL head with no snapshot at or below it cannot be rewound. Rebuilding the working
// copy from an empty snapshot would delete the history it holds; refusing leaves that history in place.
func TestOpenSCAboveTheWALHeadWithoutASnapshotIsRefused(t *testing.T) {
	manager, cfg := openManager(t, disableSS)
	commitBlocks(t, manager, 3)
	closeStateDB(t, manager)
	require.NoError(t, os.RemoveAll(scSnapshotDir(cfg.FlatKVConfig.DataDir, 0)))
	require.NoError(t, statewal.PruneAfter(flatkv.StateWALConfig(cfg.FlatKVConfig.DataDir), 2))

	require.ErrorContains(t, manager.openStateDB(t.Context()), "no snapshot")

	openedAt, err := flatkv.GetWorkingCopyVersion(cfg.FlatKVConfig.DataDir)
	require.NoError(t, err)
	require.Equal(t, int64(3), openedAt, "a refused open must not have rebuilt SC from an empty snapshot")
}

// scSnapshotDir returns where SC keeps the snapshot for version under dataDir.
func scSnapshotDir(dataDir string, version int64) string {
	return filepath.Join(dataDir, fmt.Sprintf("snapshot-%020d", version))
}

// The target a healthy recovery computes is the WAL's own head, where there is nothing to roll back.
// The snapshots above it still have to go: a crash can leave one there, and a later rollback that lands
// on it would replay this branch's blocks over an abandoned one.
func TestRecoverAtTheWALHeadStillDropsSnapshotsAboveIt(t *testing.T) {
	manager, cfg := openManager(t, disableSS)
	commitBlocks(t, manager, 4)
	snapshotSCAt(t, manager, 5)
	closeStateDB(t, manager)
	closeReceiptDB(t, manager)
	require.NoError(t, statewal.PruneAfter(flatkv.StateWALConfig(cfg.FlatKVConfig.DataDir), 3))

	require.NoError(t, manager.recoverStores(t.Context(), 3))

	require.NoDirExists(t, scSnapshotDir(cfg.FlatKVConfig.DataDir, 5))
	require.Equal(t, int64(3), manager.SC().Version())
}

// Snapshot retention eventually reclaims the oldest snapshots, so a target can fall below every one SC
// has left. Nothing can put SC on it then, and the refusal has to say so rather than report the cleanup
// step it happened to fail in.
func TestRecoverBelowEverySCSnapshotIsRefused(t *testing.T) {
	manager, cfg := openManager(t, disableSS)
	commitBlocks(t, manager, 4)
	snapshotSCAt(t, manager, 5)
	closeStateDB(t, manager)
	closeReceiptDB(t, manager)
	require.NoError(t, os.RemoveAll(scSnapshotDir(cfg.FlatKVConfig.DataDir, 0)))

	err := manager.recoverStores(t.Context(), 3)

	require.ErrorContains(t, err, "cannot roll back the state commit store to 3")
}

// A target above where SC sits is not a rollback for it: the WAL's tail is cut to the target and SC
// replays forward from the height it already holds. Its snapshot is far below and stays untouched,
// since landing on it would discard everything committed since and replay the lot back.
func TestRecoverReplaysForwardFromTheStoreOwnHeight(t *testing.T) {
	manager, cfg := openManager(t, disableSS)
	snapshotSCAt(t, manager, 1)
	manager.SC().SetCheckpointScheduler(controller.NewCheckpointScheduler(
		config.CheckpointConfig{BlockInterval: 1_000_000}))
	for block := byte(2); block <= 7; block++ {
		require.NoError(t, manager.StateDB().CommitStateChanges(int64(block), evmBlock(block, block)))
	}
	// Block 8 goes to SC under an address the WAL's own block 8 does not carry. It marks the working
	// copy: a rebuild from the snapshot replays the WAL's block 8 instead and the address is gone.
	require.NoError(t, manager.SC().CommitStateChanges(8, evmBlock(108, 108)))
	writeWALOnly(t, manager.StateWAL(), 8, evmBlock(8, 8))
	writeWALOnly(t, manager.StateWAL(), 9, evmBlock(9, 9))
	writeWALOnly(t, manager.StateWAL(), 10, evmBlock(10, 10))
	require.Equal(t, int64(8), manager.SC().Version())

	reconverge(t, manager, 9)

	require.Equal(t, int64(9), manager.SC().Version())
	requireWALTail(t, manager, 9)
	require.DirExists(t, scSnapshotDir(cfg.FlatKVConfig.DataDir, 1))
	marker, ok := manager.SC().OpenView().Get(evm.EVMStoreKey, evmNonceKey(108))
	require.True(t, ok, "SC replayed forward from 8, so the working copy it held there is still open")
	require.Equal(t, evmNonce(108), marker)
}

// A crash that leaves the WAL a block ahead of the rest of the node makes the target the height the
// stores are already on. Rewinding them to a snapshot for it costs a replay of everything since, and
// for an SS with no snapshot to land on it is not a rewind at all but a wipe.
func TestRecoverLeavesAStoreAlreadyOnTheTargetAlone(t *testing.T) {
	manager, _ := openManager(t, nil)
	commitBlocks(t, manager, 4)
	// A key no WAL block carries, so a wipe loses it where a replay would not put it back.
	require.NoError(t, manager.SS().ApplyChangesetSync(4, evmBlock(9, 9)))
	writeWALOnly(t, manager.StateWAL(), 5, evmBlock(5, 5))

	reconverge(t, manager, 4)

	require.Equal(t, int64(4), manager.SC().Version())
	require.Equal(t, int64(4), manager.SS().GetLatestVersion())
	survived, err := manager.SS().Get(evm.EVMStoreKey, 4, evmNonceKey(9))
	require.NoError(t, err)
	require.Equal(t, evmNonce(9), survived,
		"a rollback to the height SS is already on must not clear it")
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
	manager, _ := openManager(t, disableSS)
	commitBlocks(t, manager, 3)

	reconverge(t, manager, 2)

	require.Equal(t, int64(2), manager.SC().Version())
}

// A rollback target below every WAL block empties the WAL. The stores still have to land on the
// snapshot at the target rather than keep a working copy the empty WAL can no longer account for.
func TestRecoverToATargetThatEmptiesTheWALLandsOnTheSnapshot(t *testing.T) {
	manager, cfg := openManager(t, nil)
	snapshotSSEveryBlock(manager)
	snapshotSCAt(t, manager, 1)
	waitSSSnapshot(t, manager, 1)
	huge := controller.NewCheckpointScheduler(config.CheckpointConfig{BlockInterval: 1_000_000})
	manager.SC().SetCheckpointScheduler(huge)
	manager.SS().SetCheckpointScheduler(huge)
	for block := byte(2); block <= 5; block++ {
		require.NoError(t, manager.StateDB().CommitStateChanges(int64(block), evmBlock(block, block)))
	}
	closeStateDB(t, manager)
	replaceWALWithBlocks(t, cfg, 2, 5)

	reconverge(t, manager, 1)

	require.Equal(t, int64(1), manager.SC().Version())
	require.Equal(t, int64(1), manager.SS().GetLatestVersion())
	requireWALTail(t, manager, 0)
	require.NoError(t, manager.StateDB().CommitStateChanges(2, evmBlock(2, 2)))
}

// A target the WAL no longer spans is refused before snapshots, the WAL tail or the receipt head move,
// so a second attempt at a reachable height still has the history it needs. Receipts are the ones a
// retry cannot recover: recoverReceipt drops bodies and range-deletes the tag index, which no replay
// puts back, so it has to run after the rollback that refuses rather than before it.
func TestRecoverRefusesATargetTheWALCannotSpan(t *testing.T) {
	manager, cfg := openManager(t, disableSS)
	snapshotSCAt(t, manager, 1)
	manager.SC().SetCheckpointScheduler(controller.NewCheckpointScheduler(
		config.CheckpointConfig{BlockInterval: 1_000_000}))
	for block := byte(2); block <= 5; block++ {
		require.NoError(t, manager.StateDB().CommitStateChanges(int64(block), evmBlock(block, block)))
	}
	writeReceipts(t, manager, 5)
	closeStateDB(t, manager)
	replaceWALWithBlocks(t, cfg, 3, 5)

	require.ErrorContains(t, reconvergeErr(t, manager, 2), "replay must start at block 2")

	require.NoError(t, manager.openStateDB(t.Context()))
	require.NoError(t, manager.openReceiptStore())
	require.Equal(t, int64(5), manager.SC().Version(), "a refused rollback must not have moved SC")
	requireWALTail(t, manager, 5)
	require.Equal(t, int64(5), manager.ReceiptDB().LatestVersion(),
		"a refused rollback must not have cut receipts it can no longer reach")
}

// An empty WAL is no evidence about where state belongs, so a plain open leaves both stores holding the
// blocks they committed above their newest snapshot. Dropping them down to that snapshot is what a
// rollback does, from a target; doing it here would take SC down on its own and leave the two stores at
// different heights, with nothing left to reconcile them.
//
// Committing afterwards is the check that they are still aligned, not merely reporting the same height.
func TestOpenWithAnEmptyWALLeavesBothStoresAlone(t *testing.T) {
	manager, cfg := openManager(t, nil)
	huge := controller.NewCheckpointScheduler(config.CheckpointConfig{BlockInterval: 1_000_000})
	manager.SC().SetCheckpointScheduler(huge)
	manager.SS().SetCheckpointScheduler(huge)
	for block := byte(1); block <= 3; block++ {
		require.NoError(t, manager.StateDB().CommitStateChanges(int64(block), evmBlock(block, block)))
	}
	closeStateDB(t, manager)
	require.NoError(t, statewal.PruneAfter(flatkv.StateWALConfig(cfg.FlatKVConfig.DataDir), 0))

	require.NoError(t, manager.openStateDB(t.Context()))

	require.Equal(t, int64(3), manager.SC().Version())
	require.Equal(t, int64(3), manager.SS().GetLatestVersion())
	require.NoError(t, manager.StateDB().CommitStateChanges(4, evmBlock(4, 4)))
}

// A commit store held above the WAL head by a snapshot of its own is rewound to a snapshot boundary at
// or below the target and replayed back up to it, rather than left where it is. Only a snapshot can put
// SC above the head, since a truncated WAL is otherwise what its load lands on; taking one above the
// height the WAL is truncated to is what reaches this.
//
// Committing afterwards is the check that the rewind left both the store and the WAL writable at the
// height it converged on, not merely reporting that height.
func TestRecoverSCAboveTheWALHeadRewindsToASnapshotAndReplays(t *testing.T) {
	manager, _ := openManager(t, disableSS)
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
	waitSSWrites(manager)
	require.Equal(t, int64(2), manager.SS().GetLatestVersion())
	writeWALOnly(t, manager.StateWAL(), 3, evmBlock(3, 3))

	reconverge(t, manager, 3)

	require.Equal(t, int64(3), manager.SS().GetLatestVersion())
	value, err := manager.SS().Get(evm.EVMStoreKey, 3, evmNonceKey(3))
	require.NoError(t, err)
	require.Equal(t, evmNonce(3), value)
}

func TestRecoverSSRollsBackToTheTarget(t *testing.T) {
	manager, _ := openManager(t, nil)
	commitBlocksWithSSSnapshots(t, manager, 3)

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
	commitBlocksWithSSSnapshots(t, manager, 3)
	require.Equal(t, int64(3), manager.SS().Snapshots().Newest())

	reconverge(t, manager, 2)

	require.Equal(t, int64(2), manager.SS().Snapshots().Newest(),
		"a snapshot above the target must not survive the rollback")
	require.Equal(t, int64(2), manager.SS().GetLatestVersion())
}

// Opening at a target rewinds SC, SS and the WAL that feeds them, so the write head lands on the
// target. Committing the block after the target is what proves the WAL was truncated rather than only
// the stores rewound: a WAL still holding that block refuses to write it a second time.
func TestOpenAtATargetRewindsEveryStoreAndTheWAL(t *testing.T) {
	manager, _ := openManager(t, nil)
	commitBlocksWithSSSnapshots(t, manager, 5)

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

// An SS above the target with no snapshot at or below it is emptied and replayed from block 1, the same
// route the plain open takes. A rollback reaches this more readily than an open does: recovery converges
// on the lowest of the block, state and receipt heads, so any crash leaving one of them a block behind
// the WAL lands below an SS that has not checkpointed yet.
func TestRecoverAboveSSWithoutASnapshotRebuildsItFromBlockOne(t *testing.T) {
	manager, _ := openManager(t, nil)
	commitBlocks(t, manager, 3)

	reconverge(t, manager, 2)

	require.Equal(t, int64(2), manager.SC().Version())
	require.Equal(t, int64(2), manager.SS().GetLatestVersion())
	above, err := manager.SS().Get(evm.EVMStoreKey, 2, evmNonceKey(3))
	require.NoError(t, err)
	require.Nil(t, above, "the block above the target must not have survived")
	requireWALTail(t, manager, 2)
	require.NoError(t, manager.StateDB().CommitStateChanges(3, evmBlock(3, 3)))
}

// Refusing is still right once the WAL has had a retention cut: with no snapshot at or below the target
// and no block 1 to replay from, neither route reaches it, and emptying SS would drop history nothing
// can put back.
func TestRecoverAboveSSWithoutASnapshotOrBlockOneIsRefused(t *testing.T) {
	manager, cfg := openManager(t, nil)
	commitBlocks(t, manager, 5)
	closeStateDB(t, manager)
	replaceWALWithBlocks(t, cfg, 3, 5)

	require.ErrorContains(t, reconvergeErr(t, manager, 4), "no longer reaches block 1")

	require.NoError(t, manager.openStateDB(t.Context()))
	require.Equal(t, int64(5), manager.SS().GetLatestVersion(),
		"a refused rollback must not have emptied the EVM state store")
	requireWALTail(t, manager, 5)
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

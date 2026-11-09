package bootstrap

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/apphash/builder"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/controller"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/undo"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

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

func evmNonceKey(addr byte) []byte {
	return append([]byte{0x0a}, append([]byte{addr}, make([]byte, 19)...)...)
}

func evmNonce(value byte) []byte {
	return append(make([]byte, 7), value)
}

// evmBlock returns a block that sets the nonce of the account evmNonceKey(addr) names to nonce.
func evmBlock(addr byte, nonce byte) *gigatypes.StateChangeSet {
	return &gigatypes.StateChangeSet{Accounts: []gigatypes.AccountChange{{
		Address: gigatypes.Address{addr},
		Account: gigatypes.AccountState{Nonce: uint64(nonce)},
	}}}
}

// testBlockHash is the block hash execution would report for height. It is a function of the height
// alone, so a block re-executed after a restart reports the same hash the vault already stored.
func testBlockHash(height uint64) [32]byte {
	var blockHash [32]byte
	binary.BigEndian.PutUint64(blockHash[:8], height)
	return blockHash
}

// reportTestBlockHash reports height's block hash to the app hash builder, as execution does before
// running the block.
func reportTestBlockHash(t *testing.T, manager *GigaStorageManager, height uint64) {
	t.Helper()
	require.NoError(t, manager.AppHashBuilder().ReportBlockHash(t.Context(), height, testBlockHash(height)))
}

// commitBlocks commits blocks 1 through through, and waits until their app hashes are stored.
func commitBlocks(t *testing.T, manager *GigaStorageManager, through byte) {
	t.Helper()
	for block := byte(1); block <= through; block++ {
		reportTestBlockHash(t, manager, uint64(block))
		commitBlock(t, manager, int64(block), evmBlock(block, block))
	}
	awaitAppHashes(t, manager, uint64(through))
}

// commitBlock commits changes as block height and, while a receipt hasher is open, submits an empty receipt
// list for the block.
func commitBlock(t *testing.T, manager *GigaStorageManager, height int64, changes *gigatypes.StateChangeSet) {
	t.Helper()
	require.NoError(t, manager.StateDB().CommitStateChanges(height, changes))
	if manager.receiptHasher != nil {
		blockHeight := uint64(height) //nolint:gosec // small test heights
		require.NoError(t, manager.receiptHasher.Submit(t.Context(), blockHeight, nil))
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

// writeWALOnly writes block to the state WAL alone, converting changes as though no key they write existed
// before the block.
func writeWALOnly(t *testing.T, wal statewal.StateWAL, block uint64, changes *gigatypes.StateChangeSet) {
	t.Helper()
	//nolint:gosec // small test heights
	mutations, err := flatkv.MutationsFromStateChangeSet(changes, block, emptyStateView{height: int64(block) - 1},
		flatkvconfig.DefaultConfig())
	require.NoError(t, err)
	require.NoError(t, wal.Write(block, mutations))
	require.NoError(t, wal.Flush())
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

// closeStateDB closes the stores the StateDB owns, then the app hash builder and the hashers reporting into it,
// and drops them from the manager, as a restart does. It does nothing when no StateDB is open. Manager.Close
// tolerates the result, so a test may still defer it.
func closeStateDB(t *testing.T, manager *GigaStorageManager) {
	t.Helper()
	if manager.StateDB() == nil {
		return
	}
	require.NoError(t, manager.StateDB().Close())
	manager.stateDB = nil
	// A builder left open would be handed the blocks committed after the state DB reopens, which it has already
	// moved past.
	closeAppHashBuilder(t, manager)
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
//
// The blocks committed before this are flushed through the writer first. A block is offered to the
// writer on commit but asked about on the writer's goroutine, against whichever schedule is installed
// when it gets there, so one still queued when the every-block schedule goes in would be snapshotted
// too.
func snapshotSCAt(t *testing.T, manager *GigaStorageManager, height byte) {
	t.Helper()
	require.NoError(t, manager.SC().FlushSnapshots())
	manager.SC().SetCheckpointScheduler(controller.NewCheckpointScheduler(config.CheckpointConfig{BlockInterval: 1}))
	commitBlock(t, manager, int64(height), evmBlock(height, height))
	require.NoError(t, manager.SC().FlushSnapshots())
}

// historicalStateDB returns the manager's historical state DB.
func historicalStateDB(manager *GigaStorageManager) *undo.Database {
	return manager.StateDB().UndoSS()
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
		name                string
		blockHeight         uint64
		stateHeight         uint64
		historicalStateHead utils.Option[uint64]
		receiptHead         uint64
		appHashHead         uint64
		want                uint64
		wantErr             bool
	}{
		{name: "a fresh node has no height to converge on"},
		{name: "the lowest head wins", blockHeight: 7, stateHeight: 5, receiptHead: 6, appHashHead: 9, want: 5},
		{name: "receipts can be the lowest",
			blockHeight: 7, stateHeight: 6, receiptHead: 4, appHashHead: 9, want: 4},
		{name: "the historical state DB can be the lowest",
			blockHeight: 7, stateHeight: 7, historicalStateHead: utils.Some[uint64](3), receiptHead: 6,
			appHashHead: 9, want: 3},
		{name: "a historical state DB at block 0 is the lowest",
			blockHeight: 7, stateHeight: 7, historicalStateHead: utils.Some[uint64](0), receiptHead: 6,
			appHashHead: 9, want: 0},
		// A historical state DB newly enabled, or disabled, holds nothing to disagree with. Folding its 0 into
		// the minimum would skip recovery for the stores that do have history.
		{name: "an empty historical state DB does not collapse the target",
			blockHeight: 7, stateHeight: 5, receiptHead: 6, appHashHead: 9, want: 5},
		// The regression: receipts newly enabled, or a receipt directory recreated after corruption,
		// leave a head of 0 alongside real block and state history. Folding that 0 into the minimum
		// collapses the target and skips recovery for the stores that do have history.
		{name: "an empty receipt store does not collapse the target",
			blockHeight: 7, stateHeight: 5, appHashHead: 9, want: 5},
		{name: "a disabled receipt store reads the same as an empty one",
			blockHeight: 4, stateHeight: 4, appHashHead: 9, want: 4},
		{name: "an empty state WAL yields no target", blockHeight: 7, receiptHead: 7},
		{name: "an empty block store yields no target", stateHeight: 7, receiptHead: 7},
		{name: "the app hash vault can be the lowest", blockHeight: 7, stateHeight: 5, appHashHead: 4, want: 4},
		// An empty vault's head is the block before the giga activation height, which holds the stores there.
		{name: "an empty app hash vault holds the target below a later giga activation",
			blockHeight: 7, stateHeight: 7, appHashHead: 3, want: 3},
		{name: "an empty app hash vault at giga activation 1 refuses stores holding blocks",
			blockHeight: 7, stateHeight: 5, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := recoveryTarget(
				tc.blockHeight, tc.stateHeight, tc.historicalStateHead, tc.receiptHead, tc.appHashHead)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
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
	// The version marker rides SetReceipts, so it is off the store's interface; this test stamps a
	// head without bodies on purpose.
	pinner, ok := manager.ReceiptDB().(receipt.VersionPinner)
	require.True(t, ok)
	require.NoError(t, pinner.SetLatestVersion(3))
	// findTargetRecoveryHeight reads the state, receipt and app hash directories offline, so those
	// stores have to be closed for it.
	stopForRestart(t, manager)
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
	commitBlock(t, manager, 4, evmBlock(4, 4))
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
	commitBlock(t, manager, 4, evmBlock(4, 4))
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
	commitBlock(t, manager, 3, evmBlock(3, 3))
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
	commitBlock(t, manager, 4, evmBlock(4, 4))
}

// The historical state DB is cut back to the WAL's head along with the live state DB, its records of the
// blocks above the head going with them.
func TestOpenCutsTheHistoricalStateDBBackToTheWALHead(t *testing.T) {
	manager, cfg := openManager(t, nil)
	commitBlocks(t, manager, 3)
	closeStateDB(t, manager)
	require.NoError(t, statewal.PruneAfter(flatkv.StateWALConfig(cfg.FlatKVConfig.DataDir), 2))

	require.NoError(t, manager.openStateDB(t.Context()))

	require.Equal(t, int64(2), historicalStateDB(manager).GetLatestVersion())
	commitBlock(t, manager, 3, evmBlock(3, 3))
}

// An SC above the WAL head with no snapshot at or below it cannot be rewound. Rebuilding the working
// copy from an empty snapshot would delete the history it holds; refusing leaves that history in place.
func TestOpenSCAboveTheWALHeadWithoutASnapshotIsRefused(t *testing.T) {
	manager, cfg := openManager(t, disableSS)
	commitBlocks(t, manager, 3)
	closeStateDB(t, manager)
	require.NoError(t, os.RemoveAll(scSnapshotDir(cfg.FlatKVConfig.DataDir, 0)))
	require.NoError(t, statewal.PruneAfter(flatkv.StateWALConfig(cfg.FlatKVConfig.DataDir), 2))

	err := manager.openStateDB(t.Context())

	require.ErrorContains(t, err, "no snapshot")
	// The operator is here because a node will not start. Naming a rollback would send them looking for
	// one nobody ran, rather than at the WAL head the open refused.
	require.ErrorContains(t, err, "cannot open on the state WAL's head 2")
	require.NotContains(t, err.Error(), "roll back")

	openedAt, _, err := flatkv.StoredVersions(cfg.FlatKVConfig.DataDir)
	require.NoError(t, err)
	require.Equal(t, int64(3), openedAt, "a refused open must not have rebuilt SC from an empty snapshot")
}

// scSnapshotDir returns where SC keeps the snapshot for version under dataDir.
func scSnapshotDir(dataDir string, version int64) string {
	return filepath.Join(dataDir, fmt.Sprintf("snapshot-%020d", version))
}

// A snapshot above the target has to go even when SC sits at or below the target, which is the one
// state the rewind cannot sweep up: nothing moves SC, and it is the rewind that otherwise removes the
// snapshots above where it lands.
//
// An interrupted rewind is what leaves that state. SC repoints its current link before it removes the
// snapshots above, so a crash in between abandons a branch while SC reads as merely behind. A later
// rollback landing on one of that branch's snapshots would replay over it.
func TestRecoverDropsASnapshotAboveASCAlreadyAtTheTarget(t *testing.T) {
	manager, cfg := openManager(t, disableSS)
	dataDir := cfg.FlatKVConfig.DataDir
	commitBlocks(t, manager, 2)
	snapshotSCAt(t, manager, 3)
	closeStateDB(t, manager)
	closeReceiptDB(t, manager)
	// The snapshot the interrupted rewind failed to remove. Its contents do not matter, only that a
	// later rollback could land on it.
	require.NoError(t, os.CopyFS(scSnapshotDir(dataDir, 5), os.DirFS(scSnapshotDir(dataDir, 3))))
	require.DirExists(t, scSnapshotDir(dataDir, 5), "the fixture must plant the snapshot it expects gone")

	require.NoError(t, manager.recoverStores(t.Context(), 3))

	require.NoDirExists(t, scSnapshotDir(dataDir, 5),
		"a rollback that never moved SC still has to drop the branch abandoned above it")
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

	require.ErrorContains(t, err, "cannot roll back to 3")
	require.ErrorContains(t, err, "the state commit store cannot reach 3")
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
		commitBlock(t, manager, int64(block), evmBlock(block, block))
	}
	// Block 8 goes to SC under an address the WAL's own block 8 does not carry. It marks the working
	// copy: a rebuild from the snapshot replays the WAL's block 8 instead and the address is gone.
	prior := manager.SC().OpenView()
	marked, err := flatkv.MutationsFromStateChangeSet(evmBlock(108, 108), 8, prior, flatkvconfig.DefaultConfig())
	prior.Close()
	require.NoError(t, err)
	require.NoError(t, manager.SC().CommitStateChanges(8, marked))
	writeWALOnly(t, manager.StateWAL(), 8, evmBlock(8, 8))
	writeWALOnly(t, manager.StateWAL(), 9, evmBlock(9, 9))
	writeWALOnly(t, manager.StateWAL(), 10, evmBlock(10, 10))
	require.Equal(t, int64(8), manager.SC().Version())

	reconverge(t, manager, 9)

	require.Equal(t, int64(9), manager.SC().Version())
	requireWALTail(t, manager, 9)
	require.DirExists(t, scSnapshotDir(cfg.FlatKVConfig.DataDir, 1))
	view := manager.SC().OpenView()
	defer view.Close()
	marker, ok := view.Get(keys.EVMStoreKey, evmNonceKey(108))
	require.True(t, ok, "SC replayed forward from 8, so the working copy it held there is still open")
	require.Equal(t, evmNonce(108), marker)
}

func TestRecoverSCReplaysAMissedWALBlock(t *testing.T) {
	manager, _ := openManager(t, disableSS)
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
	snapshotSCAt(t, manager, 1)
	manager.SC().SetCheckpointScheduler(controller.NewCheckpointScheduler(
		config.CheckpointConfig{BlockInterval: 1_000_000}))
	for block := byte(2); block <= 5; block++ {
		commitBlock(t, manager, int64(block), evmBlock(block, block))
	}
	closeStateDB(t, manager)
	replaceWALWithBlocks(t, cfg, 2, 5)

	reconverge(t, manager, 1)

	require.Equal(t, int64(1), manager.SC().Version())
	require.Equal(t, int64(1), historicalStateDB(manager).GetLatestVersion())
	requireWALTail(t, manager, 0)
	commitBlock(t, manager, 2, evmBlock(2, 2))
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
		commitBlock(t, manager, int64(block), evmBlock(block, block))
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
	manager.SC().SetCheckpointScheduler(controller.NewCheckpointScheduler(
		config.CheckpointConfig{BlockInterval: 1_000_000}))
	for block := byte(1); block <= 3; block++ {
		commitBlock(t, manager, int64(block), evmBlock(block, block))
	}
	closeStateDB(t, manager)
	require.NoError(t, statewal.PruneAfter(flatkv.StateWALConfig(cfg.FlatKVConfig.DataDir), 0))

	require.NoError(t, manager.openStateDB(t.Context()))

	require.Equal(t, int64(3), manager.SC().Version())
	require.Equal(t, int64(3), historicalStateDB(manager).GetLatestVersion())
	commitBlock(t, manager, 4, evmBlock(4, 4))
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
	commitBlock(t, manager, 3, evmBlock(3, 3))
}

// The historical state DB's records above the target are of blocks that run again, possibly differently,
// so they go.
func TestRecoverSSRollsBackToTheTarget(t *testing.T) {
	manager, _ := openManager(t, nil)
	commitBlocks(t, manager, 3)

	reconverge(t, manager, 2)

	require.Equal(t, int64(2), historicalStateDB(manager).GetLatestVersion())
	_, ok := manager.StateDB().OpenViewAt(3)
	require.False(t, ok, "a height above the target must not be served")
	commitBlock(t, manager, 3, evmBlock(3, 3))
}

// A crash that loses the historical state DB's write queue leaves it behind the other stores, and its head
// then sets the target: every store rolls back to it, keeping the history it holds, and the blocks above
// are executed again.
func TestRecoveryConvergesOnAHistoricalStateDBBehindTheOthers(t *testing.T) {
	manager, cfg := openManager(t, nil)
	commitBlocks(t, manager, 5)
	stored := publishedAppHashes(t, manager, 1, 5)
	stopForRestart(t, manager)
	closeReceiptDB(t, manager)
	require.NoError(t, undo.DiscardStateAbove(cfg.SSConfig.EVMDBDirectory, cfg.SSConfig, 3))

	present, historicalStateHead, err := undo.GetLatestBlock(cfg.SSConfig.EVMDBDirectory)
	require.NoError(t, err)
	require.True(t, present)
	walHead, err := manager.stateWALHead()
	require.NoError(t, err)
	appHashHead, err := builder.GetLatestBlock(*cfg.AppHashBuilderConfig)
	require.NoError(t, err)
	target, err := recoveryTarget(5, walHead, utils.Some(historicalStateHead), 0, appHashHead)
	require.NoError(t, err)
	require.Equal(t, uint64(3), target)
	restartAt(t, manager, int64(target)) //nolint:gosec // small test heights

	require.Equal(t, int64(3), manager.SC().Version())
	require.Equal(t, int64(3), historicalStateDB(manager).GetLatestVersion())
	requireWALTail(t, manager, 3)

	for block := byte(4); block <= 5; block++ {
		reportTestBlockHash(t, manager, uint64(block))
		commitBlock(t, manager, int64(block), evmBlock(block, block))
	}
	rebuilt := publishedAppHashes(t, manager, 4, 5)
	require.Equal(t, stored[3].AppHash(), rebuilt[0].AppHash())
	require.Equal(t, stored[4].AppHash(), rebuilt[1].AppHash())
	historicalStateDB(manager).WaitForPendingWrites()
	require.Zero(t, historicalStateDB(manager).GetEarliestVersion(), "the history it held must survive")
	for height := int64(0); height <= 5; height++ {
		view, ok := manager.StateDB().OpenViewAt(height)
		require.True(t, ok, "height %d", height)
		for addr := byte(1); addr <= 5; addr++ {
			want := uint64(0)
			if int64(addr) <= height {
				want = uint64(addr)
			}
			require.Equal(t, want, view.GetNonce(gigatypes.Address{addr}), "account %d at %d", addr, height)
		}
		view.Close()
	}
}

// Opening at a target rewinds SC, SS and the WAL that feeds them, so the write head lands on the
// target. Committing the block after the target is what proves the WAL was truncated rather than only
// the stores rewound: a WAL still holding that block refuses to write it a second time.
func TestOpenAtATargetRewindsEveryStoreAndTheWAL(t *testing.T) {
	manager, _ := openManager(t, nil)
	commitBlocks(t, manager, 5)

	reconverge(t, manager, 2)

	require.Equal(t, int64(2), manager.SC().Version())
	require.Equal(t, int64(2), historicalStateDB(manager).GetLatestVersion())
	requireWALTail(t, manager, 2)

	commitBlock(t, manager, 3, evmBlock(3, 3))
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

// A crash while the historical state DB was being created leaves its directory holding no database.
// Startup removes it and opens the historical state DB as a new one.
func TestStartupRemovesAHalfCreatedHistoricalStateDB(t *testing.T) {
	cfg, err := config.DefaultGigaStorageConfig(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(cfg.SSConfig.EVMDBDirectory, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(cfg.SSConfig.EVMDBDirectory, "LOCK"), nil, 0o600))

	manager, err := NewGigaStorageManager(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, manager.Close()) })

	require.NotNil(t, historicalStateDB(manager))
	commitBlocks(t, manager, 1)
}

func TestOpenDBWithoutRecoveryOnAFreshHome(t *testing.T) {
	manager, _ := openManager(t, nil)

	require.Zero(t, manager.SC().Version())
	require.Zero(t, historicalStateDB(manager).GetLatestVersion())
	require.Zero(t, manager.ReceiptDB().LatestVersion())
}

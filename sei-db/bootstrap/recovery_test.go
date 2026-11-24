package bootstrap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	"github.com/sei-protocol/sei-chain/sei-db/archive/chstore"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/clickhouse"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/undo"
	"github.com/sei-protocol/sei-chain/sei-db/receipt_db/littidx"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

// reportTestBlockHash reports height's block hash to the app hash builder, as execution does before
// running the block.
func reportTestBlockHash(t *testing.T, manager *GigaStorageManager, height uint64) {
	t.Helper()
	require.NoError(t, manager.AppHashBuilder().ReportBlockHash(t.Context(), height, testBlockHash(height)))
}

// commitBlocks commits blocks 1 through through, and waits until their app hashes are published.
func commitBlocks(t *testing.T, manager *GigaStorageManager, through byte) {
	t.Helper()
	for block := byte(1); block <= through; block++ {
		commitBlock(t, manager, int64(block), evmBlock(block, block))
	}
	awaitAppHashes(t, manager, uint64(through))
}

// commitBlock feeds block height to every store as execution does: it reports the block's hash, commits changes,
// writes an empty receipt list, and submits that list to the receipt hasher.
func commitBlock(t *testing.T, manager *GigaStorageManager, height int64, changes *gigatypes.StateChangeSet) {
	t.Helper()
	blockHeight := uint64(height) //nolint:gosec // small test heights
	reportTestBlockHash(t, manager, blockHeight)
	require.NoError(t, manager.StateDB().CommitStateChanges(height, changes))
	writeBlockReceipts(t, manager, height)
	require.NoError(t, manager.ReceiptHasher().Submit(t.Context(), blockHeight, nil))
}

// writeBlockReceipts writes an empty receipt list for block height to the manager's receipt store, if it has one.
func writeBlockReceipts(t *testing.T, manager *GigaStorageManager, height int64) {
	t.Helper()
	receiptDB := manager.ReceiptDB()
	if receiptDB == nil {
		return
	}
	storeKey := storetypes.NewKVStoreKey("evm")
	ctx := testutil.DefaultContext(storeKey, storetypes.NewTransientStoreKey("evm_transient"))
	require.NoError(t, receiptDB.SetReceipts(ctx.WithBlockHeight(height), nil))
}

// historicalStateDB returns the manager's historical state DB.
func historicalStateDB(manager *GigaStorageManager) gigatypes.HistoricalStateStore {
	return manager.StateDB().HistoricalStateDB()
}

// disableSS turns the EVM state store off.
func disableSS(cfg *config.GigaStorageConfig) {
	cfg.SSConfig.Enable = false
}

func requireWALTail(t *testing.T, manager *GigaStorageManager, want uint64) {
	t.Helper()
	stored, _, last, err := manager.StateDB().WAL().GetStoredRange()
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
		name  string
		heads storeHeads
		want  uint64
	}{
		{name: "a fresh node aligns on 0"},
		{name: "the lowest head wins",
			heads: storeHeads{stateWAL: 5, receiptStore: 6, appHashVault: 9}, want: 5},
		{name: "receipts can be the lowest",
			heads: storeHeads{stateWAL: 6, receiptStore: 4, appHashVault: 9}, want: 4},
		{name: "the historical state DB can be the lowest",
			heads: storeHeads{stateWAL: 7, historicalStateDB: 3, receiptStore: 6, appHashVault: 9},
			want:  3},
		// A historical state DB newly enabled, or disabled, holds nothing to disagree with. Folding its 0 into
		// the minimum would skip recovery for the stores that do have history.
		{name: "an empty historical state DB does not collapse the target",
			heads: storeHeads{stateWAL: 5, receiptStore: 6, appHashVault: 9}, want: 5},
		// The regression: receipts newly enabled, or a receipt directory recreated after corruption,
		// leave a head of 0 alongside real state history. Folding that 0 into the minimum collapses the
		// target and skips recovery for the stores that do have history.
		{name: "an empty receipt store does not collapse the target",
			heads: storeHeads{stateWAL: 5, appHashVault: 9}, want: 5},
		{name: "an empty state WAL over an empty live state DB aligns on 0",
			heads: storeHeads{receiptStore: 7, appHashVault: 9}, want: 0},
		// A state transplant: state copied in with no WAL to replay.
		{name: "an empty state WAL counts as the live state DB's version",
			heads: storeHeads{liveStateDB: 5, receiptStore: 7, appHashVault: 9}, want: 5},
		{name: "a state WAL holding blocks counts over the live state DB's version",
			heads: storeHeads{stateWAL: 7, liveStateDB: 5, appHashVault: 9}, want: 7},
		{name: "the app hash vault can be the lowest",
			heads: storeHeads{stateWAL: 5, appHashVault: 4}, want: 4},
		// An empty vault's head is the block before the giga activation height, which holds the stores there.
		{name: "an empty app hash vault holds the target below a later giga activation",
			heads: storeHeads{stateWAL: 7, appHashVault: 3}, want: 3},
		{name: "an empty app hash vault at giga activation 1 aligns on 0",
			heads: storeHeads{stateWAL: 5}, want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, recoveryTarget(tc.heads))
		})
	}
}

func TestRequireStateWALReplayableFromTheLiveStateDB(t *testing.T) {
	for _, tc := range []struct {
		name  string
		heads storeHeads
		// A substring of the error, or empty when the heads pass.
		wantErr string
	}{
		{name: "an empty state WAL passes", heads: storeHeads{liveStateDB: 5}},
		{name: "a state WAL starting at the block after the live state DB passes",
			heads: storeHeads{stateWALFirst: 6, stateWAL: 9, liveStateDB: 5}},
		{name: "a state WAL starting below the live state DB passes",
			heads: storeHeads{stateWALFirst: 1, stateWAL: 9, liveStateDB: 5}},
		// A rollback rewinds the live state DB to a snapshot, and checks that one exists.
		{name: "a live state DB above the state WAL's head passes",
			heads: storeHeads{stateWALFirst: 1, stateWAL: 3, liveStateDB: 5}},
		{name: "a fresh live state DB under a state WAL from block 1 passes",
			heads: storeHeads{stateWALFirst: 1, stateWAL: 3}},
		{name: "a state WAL starting past the block after the live state DB is refused",
			heads:   storeHeads{stateWALFirst: 8, stateWAL: 9, liveStateDB: 5},
			wantErr: "blocks 6-7 are missing"},
		{name: "a fresh live state DB under a state WAL from block 2 is refused",
			heads:   storeHeads{stateWALFirst: 2, stateWAL: 3},
			wantErr: "blocks 1-1 are missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := requireStateWALReplayableFromTheLiveStateDB(tc.heads)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

// A state WAL starting past the block after the live state DB's version has lost blocks the live state DB needs,
// and startup refuses it with every store left as it was found.
func TestAStateWALTheLiveStateDBCannotReplayIsRefused(t *testing.T) {
	cfg := newHome(t, disableSS)
	stateDB := openStandaloneStateDB(t, cfg)
	commitStateBlocks(t, stateDB, 1, 2)
	require.NoError(t, stateDB.Close())
	replaceWALWithBlocks(t, cfg, 5, 7)
	writeBlockLedger(t, cfg, 7)
	writeReceipts(t, cfg, 7)
	writeAppHashes(t, cfg, 7)

	_, err := NewGigaStorageManager(t.Context(), cfg)

	require.ErrorContains(t, err, "blocks 3-4 are missing")
	walFirst, walHead, err := stateWALRange(cfg)
	require.NoError(t, err)
	require.Equal(t, uint64(5), walFirst, "a refused start must not have moved the state WAL")
	require.Equal(t, uint64(7), walHead, "a refused start must not have moved the state WAL")
	receiptHead, err := littidx.GetLatestBlock(cfg.ReceiptDBConfig)
	require.NoError(t, err)
	require.Equal(t, uint64(7), receiptHead, "a refused start must not have cut receipts")
}

func TestRequireNoStoreAheadOfTheBlockStore(t *testing.T) {
	for _, tc := range []struct {
		name           string
		blockStoreHead uint64
		heads          storeHeads
		// The first block whose app hash the vault stores. Zero is read as 1.
		gigaActivationHeight uint64
		// A substring of the error, or empty when the heads pass.
		wantErr string
	}{
		{name: "stores at the block store pass", blockStoreHead: 5,
			heads: storeHeads{stateWAL: 5, historicalStateDB: 5, receiptStore: 5, appHashVault: 5}},
		{name: "stores behind the block store pass", blockStoreHead: 9,
			heads: storeHeads{stateWAL: 5, receiptStore: 4, appHashVault: 3}},
		// A home bootstrapped from copied state, or restored from a snapshot, has no blocks yet.
		{name: "an empty block store passes stores holding blocks",
			heads: storeHeads{stateWAL: 5, historicalStateDB: 5, receiptStore: 5, appHashVault: 5}},
		{name: "a state WAL ahead is refused", blockStoreHead: 3,
			heads: storeHeads{stateWAL: 4}, wantErr: "the state WAL holds block 4"},
		{name: "a historical state DB ahead is refused", blockStoreHead: 3,
			heads:   storeHeads{historicalStateDB: 4},
			wantErr: "the historical state DB holds block 4"},
		{name: "a receipt store ahead is refused", blockStoreHead: 3,
			heads: storeHeads{receiptStore: 4}, wantErr: "the receipt store holds block 4"},
		{name: "an app hash vault ahead is refused", blockStoreHead: 3,
			heads: storeHeads{appHashVault: 4}, wantErr: "the app hash vault holds block 4"},
		// An empty vault reports the block before the giga activation height, which it holds no record of.
		{name: "an empty app hash vault below a later giga activation passes", blockStoreHead: 3,
			heads: storeHeads{appHashVault: 9}, gigaActivationHeight: 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			activation := max(tc.gigaActivationHeight, 1)
			err := requireNoStoreAheadOfTheBlockStore(tc.blockStoreHead, tc.heads, activation)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

// A store ahead of the block store means the block store's durability broke, and startup takes no action on it:
// every store is left as it was found, for an operator to investigate.
func TestAStoreAheadOfTheBlockStoreIsRefused(t *testing.T) {
	cfg := newHome(t, nil)
	writeBlockLedger(t, cfg, 3)
	writeState(t, cfg, 5)
	writeReceipts(t, cfg, 5)
	writeAppHashes(t, cfg, 5)

	_, err := NewGigaStorageManager(t.Context(), cfg)

	require.ErrorContains(t, err, "but the block store ends at block 3")
	_, walHead, err := stateWALRange(cfg)
	require.NoError(t, err)
	require.Equal(t, uint64(5), walHead, "a refused start must not have moved the state WAL")
	receiptHead, err := littidx.GetLatestBlock(cfg.ReceiptDBConfig)
	require.NoError(t, err)
	require.Equal(t, uint64(5), receiptHead, "a refused start must not have cut receipts")
}

// A home bootstrapped from copied state has no blocks yet. Its other stores are still aligned with each other.
func TestAnEmptyBlockStoreStillAlignsTheOtherStores(t *testing.T) {
	cfg := newHome(t, nil)
	writeState(t, cfg, 3)
	// The receipt store behind the others sets the target.
	writeReceipts(t, cfg, 2)
	writeAppHashes(t, cfg, 3)

	manager := startManager(t, cfg)

	require.Equal(t, int64(2), manager.StateDB().LiveStateDB().Version())
	require.Equal(t, int64(2), manager.ReceiptDB().LatestVersion())
	commitBlock(t, manager, 3, evmBlock(3, 3))
}

// A state transplant leaves the live state DB at a height with an empty state WAL, which stands at any height, so
// the live state DB's version sets the target and the receipt store above it rolls back to it.
func TestAnEmptyWALAlignsTheOtherStoresOnTheLiveStateDB(t *testing.T) {
	cfg := newHome(t, nil)
	writeBlockLedger(t, cfg, 5)
	writeState(t, cfg, 3)
	truncateWAL(t, cfg, 0)
	writeReceipts(t, cfg, 5)
	writeAppHashes(t, cfg, 3)

	manager := startManager(t, cfg)

	require.Equal(t, int64(3), manager.StateDB().LiveStateDB().Version())
	require.Equal(t, int64(3), manager.ReceiptDB().LatestVersion())
	commitBlock(t, manager, 4, evmBlock(4, 4))
}

// A new chain that crashes with blocks in the state WAL before the app hash vault stored any app hash can only
// recover by rolling every store back to 0.
func TestStateAheadOfAnEmptyAppHashVaultRecoversFromBlock0(t *testing.T) {
	cfg := newHome(t, nil)
	writeBlockLedger(t, cfg, 5)
	writeState(t, cfg, 5)
	writeReceipts(t, cfg, 5)

	manager := startManager(t, cfg)

	require.Zero(t, manager.StateDB().LiveStateDB().Version())
	require.Zero(t, historicalStateDB(manager).GetLatestVersion())
	require.Zero(t, manager.ReceiptDB().LatestVersion())
	requireWALTail(t, manager, 0)
	commitBlocks(t, manager, 1)
}

// A new chain that crashes with receipts on disk but no state recovers by rolling the receipt store back to 0.
func TestReceiptsAheadOfEmptyStateRecoverFromBlock0(t *testing.T) {
	cfg := newHome(t, nil)
	writeBlockLedger(t, cfg, 3)
	writeReceipts(t, cfg, 3)

	manager := startManager(t, cfg)

	require.Zero(t, manager.StateDB().LiveStateDB().Version())
	require.Zero(t, manager.ReceiptDB().LatestVersion())
	commitBlocks(t, manager, 1)
}

// A new chain that crashes with history in the historical state DB but no live state recovers by rolling the
// historical state DB back to 0.
func TestHistoricalStateDBAheadOfEmptyStateRecoversFromBlock0(t *testing.T) {
	cfg := newHome(t, nil)
	writeBlockLedger(t, cfg, 3)
	writeState(t, cfg, 3)
	// Drops the live state DB and the state WAL under it, leaving only the historical state DB's history.
	require.NoError(t, os.RemoveAll(cfg.FlatKVConfig.DataDir))

	manager := startManager(t, cfg)

	require.Zero(t, manager.StateDB().LiveStateDB().Version())
	require.Zero(t, historicalStateDB(manager).GetLatestVersion())
	commitBlocks(t, manager, 1)
}

// The stores a rollback to 0 leaves behind align on 0 again at the next startup, and that alignment changes
// nothing.
func TestRestartAfterARollbackToBlock0(t *testing.T) {
	cfg := newHome(t, nil)
	writeBlockLedger(t, cfg, 5)
	writeState(t, cfg, 5)
	writeReceipts(t, cfg, 5)
	first, err := NewGigaStorageManager(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, first.Close())

	manager := startManager(t, cfg)

	require.Zero(t, manager.StateDB().LiveStateDB().Version())
	require.Zero(t, historicalStateDB(manager).GetLatestVersion())
	require.Zero(t, manager.ReceiptDB().LatestVersion())
	requireWALTail(t, manager, 0)
	commitBlocks(t, manager, 1)
}

// Aligning a fresh home on 0 must not create a receipt store before the receipt store itself opens one.
func TestAligningAFreshHomeCreatesNoReceiptStore(t *testing.T) {
	cfg := newHome(t, nil)

	require.NoError(t, alignStores(t.Context(), cfg, 0))

	require.NoDirExists(t, cfg.ReceiptDBConfig.DBDirectory)
}

// Recovering to a target below the WAL head drops every block above it, so the write head resumes at
// the target. Committing the block after it is what proves the truncation: an untruncated WAL still
// holds that block and refuses to write it a second time.
func TestRecoverStateDropsWALBlocksAboveTheTarget(t *testing.T) {
	cfg := newHome(t, disableSS)
	writeBlockLedger(t, cfg, 5)
	writeState(t, cfg, 5)
	// The receipt store behind the others sets the target.
	writeReceipts(t, cfg, 3)
	writeAppHashes(t, cfg, 5)

	manager := startManager(t, cfg)

	requireWALTail(t, manager, 3)
	commitBlock(t, manager, 4, evmBlock(4, 4))
}

// A snapshot above the target has to go even when the live state DB sits at or below the target, which is the
// one state the rewind cannot sweep up: nothing moves the live state DB, and it is the rewind that otherwise
// removes the snapshots above where it lands.
//
// An interrupted rewind is what leaves that state. The live state DB repoints its current link before it removes
// the snapshots above, so a crash in between abandons a branch while the live state DB reads as merely behind. A
// later rollback landing on one of that branch's snapshots would replay over it.
func TestRecoverDropsASnapshotAboveALiveStateDBAlreadyAtTheTarget(t *testing.T) {
	cfg := newHome(t, disableSS)
	dataDir := cfg.FlatKVConfig.DataDir
	stateDB := openStandaloneStateDB(t, cfg)
	commitStateBlocks(t, stateDB, 1, 2)
	snapshotLiveStateDBAt(t, stateDB, 3)
	require.NoError(t, stateDB.Close())
	// The snapshot the interrupted rewind failed to remove. Its contents do not matter, only that a
	// later rollback could land on it.
	require.NoError(t, os.CopyFS(liveStateDBSnapshotDir(dataDir, 5), os.DirFS(liveStateDBSnapshotDir(dataDir, 3))))
	require.DirExists(t, liveStateDBSnapshotDir(dataDir, 5), "the fixture must plant the snapshot it expects gone")
	writeBlockLedger(t, cfg, 3)
	writeReceipts(t, cfg, 3)
	writeAppHashes(t, cfg, 3)

	manager := startManager(t, cfg)

	require.NoDirExists(t, liveStateDBSnapshotDir(dataDir, 5),
		"a rollback that never moved the live state DB still has to drop the branch abandoned above it")
	require.Equal(t, int64(3), manager.StateDB().LiveStateDB().Version())
}

// Snapshot retention eventually reclaims the oldest snapshots, so a target can fall below every one the live
// state DB has left. Nothing can put the live state DB on it then, and the refusal has to say so rather than
// report the cleanup step it happened to fail in.
func TestRecoverBelowEveryLiveStateDBSnapshotIsRefused(t *testing.T) {
	cfg := newHome(t, disableSS)
	stateDB := openStandaloneStateDB(t, cfg)
	commitStateBlocks(t, stateDB, 1, 4)
	snapshotLiveStateDBAt(t, stateDB, 5)
	require.NoError(t, stateDB.Close())
	require.NoError(t, os.RemoveAll(liveStateDBSnapshotDir(cfg.FlatKVConfig.DataDir, 0)))
	writeBlockLedger(t, cfg, 5)
	writeReceipts(t, cfg, 5)
	// The app hash vault behind the others sets the target.
	writeAppHashes(t, cfg, 3)

	_, err := NewGigaStorageManager(t.Context(), cfg)

	require.ErrorContains(t, err, "cannot roll back to 3")
	require.ErrorContains(t, err, "the live state DB cannot reach 3")
}

func TestRecoverLiveStateDBReplaysAMissedWALBlock(t *testing.T) {
	cfg := newHome(t, disableSS)
	stateDB := openStandaloneStateDB(t, cfg)
	commitStateBlocks(t, stateDB, 1, 2)
	writeWALOnly(t, stateDB.WAL(), 3, evmBlock(3, 3))
	require.Equal(t, int64(2), stateDB.LiveStateDB().Version())
	require.NoError(t, stateDB.Close())
	writeBlockLedger(t, cfg, 3)
	writeReceipts(t, cfg, 3)
	writeAppHashes(t, cfg, 3)

	manager := startManager(t, cfg)

	require.Equal(t, int64(3), manager.StateDB().LiveStateDB().Version())
}

func TestRecoverLiveStateDBRollsBackToTheTarget(t *testing.T) {
	cfg := newHome(t, disableSS)
	writeBlockLedger(t, cfg, 3)
	writeState(t, cfg, 3)
	writeReceipts(t, cfg, 3)
	// The app hash vault behind the others sets the target.
	writeAppHashes(t, cfg, 2)

	manager := startManager(t, cfg)

	require.Equal(t, int64(2), manager.StateDB().LiveStateDB().Version())
}

// A rollback target below every WAL block empties the WAL. The stores still have to land on the
// snapshot at the target rather than keep a working copy the empty WAL can no longer account for.
func TestRecoverToATargetThatEmptiesTheWALLandsOnTheSnapshot(t *testing.T) {
	cfg := newHome(t, nil)
	stateDB := openStandaloneStateDB(t, cfg)
	snapshotLiveStateDBAt(t, stateDB, 1)
	stopSnapshotting(stateDB)
	commitStateBlocks(t, stateDB, 2, 5)
	require.NoError(t, stateDB.Close())
	replaceWALWithBlocks(t, cfg, 2, 5)
	writeBlockLedger(t, cfg, 5)
	writeReceipts(t, cfg, 5)
	// The app hash vault behind the others sets the target.
	writeAppHashes(t, cfg, 1)

	manager := startManager(t, cfg)

	require.Equal(t, int64(1), manager.StateDB().LiveStateDB().Version())
	require.Equal(t, int64(1), historicalStateDB(manager).GetLatestVersion())
	requireWALTail(t, manager, 0)
	commitBlock(t, manager, 2, evmBlock(2, 2))
}

// A WAL that starts above the first block the replay to the target needs is a nonsensical history, and startup
// refuses it.
func TestRecoverRefusesATargetTheWALCannotSpan(t *testing.T) {
	cfg := newHome(t, disableSS)
	stateDB := openStandaloneStateDB(t, cfg)
	snapshotLiveStateDBAt(t, stateDB, 1)
	stopSnapshotting(stateDB)
	commitStateBlocks(t, stateDB, 2, 5)
	require.NoError(t, stateDB.Close())
	replaceWALWithBlocks(t, cfg, 3, 5)
	writeBlockLedger(t, cfg, 5)
	writeReceipts(t, cfg, 5)
	// The app hash vault behind the others sets the target.
	writeAppHashes(t, cfg, 2)

	_, err := NewGigaStorageManager(t.Context(), cfg)
	require.ErrorContains(t, err, "replay must start at block 2")
}

// An empty WAL is no evidence about where state belongs, so a plain open leaves both stores holding the
// blocks they committed above their newest snapshot. Dropping them down to that snapshot is what a
// rollback does, from a target; doing it here would take the live state DB down on its own and leave the two
// stores at different heights, with nothing left to reconcile them.
//
// Committing afterwards is the check that they are still aligned, not merely reporting the same height.
func TestOpenWithAnEmptyWALLeavesBothStoresAlone(t *testing.T) {
	cfg := newHome(t, nil)
	stateDB := openStandaloneStateDB(t, cfg)
	stopSnapshotting(stateDB)
	commitStateBlocks(t, stateDB, 1, 3)
	require.NoError(t, stateDB.Close())
	truncateWAL(t, cfg, 0)
	writeAppHashes(t, cfg, 3)

	manager := startManager(t, cfg)

	require.Equal(t, int64(3), manager.StateDB().LiveStateDB().Version())
	require.Equal(t, int64(3), historicalStateDB(manager).GetLatestVersion())
	commitBlock(t, manager, 4, evmBlock(4, 4))
}

// A live state DB held above the WAL head by a snapshot of its own is rewound to a snapshot boundary at or below
// the target and replayed back up to it, rather than left where it is. Only a snapshot can put the live state DB
// above the head, since a truncated WAL is otherwise what its load lands on; taking one above the height the WAL
// is truncated to is what reaches this.
//
// Committing afterwards is the check that the rewind left both the store and the WAL writable at the
// height it converged on, not merely reporting that height.
func TestRecoverLiveStateDBAboveTheWALHeadRewindsToASnapshotAndReplays(t *testing.T) {
	cfg := newHome(t, disableSS)
	stateDB := openStandaloneStateDB(t, cfg)
	commitStateBlocks(t, stateDB, 1, 2)
	snapshotLiveStateDBAt(t, stateDB, 3)
	require.NoError(t, stateDB.Close())
	writeBlockLedger(t, cfg, 3)
	writeReceipts(t, cfg, 3)
	// The app hash vault behind the others sets the target.
	writeAppHashes(t, cfg, 2)

	manager := startManager(t, cfg)

	require.Equal(t, int64(2), manager.StateDB().LiveStateDB().Version())
	requireWALTail(t, manager, 2)
	commitBlock(t, manager, 3, evmBlock(3, 3))
}

// The historical state DB's records above the target are of blocks that run again, possibly differently,
// so they go.
func TestRecoverHistoricalStateDBRollsBackToTheTarget(t *testing.T) {
	cfg := newHome(t, nil)
	writeBlockLedger(t, cfg, 3)
	writeState(t, cfg, 3)
	writeReceipts(t, cfg, 3)
	// The app hash vault behind the others sets the target.
	writeAppHashes(t, cfg, 2)

	manager := startManager(t, cfg)

	require.Equal(t, int64(2), historicalStateDB(manager).GetLatestVersion())
	_, ok := manager.StateDB().OpenViewAt(3)
	require.False(t, ok, "a height above the target must not be served")
	commitBlock(t, manager, 3, evmBlock(3, 3))
}

// A crash that loses the historical state DB's write queue leaves it behind the other stores, and its head
// then sets the target: every store rolls back to it, keeping the history it holds.
func TestRecoveryConvergesOnAHistoricalStateDBBehindTheOthers(t *testing.T) {
	cfg := newHome(t, nil)
	writeNode(t, cfg, 5)
	require.NoError(t, undo.DiscardStateAbove(cfg.SSConfig.EVMDBDirectory, cfg.SSConfig, 3))

	manager := startManager(t, cfg)

	require.Equal(t, int64(3), manager.StateDB().LiveStateDB().Version())
	require.Equal(t, int64(3), historicalStateDB(manager).GetLatestVersion())
	requireWALTail(t, manager, 3)
	historicalStateDB(manager).WaitForPendingWrites()
	require.Zero(t, historicalStateDB(manager).GetEarliestVersion(), "the history it held must survive")
	for height := int64(0); height <= 3; height++ {
		view, ok := manager.StateDB().OpenViewAt(height)
		require.True(t, ok, "height %d", height)
		for addr := byte(1); addr <= 3; addr++ {
			want := uint64(0)
			if int64(addr) <= height {
				want = uint64(addr)
			}
			require.Equal(t, want, view.GetNonce(gigatypes.Address{addr}), "account %d at %d", addr, height)
		}
		view.Close()
	}
}

// Opening at a target rewinds the live state DB, the historical state DB and the WAL that feeds them, so the
// write head lands on the target. Committing the block after the target is what proves the WAL was truncated
// rather than only the stores rewound: a WAL still holding that block refuses to write it a second time.
func TestOpenAtATargetRewindsEveryStoreAndTheWAL(t *testing.T) {
	cfg := newHome(t, nil)
	writeBlockLedger(t, cfg, 5)
	writeState(t, cfg, 5)
	writeReceipts(t, cfg, 5)
	// The app hash vault behind the others sets the target.
	writeAppHashes(t, cfg, 2)

	manager := startManager(t, cfg)

	require.Equal(t, int64(2), manager.StateDB().LiveStateDB().Version())
	require.Equal(t, int64(2), historicalStateDB(manager).GetLatestVersion())
	requireWALTail(t, manager, 2)

	commitBlock(t, manager, 3, evmBlock(3, 3))
	require.Equal(t, int64(3), manager.StateDB().LiveStateDB().Version())
}

// Aligning on a target leaves a disabled historical state DB's directory alone, rather than opening one there.
func TestAlignmentLeavesADisabledHistoricalStateDBAlone(t *testing.T) {
	cfg := newHome(t, disableSS)
	require.NoError(t, os.MkdirAll(cfg.SSConfig.EVMDBDirectory, 0o750))
	writeBlockLedger(t, cfg, 3)
	writeState(t, cfg, 3)
	writeReceipts(t, cfg, 3)
	// The app hash vault behind the others sets the target.
	writeAppHashes(t, cfg, 2)

	manager := startManager(t, cfg)

	require.Equal(t, int64(2), manager.StateDB().LiveStateDB().Version())
	entries, err := os.ReadDir(cfg.SSConfig.EVMDBDirectory)
	require.NoError(t, err)
	require.Empty(t, entries, "alignment must not write to the directory of a store this node does not run")
}

// A crash while the historical state DB was being created leaves its directory holding no database.
// Startup removes it and opens the historical state DB as a new one.
func TestStartupRemovesAHalfCreatedHistoricalStateDB(t *testing.T) {
	cfg := newHome(t, nil)
	require.NoError(t, os.MkdirAll(cfg.SSConfig.EVMDBDirectory, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(cfg.SSConfig.EVMDBDirectory, "LOCK"), nil, 0o600))

	manager := startManager(t, cfg)

	require.NotNil(t, historicalStateDB(manager))
	commitBlocks(t, manager, 1)
}

func TestOpenDBWithoutRecoveryOnAFreshHome(t *testing.T) {
	manager, _ := openManager(t, nil)

	require.Zero(t, manager.StateDB().LiveStateDB().Version())
	require.Zero(t, historicalStateDB(manager).GetLatestVersion())
	require.Zero(t, manager.ReceiptDB().LatestVersion())
}

func TestOpenArchiveRefusesARollbackWindowShorterThanItsLag(t *testing.T) {
	cfg := &config.GigaStorageConfig{
		// Nothing listens here, so an archive that passes the check fails to connect instead.
		ArchiveConfig: &clickhouse.Config{Addr: "127.0.0.1:1", Database: "archive"},
		PruningConfig: &config.StorageGarbageCollectorConfig{RollbackWindow: chstore.MaxLag - 1},
	}
	_, err := openArchive(t.Context(), cfg, 1)
	require.ErrorContains(t, err, "rollback window")

	cfg.PruningConfig.RollbackWindow = chstore.MaxLag
	_, err = openArchive(t.Context(), cfg, 1)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "rollback window")
}

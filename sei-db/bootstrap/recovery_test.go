package bootstrap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/undo"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/gigareceipt"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
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
		reportTestBlockHash(t, manager, uint64(block))
		commitBlock(t, manager, int64(block), evmBlock(block, block))
	}
	awaitAppHashes(t, manager, uint64(through))
}

// commitBlock commits changes as block height and submits an empty receipt list for the block.
func commitBlock(t *testing.T, manager *GigaStorageManager, height int64, changes *gigatypes.StateChangeSet) {
	t.Helper()
	require.NoError(t, manager.StateDB().CommitStateChanges(height, changes))
	blockHeight := uint64(height) //nolint:gosec // small test heights
	require.NoError(t, manager.ReceiptHasher().Submit(t.Context(), blockHeight, nil))
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
		{name: "a fresh node has no height to align on"},
		{name: "the lowest head wins",
			heads: storeHeads{stateWAL: 5, receiptStore: 6, appHashVault: 9}, want: 5},
		{name: "receipts can be the lowest",
			heads: storeHeads{stateWAL: 6, receiptStore: 4, appHashVault: 9}, want: 4},
		{name: "the historical state DB can be the lowest",
			heads: storeHeads{stateWAL: 7, historicalStateDB: utils.Some[uint64](3), receiptStore: 6,
				appHashVault: 9}, want: 3},
		{name: "a historical state DB at block 0 is the lowest",
			heads: storeHeads{stateWAL: 7, historicalStateDB: utils.Some[uint64](0), receiptStore: 6,
				appHashVault: 9}, want: 0},
		// A historical state DB newly enabled, or disabled, holds nothing to disagree with. Folding its 0 into
		// the minimum would skip recovery for the stores that do have history.
		{name: "an empty historical state DB does not collapse the target",
			heads: storeHeads{stateWAL: 5, receiptStore: 6, appHashVault: 9}, want: 5},
		// The regression: receipts newly enabled, or a receipt directory recreated after corruption,
		// leave a head of 0 alongside real state history. Folding that 0 into the minimum collapses the
		// target and skips recovery for the stores that do have history.
		{name: "an empty receipt store does not collapse the target",
			heads: storeHeads{stateWAL: 5, appHashVault: 9}, want: 5},
		{name: "an empty state WAL yields no target", heads: storeHeads{receiptStore: 7}},
		{name: "the app hash vault can be the lowest",
			heads: storeHeads{stateWAL: 5, appHashVault: 4}, want: 4},
		// An empty vault's head is the block before the giga activation height, which holds the stores there.
		{name: "an empty app hash vault holds the target below a later giga activation",
			heads: storeHeads{stateWAL: 7, appHashVault: 3}, want: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, recoveryTarget(tc.heads))
		})
	}
}

func TestRequireAppHashesForStateBlocks(t *testing.T) {
	for _, tc := range []struct {
		name  string
		heads storeHeads
		// Whether requireAppHashesForStateBlocks() errors.
		wantErr bool
	}{
		{name: "a fresh node passes"},
		{name: "a vault holding app hashes passes", heads: storeHeads{stateWAL: 5, appHashVault: 5}},
		// An empty vault's head is the block before the giga activation height, which holds the stores there.
		{name: "an empty vault below a later giga activation passes",
			heads: storeHeads{stateWAL: 5, appHashVault: 3}},
		{name: "an empty vault at giga activation 1 refuses a state WAL holding blocks",
			heads: storeHeads{stateWAL: 5}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := requireAppHashesForStateBlocks(tc.heads)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
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
			heads: storeHeads{stateWAL: 5, historicalStateDB: utils.Some[uint64](5), receiptStore: 5,
				appHashVault: 5}},
		{name: "stores behind the block store pass", blockStoreHead: 9,
			heads: storeHeads{stateWAL: 5, receiptStore: 4, appHashVault: 3}},
		// A home bootstrapped from copied state, or restored from a snapshot, has no blocks yet.
		{name: "an empty block store passes stores holding blocks",
			heads: storeHeads{stateWAL: 5, historicalStateDB: utils.Some[uint64](5), receiptStore: 5,
				appHashVault: 5}},
		{name: "a state WAL ahead is refused", blockStoreHead: 3,
			heads: storeHeads{stateWAL: 4}, wantErr: "the state WAL holds block 4"},
		{name: "a historical state DB ahead is refused", blockStoreHead: 3,
			heads:   storeHeads{historicalStateDB: utils.Some[uint64](4)},
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
	walHead, err := stateWALHead(cfg)
	require.NoError(t, err)
	require.Equal(t, uint64(5), walHead, "a refused start must not have moved the state WAL")
	receiptHead, err := gigareceipt.GetLatestBlock(cfg.ReceiptDBConfig)
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

	require.Equal(t, int64(2), manager.StateDB().SC().Version())
	require.Equal(t, int64(2), manager.ReceiptDB().LatestVersion())
	commitBlock(t, manager, 3, evmBlock(3, 3))
}

// An empty state WAL is no height to align on, and rolling back to 0 would drop every receipt the node holds. The
// guard has to cover receipts and not just state: gigareceipt.PruneAfter() rewinds the head, range-deletes the
// whole tag index and drops every body, none of which a replay can put back.
func TestAnEmptyWALLeavesReceiptsAlone(t *testing.T) {
	cfg := newHome(t, nil)
	writeBlockLedger(t, cfg, 5)
	writeState(t, cfg, 3)
	truncateWAL(t, cfg, 0)
	writeReceipts(t, cfg, 5)
	writeAppHashes(t, cfg, 3)

	manager := startManager(t, cfg)

	require.Equal(t, int64(5), manager.ReceiptDB().LatestVersion(),
		"a zero target must leave the receipt store where it was found")
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
	require.Equal(t, int64(3), manager.StateDB().SC().Version())
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
	require.Equal(t, int64(2), stateDB.SC().Version())
	require.NoError(t, stateDB.Close())
	writeBlockLedger(t, cfg, 3)
	writeReceipts(t, cfg, 3)
	writeAppHashes(t, cfg, 3)

	manager := startManager(t, cfg)

	require.Equal(t, int64(3), manager.StateDB().SC().Version())
}

func TestRecoverLiveStateDBRollsBackToTheTarget(t *testing.T) {
	cfg := newHome(t, disableSS)
	writeBlockLedger(t, cfg, 3)
	writeState(t, cfg, 3)
	writeReceipts(t, cfg, 3)
	// The app hash vault behind the others sets the target.
	writeAppHashes(t, cfg, 2)

	manager := startManager(t, cfg)

	require.Equal(t, int64(2), manager.StateDB().SC().Version())
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

	require.Equal(t, int64(1), manager.StateDB().SC().Version())
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

	require.Equal(t, int64(3), manager.StateDB().SC().Version())
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

	require.Equal(t, int64(2), manager.StateDB().SC().Version())
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

	require.Equal(t, int64(3), manager.StateDB().SC().Version())
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

	require.Equal(t, int64(2), manager.StateDB().SC().Version())
	require.Equal(t, int64(2), historicalStateDB(manager).GetLatestVersion())
	requireWALTail(t, manager, 2)

	commitBlock(t, manager, 3, evmBlock(3, 3))
	require.Equal(t, int64(3), manager.StateDB().SC().Version())
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

	require.Equal(t, int64(2), manager.StateDB().SC().Version())
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

	require.Zero(t, manager.StateDB().SC().Version())
	require.Zero(t, historicalStateDB(manager).GetLatestVersion())
	require.Zero(t, manager.ReceiptDB().LatestVersion())
}

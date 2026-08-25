package receipt_test

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/controller"

	"github.com/ethereum/go-ethereum/common"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/stretchr/testify/require"
)

// setupLittIdxForGC opens a littidx store configured the way a collector-managed one actually is,
// so the only thing that moves the retention floor is the collector call under test. Note that this
// is the real mechanism rather than a test-only dodge: zeroing PruneIntervalSeconds would silence
// the local pruner just as well, and would not exercise the flag the collector depends on.
func setupLittIdxForGC(t *testing.T, keepRecent int) (receipt.ReceiptStore, controller.PrunableStore, sdk.Context) {
	t.Helper()
	storeKey := storetypes.NewKVStoreKey("evm")
	tkey := storetypes.NewTransientStoreKey("evm_transient")
	ctx := testutil.DefaultContext(storeKey, tkey).WithBlockHeight(1)

	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "littidx"
	cfg.DBDirectory = t.TempDir()
	cfg.KeepRecent = keepRecent
	cfg.ExternalPruning = true

	store, err := receipt.NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	prunable, ok := store.(controller.PrunableStore)
	require.True(t, ok, "a littidx receipt store must satisfy gc.PrunableStore")
	return store, prunable, ctx
}

// ExternalPruning is off unless asked for: a store built without a collector must keep pruning
// itself, since standing down with nothing to replace it grows the tag index without bound.
func TestReceiptGCExternalPruningDefaultsOff(t *testing.T) {
	storeKey := storetypes.NewKVStoreKey("evm")
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "littidx"
	cfg.DBDirectory = t.TempDir()
	require.False(t, cfg.ExternalPruning, "the default must leave retention with the store")

	store, err := receipt.NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	prunable, ok := store.(controller.PrunableStore)
	require.True(t, ok)
	require.False(t, prunable.ExternalPruning())

	_, managed, _ := setupLittIdxForGC(t, 0)
	require.True(t, managed.ExternalPruning())
}

// The pebble backend is not a gc.PrunableStore, so the collector would never prune it and honoring
// ExternalPruning would leave it with no pruner at all. Refused at startup rather than discovered
// later from a full disk.
func TestReceiptExternalPruningRejectedOnPebbleBackend(t *testing.T) {
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.DBDirectory = t.TempDir()
	cfg.ExternalPruning = true

	_, err := receipt.NewReceiptStore(cfg, storetypes.NewKVStoreKey("evm"))
	require.ErrorContains(t, err, "does not support external pruning")
}

// The standalone shape, and the one a collector-shaped change is most likely to break: with
// ExternalPruning unset there is no collector, and the store's own pruner has to advance the
// retention floor or the tag index grows without bound. Nodes running rs-backend = "littidx" with
// KeepRecent from min-retain-blocks depend on exactly this.
//
// Pays a real wait because the pruner is on a jittered timer and there is no way to observe it
// otherwise; TestRunsLocalPruner covers the decision itself without waiting.
func TestReceiptLocalPrunerAdvancesFloorWithoutCollector(t *testing.T) {
	storeKey := storetypes.NewKVStoreKey("evm")
	tkey := storetypes.NewTransientStoreKey("evm_transient")
	ctx := testutil.DefaultContext(storeKey, tkey).WithBlockHeight(1)

	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "littidx"
	cfg.DBDirectory = t.TempDir()
	cfg.KeepRecent = 2
	cfg.PruneIntervalSeconds = 1 // the shortest cadence there is; the pruner jitters to 1-2s

	store, err := receipt.NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	addr := common.HexToAddress("0xabcd")
	topic := common.HexToHash("0x1111")
	for block := uint64(1); block <= 5; block++ {
		writeLitBlock(t, store, ctx, block, litReceipt(block, 0, addr, topic))
	}

	// KeepRecent 2 at head 5 keeps [4, 5], so the floor lands on 4.
	require.Eventually(t, func() bool {
		return store.EarliestVersion() == 4
	}, 6*time.Second, 25*time.Millisecond, "the local pruner must advance the floor with no collector present")
}

// KeepRecent belongs to the local pruner and must not reach the collector's answers: how deep the
// collector prunes is its own fleet-wide window. Real receipts are written first so the floor is a
// nonzero head - rollbackWindow: with an empty store GetRollbackFloor short-circuits on
// head <= rollbackWindow and returns 0 before KeepRecent could matter, so the empty case would pass
// even if the floor were computed from KeepRecent — exactly the bug this pins against (0 used to
// mean "keep everything" here and would once have suppressed pruning entirely).
func TestReceiptGCAnswersDoNotDependOnKeepRecent(t *testing.T) {
	addr := common.HexToAddress("0xabcd")
	topic := common.HexToHash("0x1111")
	for _, keepRecent := range []int{0, 100_000} {
		store, prunable, ctx := setupLittIdxForGC(t, keepRecent)
		for block := uint64(1); block <= 10; block++ {
			writeLitBlock(t, store, ctx, block, litReceipt(block, 0, addr, topic))
		}
		require.Equal(t, uint64(7), prunable.GetRollbackFloor(3),
			"keepRecent %d must not change the floor (head 10 - window 3)", keepRecent)
	}
}

// This store keeps no snapshots, so its half of the split contract is a no-op that must leave the
// retention floor where it is.
func TestReceiptGCPruneSnapshotsIsANoOp(t *testing.T) {
	store, prunable, ctx := setupLittIdxForGC(t, 0)
	addr := common.HexToAddress("0xabcd")
	topic := common.HexToHash("0x1111")
	for block := uint64(1); block <= 3; block++ {
		writeLitBlock(t, store, ctx, block, litReceipt(block, 0, addr, topic))
	}

	require.NoError(t, prunable.PruneSnapshots(3))
	require.Equal(t, int64(0), store.EarliestVersion(), "a snapshot prune must not move the floor")

	kept, err := store.GetReceiptFromStore(ctx, litTxHash(1, 0))
	require.NoError(t, err)
	require.Equal(t, uint64(1), kept.BlockNumber)
}

// The head is 0 until receipts land, so a store that is still filling reports a floor of 0 and the
// fleet holds its history where it is rather than pruning against a head this store does not have.
func TestReceiptGCLatestBlock(t *testing.T) {
	store, prunable, ctx := setupLittIdxForGC(t, 0)

	latest, err := prunable.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(0), latest, "a store that has ingested nothing has no head")

	addr := common.HexToAddress("0xabcd")
	topic := common.HexToHash("0x1111")
	writeLitBlock(t, store, ctx, 1, litReceipt(1, 0, addr, topic))
	writeLitBlock(t, store, ctx, 2, litReceipt(2, 0, addr, topic))
	writeLitBlock(t, store, ctx, 3, litReceipt(3, 0, addr, topic))

	latest, err = prunable.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(3), latest)
	require.Equal(t, int64(3), store.LatestVersion(), "the head reported to the collector must agree with the store's own version")
}

// A contiguous store resolves the window against its own head, and the prune that follows moves the
// retention floor to the height it is given — which is what makes the receipts below it stop being
// served. Reclaiming their bodies lags that, since litt also waits for the TTL, but it can no
// longer lead it (see TestGCFilterMakesLittReclamationFollowTheBlockFloor).
func TestReceiptGCRollbackFloorAndPruneHistory(t *testing.T) {
	store, prunable, ctx := setupLittIdxForGC(t, 0)
	addr := common.HexToAddress("0xabcd")
	topic := common.HexToHash("0x1111")

	// Holding nothing: the head is 0, so every window reports 0 — keep everything — which holds the
	// fleet's history where it is until this store fills.
	require.Equal(t, uint64(0), prunable.GetRollbackFloor(42))

	for block := uint64(1); block <= 3; block++ {
		writeLitBlock(t, store, ctx, block, litReceipt(block, 0, addr, topic))
	}
	require.Equal(t, uint64(1), prunable.GetRollbackFloor(2))
	// A window deeper than its own head is a rollback promise reaching past genesis, so nothing here
	// is eligible for pruning yet.
	require.Equal(t, uint64(0), prunable.GetRollbackFloor(1_000))

	require.NoError(t, prunable.PruneHistory(3))
	require.Equal(t, int64(3), store.EarliestVersion(), "PruneHistory must advance the retention floor")

	_, err := store.GetReceiptFromStore(ctx, litTxHash(1, 0))
	require.ErrorIs(t, err, receipt.ErrNotFound, "a receipt below the floor must not be served")
	kept, err := store.GetReceiptFromStore(ctx, litTxHash(3, 0))
	require.NoError(t, err)
	require.Equal(t, uint64(3), kept.BlockNumber)

	// The floor only advances: a later, lower cycle must not re-expose what was pruned.
	require.NoError(t, prunable.PruneHistory(1))
	require.Equal(t, int64(3), store.EarliestVersion())
}

// PruneHistory carries a minimum taken across every managed store, so it can arrive above this
// store's head whenever this one lags or has ingested nothing. Both cases are the store's own to
// survive: this store's own floor of 0 makes them unlikely, but that is a property of the caller.
func TestReceiptGCPruneHistoryAboveHead(t *testing.T) {
	addr := common.HexToAddress("0xabcd")
	topic := common.HexToHash("0x1111")

	t.Run("empty store keeps its floor at zero", func(t *testing.T) {
		store, prunable, _ := setupLittIdxForGC(t, 0)

		require.NoError(t, prunable.PruneHistory(1_000))
		require.Equal(t, int64(0), store.EarliestVersion(),
			"a floor above an empty store would have to be walked back once blocks arrive")
	})

	t.Run("lagging store is capped at its head", func(t *testing.T) {
		store, prunable, ctx := setupLittIdxForGC(t, 0)
		for block := uint64(1); block <= 3; block++ {
			writeLitBlock(t, store, ctx, block, litReceipt(block, 0, addr, topic))
		}

		require.NoError(t, prunable.PruneHistory(1_000))
		require.Equal(t, int64(3), store.EarliestVersion(), "the floor stops at the head, not the request")

		kept, err := store.GetReceiptFromStore(ctx, litTxHash(3, 0))
		require.NoError(t, err, "honoring the request literally would drop every block the store holds")
		require.Equal(t, uint64(3), kept.BlockNumber)
	})
}

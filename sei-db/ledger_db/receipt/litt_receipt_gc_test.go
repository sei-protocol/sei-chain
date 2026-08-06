package receipt_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-db/management/gc"
	"github.com/stretchr/testify/require"
)

// setupLittIdxForGC opens a littidx store with the given KeepRecent and no background pruner, so
// the only thing that moves the retention floor is the collector call under test.
func setupLittIdxForGC(t *testing.T, keepRecent int) (receipt.ReceiptStore, gc.PrunableStore, sdk.Context) {
	t.Helper()
	storeKey := storetypes.NewKVStoreKey("evm")
	tkey := storetypes.NewTransientStoreKey("evm_transient")
	ctx := testutil.DefaultContext(storeKey, tkey).WithBlockHeight(1)

	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "littidx"
	cfg.DBDirectory = t.TempDir()
	cfg.KeepRecent = keepRecent
	cfg.PruneIntervalSeconds = 0

	store, err := receipt.NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	prunable, ok := store.(gc.PrunableStore)
	require.True(t, ok, "a littidx receipt store must satisfy gc.PrunableStore")
	return store, prunable, ctx
}

// KeepRecent and GetRetentionWindow disagree about 0, so the translation is the behavior worth
// pinning: KeepRecent 0 means "keep everything" and is the default, while a literal 0 answer
// means "keep only the shared rollback window". Returning the field verbatim would prune a store
// configured to retain forever back to ~1_000 blocks.
func TestReceiptGCRetentionWindowMapsKeepRecent(t *testing.T) {
	for _, tc := range []struct {
		name       string
		keepRecent int
		want       int64
	}{
		{name: "keep everything", keepRecent: 0, want: gc.InfiniteRetentionWindow},
		{name: "bounded retention", keepRecent: 100_000, want: 100_000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, prunable, _ := setupLittIdxForGC(t, tc.keepRecent)
			require.Equal(t, tc.want, prunable.GetRetentionWindow())
		})
	}
}

// The head is 0 until receipts land, which keeps a store that is still filling out of the
// collector's head minimum instead of dragging every store's cut line down to it.
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
	require.Equal(t, int64(3), store.LatestVersion(), "the collector's head must agree with the store's own version")
}

// A contiguous store answers the cut line it was given whatever it holds, and the prune that
// follows moves the retention floor to it — which is what makes the receipts below it stop being
// served, even though litt reclaims their bodies later on its own TTL schedule.
func TestReceiptGCPruningBoundaryAndPruneBelow(t *testing.T) {
	store, prunable, ctx := setupLittIdxForGC(t, 0)
	addr := common.HexToAddress("0xabcd")
	topic := common.HexToHash("0x1111")

	// Holding nothing: the boundary is still cutLine, since CannotServeRollback here would stall
	// every other store rather than protect anything.
	require.Equal(t, uint64(42), prunable.GetPruningBoundary(42))

	for block := uint64(1); block <= 3; block++ {
		writeLitBlock(t, store, ctx, block, litReceipt(block, 0, addr, topic))
	}
	require.Equal(t, uint64(2), prunable.GetPruningBoundary(2))
	// Above the head, which happens whenever another store's data puts the head above this one's.
	require.Equal(t, uint64(1_000), prunable.GetPruningBoundary(1_000))

	require.NoError(t, prunable.PruneBelow(3))
	require.Equal(t, int64(3), store.EarliestVersion(), "PruneBelow must advance the retention floor")

	_, err := store.GetReceiptFromStore(ctx, litTxHash(1, 0))
	require.ErrorIs(t, err, receipt.ErrNotFound, "a receipt below the floor must not be served")
	kept, err := store.GetReceiptFromStore(ctx, litTxHash(3, 0))
	require.NoError(t, err)
	require.Equal(t, uint64(3), kept.BlockNumber)

	// The floor only advances: a later, lower cycle must not re-expose what was pruned.
	require.NoError(t, prunable.PruneBelow(1))
	require.Equal(t, int64(3), store.EarliestVersion())
}

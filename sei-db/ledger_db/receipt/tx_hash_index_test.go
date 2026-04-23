package receipt

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/filters"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/stretchr/testify/require"
)

func TestPebbleTxHashIndexBasicOperations(t *testing.T) {
	idx, err := NewPebbleTxHashIndex(TxHashIndexDir(t.TempDir()))
	require.NoError(t, err)
	defer idx.Close()

	ctx := context.Background()

	txHash1 := common.HexToHash("0x1111")
	txHash2 := common.HexToHash("0x2222")
	txHash3 := common.HexToHash("0x3333")

	require.NoError(t, idx.IndexBlock(ctx, 100, []common.Hash{txHash1, txHash2}))
	require.NoError(t, idx.IndexBlock(ctx, 200, []common.Hash{txHash3}))

	blockNum, ok, err := idx.GetBlockNumber(ctx, txHash1)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(100), blockNum)

	blockNum, ok, err = idx.GetBlockNumber(ctx, txHash2)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(100), blockNum)

	blockNum, ok, err = idx.GetBlockNumber(ctx, txHash3)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(200), blockNum)

	_, ok, err = idx.GetBlockNumber(ctx, common.HexToHash("0xdead"))
	require.NoError(t, err)
	require.False(t, ok)
}

func TestPebbleTxHashIndexPruneBefore(t *testing.T) {
	idx, err := NewPebbleTxHashIndex(TxHashIndexDir(t.TempDir()))
	require.NoError(t, err)
	defer idx.Close()

	ctx := context.Background()

	require.NoError(t, idx.IndexBlock(ctx, 10, []common.Hash{common.HexToHash("0xaa")}))
	require.NoError(t, idx.IndexBlock(ctx, 20, []common.Hash{common.HexToHash("0xbb")}))
	require.NoError(t, idx.IndexBlock(ctx, 30, []common.Hash{common.HexToHash("0xcc")}))

	require.NoError(t, idx.PruneBefore(ctx, 25))

	_, ok, _ := idx.GetBlockNumber(ctx, common.HexToHash("0xaa"))
	require.False(t, ok, "block 10 should be pruned")

	_, ok, _ = idx.GetBlockNumber(ctx, common.HexToHash("0xbb"))
	require.False(t, ok, "block 20 should be pruned")

	blockNum, ok, err := idx.GetBlockNumber(ctx, common.HexToHash("0xcc"))
	require.NoError(t, err)
	require.True(t, ok, "block 30 should survive pruning")
	require.Equal(t, uint64(30), blockNum)
}

func TestPebbleTxHashIndexEmptyOperations(t *testing.T) {
	idx, err := NewPebbleTxHashIndex(TxHashIndexDir(t.TempDir()))
	require.NoError(t, err)
	defer idx.Close()

	ctx := context.Background()

	require.NoError(t, idx.IndexBlock(ctx, 1, nil))
	require.NoError(t, idx.IndexBlock(ctx, 2, []common.Hash{}))
	require.NoError(t, idx.PruneBefore(ctx, 100))
}

func TestPebbleTxHashIndexOverwrite(t *testing.T) {
	idx, err := NewPebbleTxHashIndex(TxHashIndexDir(t.TempDir()))
	require.NoError(t, err)
	defer idx.Close()

	ctx := context.Background()
	txHash := common.HexToHash("0xabcd")

	require.NoError(t, idx.IndexBlock(ctx, 100, []common.Hash{txHash}))

	blockNum, ok, _ := idx.GetBlockNumber(ctx, txHash)
	require.True(t, ok)
	require.Equal(t, uint64(100), blockNum)

	require.NoError(t, idx.IndexBlock(ctx, 200, []common.Hash{txHash}))

	blockNum, ok, _ = idx.GetBlockNumber(ctx, txHash)
	require.True(t, ok)
	require.Equal(t, uint64(200), blockNum)
}

func TestPebbleTxHashIndexPersistence(t *testing.T) {
	dir := TxHashIndexDir(t.TempDir())
	ctx := context.Background()
	txHash := common.HexToHash("0xdead")

	idx, err := NewPebbleTxHashIndex(dir)
	require.NoError(t, err)
	require.NoError(t, idx.IndexBlock(ctx, 42, []common.Hash{txHash}))
	require.NoError(t, idx.Close())

	idx2, err := NewPebbleTxHashIndex(dir)
	require.NoError(t, err)
	defer idx2.Close()

	blockNum, ok, err := idx2.GetBlockNumber(ctx, txHash)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(42), blockNum)
}

func TestParquetStoreIndexedLookup(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := newParquetTestConfig(t)

	store, err := NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	addr := common.HexToAddress("0x1")
	txHash := common.HexToHash("0xabc123")
	receipt := makeTestReceipt(txHash, 1, 0, addr, nil)

	require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(1), []ReceiptRecord{
		{TxHash: txHash, Receipt: receipt},
	}))

	got, err := store.GetReceiptFromStore(ctx, txHash)
	require.NoError(t, err)
	require.Equal(t, txHash.Hex(), got.TxHashHex)
}

func TestParquetStoreIndexedLookupFallback(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := newParquetTestConfig(t)

	store, err := NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	_, err = store.GetReceiptFromStore(ctx, common.HexToHash("0xnonexistent"))
	require.ErrorIs(t, err, ErrNotFound)
}

func TestParquetStoreDisabledIndex(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := newParquetTestConfig(t)
	cfg.TxIndexBackend = ""

	store, err := NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	addr := common.HexToAddress("0x1")
	txHash := common.HexToHash("0xdef456")
	receipt := makeTestReceipt(txHash, 1, 0, addr, nil)

	require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(1), []ReceiptRecord{
		{TxHash: txHash, Receipt: receipt},
	}))

	got, err := store.GetReceiptFromStore(ctx, txHash)
	require.NoError(t, err)
	require.Equal(t, txHash.Hex(), got.TxHashHex)
}

// When the pebble tx hash index is disabled, a cache miss must not trigger a
// full parquet scan. Unknown tx hashes should fail fast with
// ErrTxIndexDisabled.
func TestParquetStoreDisabledIndexCacheMissReturnsError(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := newParquetTestConfig(t)
	cfg.TxIndexBackend = ""

	store, err := NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	unknown := common.HexToHash("0xabcdef")
	_, err = store.GetReceipt(ctx, unknown)
	require.ErrorIs(t, err, ErrTxIndexDisabled)

	_, err = store.GetReceiptFromStore(ctx, unknown)
	require.ErrorIs(t, err, ErrTxIndexDisabled)
}

// Once a previously cached receipt is evicted (via cache rotation), queries
// must fail with ErrTxIndexDisabled instead of falling back to the parquet
// files. This is the core regression guard: we never want to quietly do a
// full-file scan when the index is off.
func TestParquetStoreDisabledIndexEvictedCacheReturnsError(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := newParquetTestConfig(t)
	cfg.TxIndexBackend = ""

	store, err := NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	addr := common.HexToAddress("0x1")
	txHash := common.HexToHash("0xbeef01")
	receipt := makeTestReceipt(txHash, 1, 0, addr, nil)

	require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(1), []ReceiptRecord{
		{TxHash: txHash, Receipt: receipt},
	}))

	// Sanity check: the receipt is served from cache while it is still hot.
	got, err := store.GetReceiptFromStore(ctx, txHash)
	require.NoError(t, err)
	require.Equal(t, txHash.Hex(), got.TxHashHex)

	// Evict by rotating past the chunk that holds this receipt. Two rotations
	// move the chunk into the prune slot and clear it on the next rotate.
	cached := store.(*cachedReceiptStore)
	cached.cache.Rotate()
	cached.cache.Rotate()

	_, err = store.GetReceipt(ctx, txHash)
	require.ErrorIs(t, err, ErrTxIndexDisabled,
		"cache miss with disabled index must not fall back to parquet scan")

	_, err = store.GetReceiptFromStore(ctx, txHash)
	require.ErrorIs(t, err, ErrTxIndexDisabled)
}

// Directly exercises the parquet backend (bypassing the cache) to pin down
// the exact place the error is raised. This guards against accidentally
// reintroducing a fallback scan in GetReceipt / GetReceiptFromStore.
func TestParquetReceiptStoreBackendReturnsErrorWhenIndexDisabled(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := newParquetTestConfig(t)
	cfg.TxIndexBackend = ""

	store, err := NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	backend := store.(*cachedReceiptStore).backend.(*parquetReceiptStore)
	require.Nil(t, backend.txHashIndex, "test precondition: tx hash index should be nil when disabled")

	_, err = backend.GetReceipt(ctx, common.HexToHash("0x1"))
	require.ErrorIs(t, err, ErrTxIndexDisabled)
	// The error must also be reported as ErrNotFound so RPC callers that treat
	// "not found" as a null response keep their standard behavior.
	require.ErrorIs(t, err, ErrNotFound)

	_, err = backend.GetReceiptFromStore(ctx, common.HexToHash("0x1"))
	require.ErrorIs(t, err, ErrTxIndexDisabled)
	require.ErrorIs(t, err, ErrNotFound)
}

// FilterLogs uses block-range queries that don't touch the tx hash index.
// Disabling the index must not break log filtering.
func TestParquetStoreDisabledIndexFilterLogsStillWorks(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := newParquetTestConfig(t)
	cfg.TxIndexBackend = ""

	store, err := NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	addr := common.HexToAddress("0x55")
	topic := common.HexToHash("0x77")
	txHash := common.HexToHash("0x99")
	receipt := makeTestReceipt(txHash, 5, 0, addr, []common.Hash{topic})

	require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(5), []ReceiptRecord{
		{TxHash: txHash, Receipt: receipt},
	}))

	logs, err := store.FilterLogs(ctx, 5, 5, filters.FilterCriteria{
		Addresses: []common.Address{addr},
		Topics:    [][]common.Hash{{topic}},
	})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, uint64(5), logs[0].BlockNumber)
}

// When the index is enabled (default), the indexed path is used and the error
// is not raised. This guards against over-eagerly returning the disabled-index
// error in the wrong code path.
func TestParquetStoreIndexedLookupDoesNotReturnDisabledError(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := newParquetTestConfig(t) // default TxIndexBackend = pebbledb

	store, err := NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	addr := common.HexToAddress("0x1")
	txHash := common.HexToHash("0xcafe01")
	receipt := makeTestReceipt(txHash, 1, 0, addr, nil)
	require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(1), []ReceiptRecord{
		{TxHash: txHash, Receipt: receipt},
	}))

	// Hit with indexed backend still works (served by cache or by index).
	got, err := store.GetReceiptFromStore(ctx, txHash)
	require.NoError(t, err)
	require.Equal(t, txHash.Hex(), got.TxHashHex)

	// Unknown hash with index enabled is ErrNotFound, not ErrTxIndexDisabled.
	_, err = store.GetReceiptFromStore(ctx, common.HexToHash("0xdead"))
	require.ErrorIs(t, err, ErrNotFound)
	require.NotErrorIs(t, err, ErrTxIndexDisabled)
}

func TestParquetStoreWALReplayPopulatesIndex(t *testing.T) {
	ctx, storeKey := newTestContext()
	dir := t.TempDir()
	cfg := newParquetTestConfigWithDir(dir)

	store, err := NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)

	pqStore := extractParquetStore(t, store)
	addr := common.HexToAddress("0x1")

	for block := uint64(1); block <= 3; block++ {
		txHash := blockTxHash(block)
		receipt := makeTestReceipt(txHash, block, 0, addr, nil)
		require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(int64(block)), []ReceiptRecord{
			{TxHash: txHash, Receipt: receipt},
		}))
	}

	simulateCrash(store, pqStore)

	store2, err := NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store2.Close() })

	for block := uint64(1); block <= 3; block++ {
		txHash := blockTxHash(block)
		got, err := store2.GetReceiptFromStore(ctx, txHash)
		require.NoError(t, err, "block %d should be recovered with index", block)
		require.Equal(t, txHash.Hex(), got.TxHashHex)
	}
}

func newParquetTestConfig(t *testing.T) dbconfig.ReceiptStoreConfig {
	t.Helper()
	return newParquetTestConfigWithDir(t.TempDir())
}

func newParquetTestConfigWithDir(dir string) dbconfig.ReceiptStoreConfig {
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "parquet"
	cfg.DBDirectory = dir
	return cfg
}

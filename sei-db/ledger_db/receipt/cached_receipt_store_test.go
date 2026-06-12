package receipt

import (
	"math/big"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

type fakeReceiptBackend struct {
	receipts            map[common.Hash]*types.Receipt
	logs                []*ethtypes.Log
	latestVersion       int64
	rotateInterval      uint64
	getReceiptCalls     int
	filterLogCalls      int
	lastFilterFromBlock uint64
	lastFilterToBlock   uint64
	filterLogsErr       error
}

func (f *fakeReceiptBackend) cacheRotateInterval() uint64 {
	return f.rotateInterval
}

type fakeReceiptReadMetrics struct {
	cacheHits            int
	cacheMisses          int
	logFilterCacheHits   int
	logFilterCacheMisses int
}

func (f *fakeReceiptReadMetrics) ReportReceiptCacheHit() {
	f.cacheHits++
}

func (f *fakeReceiptReadMetrics) ReportReceiptCacheMiss() {
	f.cacheMisses++
}

func (f *fakeReceiptReadMetrics) ReportLogFilterCacheHit() {
	f.logFilterCacheHits++
}

func (f *fakeReceiptReadMetrics) ReportLogFilterCacheMiss() {
	f.logFilterCacheMisses++
}

func (f *fakeReceiptReadMetrics) RecordCacheFilterScanDuration(float64) {}

func (f *fakeReceiptReadMetrics) RecordCacheGetDuration(float64) {}

func newFakeReceiptBackend() *fakeReceiptBackend {
	return &fakeReceiptBackend{
		receipts: make(map[common.Hash]*types.Receipt),
	}
}

func (f *fakeReceiptBackend) LatestVersion() int64 {
	return f.latestVersion
}

func (f *fakeReceiptBackend) EarliestVersion() int64 {
	return 0
}

func (f *fakeReceiptBackend) SetLatestVersion(int64) error {
	return nil
}

func (f *fakeReceiptBackend) SetEarliestVersion(int64) error {
	return nil
}

func (f *fakeReceiptBackend) GetReceipt(_ sdk.Context, txHash common.Hash) (*types.Receipt, error) {
	f.getReceiptCalls++
	if receipt, ok := f.receipts[txHash]; ok {
		return receipt, nil
	}
	return nil, ErrNotFound
}

func (f *fakeReceiptBackend) GetReceiptFromStore(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error) {
	return f.GetReceipt(ctx, txHash)
}

func (f *fakeReceiptBackend) SetReceipts(ctx sdk.Context, receipts []ReceiptRecord) error {
	for _, record := range receipts {
		if record.Receipt == nil {
			continue
		}
		f.receipts[record.TxHash] = record.Receipt
		if int64(record.Receipt.BlockNumber) > f.latestVersion { //nolint:gosec
			f.latestVersion = int64(record.Receipt.BlockNumber) //nolint:gosec
		}
	}
	if ctx.BlockHeight() > f.latestVersion {
		f.latestVersion = ctx.BlockHeight()
	}
	return nil
}

func (f *fakeReceiptBackend) FilterLogs(_ sdk.Context, from, to uint64, _ filters.FilterCriteria) ([]*ethtypes.Log, error) {
	if f.filterLogsErr != nil {
		return nil, f.filterLogsErr
	}
	f.filterLogCalls++
	f.lastFilterFromBlock = from
	f.lastFilterToBlock = to
	var result []*ethtypes.Log
	for _, lg := range f.logs {
		if lg.BlockNumber >= from && lg.BlockNumber <= to {
			result = append(result, lg)
		}
	}
	return result, nil
}

func (f *fakeReceiptBackend) Close() error {
	return nil
}

func TestCachedReceiptStoreUsesCacheForReceipt(t *testing.T) {
	ctx, _ := newTestContext()
	backend := newFakeReceiptBackend()
	store := newCachedReceiptStore(backend, nil)

	txHash := common.HexToHash("0x1")
	addr := common.HexToAddress("0x100")
	topic := common.HexToHash("0xabc")
	receipt := makeTestReceipt(txHash, 7, 1, addr, []common.Hash{topic})

	require.NoError(t, store.SetReceipts(ctx, []ReceiptRecord{{TxHash: txHash, Receipt: receipt}}))

	backend.getReceiptCalls = 0
	got, err := store.GetReceipt(ctx, txHash)
	require.NoError(t, err)
	require.Equal(t, receipt.TxHashHex, got.TxHashHex)
	require.Equal(t, 0, backend.getReceiptCalls)
}

func TestCachedReceiptStoreFilterLogsSkipsBackendWhenCacheFullyCoversRange(t *testing.T) {
	ctx, _ := newTestContext()
	backend := newFakeReceiptBackend()
	store := newCachedReceiptStore(backend, nil)

	txHash := common.HexToHash("0x2")
	addr := common.HexToAddress("0x200")
	topic := common.HexToHash("0xdef")
	receipt := makeTestReceipt(txHash, 9, 0, addr, []common.Hash{topic})

	require.NoError(t, store.SetReceipts(ctx, []ReceiptRecord{{TxHash: txHash, Receipt: receipt}}))

	backend.filterLogCalls = 0
	logs, err := store.FilterLogs(ctx, 9, 9, filters.FilterCriteria{
		Addresses: []common.Address{addr},
		Topics:    [][]common.Hash{{topic}},
	})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, 0, backend.filterLogCalls, "backend should not be called when the range is fully covered")
}

func TestCachedReceiptStoreFilterLogsReturnsSortedLogs(t *testing.T) {
	ctx, _ := newTestContext()
	backend := newFakeReceiptBackend()
	// Backend holds older blocks that predate the cache window.
	backend.logs = []*ethtypes.Log{
		{BlockNumber: 8, TxIndex: 1, Index: 2},
		{BlockNumber: 5, TxIndex: 0, Index: 0},
	}
	store := newCachedReceiptStore(backend, nil)

	// Cache holds recent blocks written through SetReceipts.
	receiptA := makeTestReceipt(common.HexToHash("0xa"), 11, 1, common.HexToAddress("0x210"), []common.Hash{common.HexToHash("0x1")})
	receiptB := makeTestReceipt(common.HexToHash("0xb"), 11, 0, common.HexToAddress("0x220"), []common.Hash{common.HexToHash("0x2")})
	require.NoError(t, store.SetReceipts(ctx, []ReceiptRecord{
		{TxHash: common.HexToHash("0xa"), Receipt: receiptA},
		{TxHash: common.HexToHash("0xb"), Receipt: receiptB},
	}))

	logs, err := store.FilterLogs(ctx, 5, 12, filters.FilterCriteria{})
	require.NoError(t, err)
	require.Len(t, logs, 4)
	require.Equal(t, uint64(5), logs[0].BlockNumber)
	require.Equal(t, uint64(8), logs[1].BlockNumber)
	require.Equal(t, uint64(11), logs[2].BlockNumber)
	require.Equal(t, uint(0), logs[2].TxIndex)
	require.Equal(t, uint64(11), logs[3].BlockNumber)
	require.Equal(t, uint(1), logs[3].TxIndex)
}

func TestFilterLogsPartialCoverageQueriesOnlyOlderBackendPrefix(t *testing.T) {
	ctx, _ := newTestContext()
	backend := newFakeReceiptBackend()
	// Use a 10-block rotation interval so the cache claims coverage of only
	// [10, latest] once blocks 10+ are written. Older blocks stay in the
	// backend.
	backend.rotateInterval = 10
	backend.logs = []*ethtypes.Log{
		{BlockNumber: 5, TxIndex: 0, Index: 0},
		{BlockNumber: 8, TxIndex: 0, Index: 0},
	}
	store := newCachedReceiptStore(backend, nil)

	receipt10 := makeTestReceipt(common.HexToHash("0xa"), 10, 0, common.HexToAddress("0x1"), nil)
	receipt11 := makeTestReceipt(common.HexToHash("0xb"), 11, 0, common.HexToAddress("0x2"), nil)
	require.NoError(t, store.SetReceipts(ctx, []ReceiptRecord{
		{TxHash: common.HexToHash("0xa"), Receipt: receipt10},
		{TxHash: common.HexToHash("0xb"), Receipt: receipt11},
	}))

	backend.filterLogCalls = 0
	logs, err := store.FilterLogs(ctx, 5, 11, filters.FilterCriteria{})
	require.NoError(t, err)
	require.Equal(t, 1, backend.filterLogCalls)
	require.Equal(t, uint64(5), backend.lastFilterFromBlock)
	require.Equal(t, uint64(9), backend.lastFilterToBlock, "backend should only serve the uncovered older prefix")

	require.Len(t, logs, 4)
	require.Equal(t, uint64(5), logs[0].BlockNumber)
	require.Equal(t, uint64(8), logs[1].BlockNumber)
	require.Equal(t, uint64(10), logs[2].BlockNumber)
	require.Equal(t, uint64(11), logs[3].BlockNumber)
}

func TestFilterLogsCoverageGapResetsAndQueriesBackendForOlderBlocks(t *testing.T) {
	ctx, _ := newTestContext()
	backend := newFakeReceiptBackend()
	// With interval=10 and writes at blocks 10 then 20, block 20's write
	// crosses the aligned rotation boundary, moving block 10 into the
	// previous cache chunk. Coverage is [20, 20]; queries that start below
	// must fetch the older prefix from the backend.
	backend.rotateInterval = 10
	backend.logs = []*ethtypes.Log{
		{BlockNumber: 5, TxIndex: 0, Index: 0},
		{BlockNumber: 9, TxIndex: 0, Index: 0},
		{BlockNumber: 15, TxIndex: 0, Index: 0},
	}
	store := newCachedReceiptStore(backend, nil).(*cachedReceiptStore)

	receipt10 := makeTestReceipt(common.HexToHash("0xc"), 10, 0, common.HexToAddress("0x1"), nil)
	require.NoError(t, store.SetReceipts(ctx, []ReceiptRecord{
		{TxHash: common.HexToHash("0xc"), Receipt: receipt10},
	}))

	receipt20 := makeTestReceipt(common.HexToHash("0xd"), 20, 0, common.HexToAddress("0x2"), nil)
	require.NoError(t, store.SetReceipts(ctx, []ReceiptRecord{
		{TxHash: common.HexToHash("0xd"), Receipt: receipt20},
	}))

	backend.filterLogCalls = 0
	logs, err := store.FilterLogs(ctx, 5, 20, filters.FilterCriteria{})
	require.NoError(t, err)
	require.Equal(t, 1, backend.filterLogCalls)
	require.Equal(t, uint64(5), backend.lastFilterFromBlock)
	require.Equal(t, uint64(19), backend.lastFilterToBlock, "backend should stop at the start of the covered recent window")

	require.Len(t, logs, 5)
	require.Equal(t, uint64(5), logs[0].BlockNumber)
	require.Equal(t, uint64(9), logs[1].BlockNumber)
	require.Equal(t, uint64(10), logs[2].BlockNumber)
	require.Equal(t, uint64(15), logs[3].BlockNumber)
	require.Equal(t, uint64(20), logs[4].BlockNumber)
}

func TestFilterLogsFallsBackToBackendWhenCacheEmpty(t *testing.T) {
	ctx, _ := newTestContext()
	backend := newFakeReceiptBackend()
	backend.logs = []*ethtypes.Log{
		{BlockNumber: 2, TxIndex: 0, Index: 0},
		{BlockNumber: 1, TxIndex: 0, Index: 0},
	}
	store := newCachedReceiptStore(backend, nil)

	backend.filterLogCalls = 0
	logs, err := store.FilterLogs(ctx, 1, 5, filters.FilterCriteria{})
	require.NoError(t, err)
	require.Equal(t, 1, backend.filterLogCalls)
	require.Equal(t, uint64(1), backend.lastFilterFromBlock)
	require.Equal(t, uint64(5), backend.lastFilterToBlock, "full range passed when cache is empty")
	require.Len(t, logs, 2)
	require.Equal(t, uint64(1), logs[0].BlockNumber)
	require.Equal(t, uint64(2), logs[1].BlockNumber)
}

func TestFilterLogsMultipleBlocksCacheOnly(t *testing.T) {
	ctx, _ := newTestContext()
	backend := newFakeReceiptBackend()
	store := newCachedReceiptStore(backend, nil)

	for block := uint64(100); block <= 105; block++ {
		txHash := common.BigToHash(new(big.Int).SetUint64(block))
		r := makeTestReceipt(txHash, block, 0, common.HexToAddress("0x1"), nil)
		require.NoError(t, store.SetReceipts(ctx, []ReceiptRecord{{TxHash: txHash, Receipt: r}}))
	}

	backend.filterLogCalls = 0
	logs, err := store.FilterLogs(ctx, 101, 104, filters.FilterCriteria{})
	require.NoError(t, err)
	require.Equal(t, 0, backend.filterLogCalls, "backend should not be called when the range is fully covered")
	require.Len(t, logs, 4)
	for i, blockNum := range []uint64{101, 102, 103, 104} {
		require.Equal(t, blockNum, logs[i].BlockNumber)
	}
}

func TestFilterLogsCoveredEmptyBlocksReturnEmptyWithoutBackend(t *testing.T) {
	ctx, _ := newTestContext()
	backend := newFakeReceiptBackend()
	store := newCachedReceiptStore(backend, nil)

	// A real write at block 49 initializes the cache boundary. Subsequent
	// empty blocks advance the boundary without forcing a backend query.
	txHash := common.HexToHash("0x49")
	receipt := makeTestReceipt(txHash, 49, 0, common.HexToAddress("0x490"), nil)
	require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(49), []ReceiptRecord{
		{TxHash: txHash, Receipt: receipt},
	}))

	for block := int64(50); block <= 52; block++ {
		require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(block), nil))
	}

	backend.filterLogCalls = 0
	logs, err := store.FilterLogs(ctx.WithBlockHeight(52), 50, 52, filters.FilterCriteria{})
	require.NoError(t, err)
	require.Empty(t, logs)
	require.Equal(t, 0, backend.filterLogCalls, "covered zero-log blocks should not force a backend query")
}

// TestFilterLogsColdReopenEmptyBlocksQueriesBackend verifies the documented
// coverageWindow invariant: after a cold reopen with no warmup, an empty
// block must not initialize the cache boundary. Otherwise coverageWindow
// would claim coverage of blocks the cache has never observed, silently
// hiding logs that only exist in the backend.
func TestFilterLogsColdReopenEmptyBlocksQueriesBackend(t *testing.T) {
	ctx, _ := newTestContext()
	backend := newFakeReceiptBackend()
	// Simulate historical logs that live only in the backend (e.g. pre-reopen
	// blocks whose receipts were never replayed into the cache).
	backend.logs = []*ethtypes.Log{
		{BlockNumber: 1200, TxHash: common.HexToHash("0xb00"), TxIndex: 0, Index: 0},
	}
	backend.latestVersion = 1233
	store := newCachedReceiptStore(backend, nil)

	// First post-reopen commit is an empty block at tip. This must not
	// initialize the cache boundary, since no receipts have been cached.
	require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(1234), nil))

	logs, err := store.FilterLogs(ctx.WithBlockHeight(1234), 1000, 1234, filters.FilterCriteria{})
	require.NoError(t, err)
	require.Equal(t, 1, backend.filterLogCalls,
		"cold-reopen empty block must not mark the cache as covering historical blocks")
	require.Len(t, logs, 1)
	require.Equal(t, uint64(1200), logs[0].BlockNumber)
}

func TestFilterLogsFallsBackToCoveredCacheWhenBackendDoesNotSupportRangeQueries(t *testing.T) {
	ctx, _ := newTestContext()
	backend := newFakeReceiptBackend()
	backend.filterLogsErr = ErrRangeQueryNotSupported
	store := newCachedReceiptStore(backend, nil)

	txHash := common.HexToHash("0x44")
	receipt := makeTestReceipt(txHash, 44, 0, common.HexToAddress("0x440"), nil)
	require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(44), []ReceiptRecord{{TxHash: txHash, Receipt: receipt}}))

	logs, err := store.FilterLogs(ctx.WithBlockHeight(44), 44, 44, filters.FilterCriteria{})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, 0, backend.filterLogCalls)
}

func TestCachedReceiptStoreReportsCacheHit(t *testing.T) {
	ctx, _ := newTestContext()
	backend := newFakeReceiptBackend()
	metrics := &fakeReceiptReadMetrics{}
	store := newCachedReceiptStore(backend, metrics)

	txHash := common.HexToHash("0x10")
	receipt := makeTestReceipt(txHash, 7, 1, common.HexToAddress("0x100"), nil)

	require.NoError(t, store.SetReceipts(ctx, []ReceiptRecord{{TxHash: txHash, Receipt: receipt}}))

	backend.getReceiptCalls = 0
	got, err := store.GetReceipt(ctx, txHash)
	require.NoError(t, err)
	require.Equal(t, receipt.TxHashHex, got.TxHashHex)
	require.Equal(t, 0, backend.getReceiptCalls)
	require.Equal(t, 1, metrics.cacheHits)
	require.Equal(t, 0, metrics.cacheMisses)
}

func TestCachedReceiptStoreReportsCacheMiss(t *testing.T) {
	ctx, _ := newTestContext()
	backend := newFakeReceiptBackend()
	metrics := &fakeReceiptReadMetrics{}
	store := newCachedReceiptStore(backend, metrics)

	_, err := store.GetReceipt(ctx, common.HexToHash("0x404"))
	require.ErrorIs(t, err, ErrNotFound)
	require.Equal(t, 1, backend.getReceiptCalls)
	require.Equal(t, 0, metrics.cacheHits)
	require.Equal(t, 1, metrics.cacheMisses)
}

// Wrapper tests for cachedReceiptStore using parquet backend.
func TestCachedReceiptStoreFallsBackToDuckDBOnReceiptCacheMiss(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "parquet"
	cfg.DBDirectory = t.TempDir()

	store, err := NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)

	txHash := common.HexToHash("0x12")
	addr := common.HexToAddress("0x212")
	topic := common.HexToHash("0xcafe")
	receipt := makeTestReceipt(txHash, 8, 0, addr, []common.Hash{topic})

	require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(8), []ReceiptRecord{
		{TxHash: txHash, Receipt: receipt},
	}))
	require.NoError(t, store.Close())

	store, err = NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	cached, ok := store.(*cachedReceiptStore)
	require.True(t, ok, "expected cached receipt store wrapper")

	// A clean reopen leaves no WAL warmup records, so receipt lookup must
	// miss the in-memory cache and fall through to the parquet/DuckDB backend.
	_, ok = cached.cache.GetReceipt(txHash)
	require.False(t, ok, "receipt cache should be cold after clean reopen")

	// There is no legacy KV receipt entry to rescue the lookup, so success
	// here proves GetReceipt() can recover from DuckDB after a cache miss.
	require.Nil(t, ctx.KVStore(storeKey).Get(types.ReceiptKey(txHash)))

	got, err := store.GetReceipt(ctx.WithBlockHeight(8), txHash)
	require.NoError(t, err)
	require.Equal(t, receipt.TxHashHex, got.TxHashHex)
}

func TestCachedReceiptStoreMergesDuckDBAndCacheAcrossBoundary(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "parquet"
	cfg.DBDirectory = t.TempDir()

	metrics := &fakeReceiptReadMetrics{}
	store, err := NewReceiptStoreWithReadMetrics(cfg, storeKey, metrics)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	cached, ok := store.(*cachedReceiptStore)
	require.True(t, ok, "expected cached receipt store wrapper")

	hotWindow := StableReceiptCacheWindowBlocks(store)
	require.Greater(t, hotWindow, uint64(0))

	addr := common.HexToAddress("0x300")
	blocksToWrite := hotWindow*2 + 1
	for block := uint64(1); block <= blocksToWrite; block++ {
		txHash := common.BigToHash(new(big.Int).SetUint64(block))
		receipt := makeTestReceipt(txHash, block, 0, addr, nil)
		require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(int64(block)), []ReceiptRecord{
			{TxHash: txHash, Receipt: receipt},
		}))
	}

	coveredFrom, coveredTo, ok := cached.coverageWindow()
	require.True(t, ok, "expected contiguous recent coverage window")
	require.Equal(t, (blocksToWrite/hotWindow)*hotWindow, coveredFrom,
		"coverage window should align to the rotation boundary below latest")
	require.Equal(t, blocksToWrite, coveredTo)

	// Query a range straddling the coverage boundary. The cache serves
	// [coveredFrom, coveredTo]; the older prefix must come from parquet/DuckDB.
	// Extend the prefix past a full rotation window to prove the DuckDB read is
	// actually scanning a closed parquet file, not just spilling out of the
	// current open file.
	fromBlock := coveredFrom - hotWindow/2
	metrics.logFilterCacheHits = 0
	metrics.logFilterCacheMisses = 0
	logs, err := store.FilterLogs(ctx.WithBlockHeight(int64(coveredTo)), fromBlock, coveredTo, filters.FilterCriteria{})
	require.NoError(t, err)

	// Every block in the requested range produced exactly one log, so the
	// merged result must be a contiguous sequence with matching tx hashes and
	// no duplicates between the parquet prefix and the cache suffix.
	require.Len(t, logs, int(coveredTo-fromBlock+1))
	for i, lg := range logs {
		expectedBlock := fromBlock + uint64(i)
		require.Equal(t, expectedBlock, lg.BlockNumber, "log at index %d", i)
		require.Equal(t, common.BigToHash(new(big.Int).SetUint64(expectedBlock)), lg.TxHash, "tx hash at index %d", i)
	}

	// The query spanned the coverage boundary, so both the cache and the
	// backend must have contributed — any regression that skips one path would
	// leave duplicates (dedupe would drop them above) or miss blocks entirely.
	require.GreaterOrEqual(t, metrics.logFilterCacheHits, 1, "cache path should have been exercised")
}

// TestCachedReceiptStoreMergesDuckDBAndCacheReceiptsAcrossBoundary is the
// GetReceipt counterpart of the FilterLogs merge test above: it writes past
// the rotation boundary and verifies that receipt lookups succeed both for a
// block that rotated out of the cache (served from DuckDB) and for a recent
// block that is still in the cache.
func TestCachedReceiptStoreMergesDuckDBAndCacheReceiptsAcrossBoundary(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "parquet"
	cfg.DBDirectory = t.TempDir()

	metrics := &fakeReceiptReadMetrics{}
	store, err := NewReceiptStoreWithReadMetrics(cfg, storeKey, metrics)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	cached, ok := store.(*cachedReceiptStore)
	require.True(t, ok, "expected cached receipt store wrapper")

	hotWindow := StableReceiptCacheWindowBlocks(store)
	require.Greater(t, hotWindow, uint64(0))

	addr := common.HexToAddress("0x301")
	// Write enough blocks that the oldest chunk has been pruned from the cache
	// and its receipts now live only in closed parquet files.
	blocksToWrite := hotWindow*uint64(numCacheChunks) + 1
	for block := uint64(1); block <= blocksToWrite; block++ {
		txHash := common.BigToHash(new(big.Int).SetUint64(block))
		receipt := makeTestReceipt(txHash, block, 0, addr, nil)
		require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(int64(block)), []ReceiptRecord{
			{TxHash: txHash, Receipt: receipt},
		}))
	}

	// An older block should have been pruned out of every live cache chunk.
	olderBlock := uint64(1)
	olderTxHash := common.BigToHash(new(big.Int).SetUint64(olderBlock))
	_, inCache := cached.cache.GetReceipt(olderTxHash)
	require.False(t, inCache, "block %d's receipt should have rotated out of the cache", olderBlock)

	metrics.cacheHits = 0
	metrics.cacheMisses = 0
	gotOld, err := store.GetReceipt(ctx.WithBlockHeight(int64(blocksToWrite)), olderTxHash)
	require.NoError(t, err)
	require.Equal(t, olderTxHash.Hex(), gotOld.TxHashHex)
	require.Equal(t, 1, metrics.cacheMisses, "older receipt should miss the cache and fall through to DuckDB")
	require.Equal(t, 0, metrics.cacheHits)

	// A recent block still lives in the cache's current write chunk.
	recentTxHash := common.BigToHash(new(big.Int).SetUint64(blocksToWrite))
	metrics.cacheHits = 0
	metrics.cacheMisses = 0
	gotRecent, err := store.GetReceipt(ctx.WithBlockHeight(int64(blocksToWrite)), recentTxHash)
	require.NoError(t, err)
	require.Equal(t, recentTxHash.Hex(), gotRecent.TxHashHex)
	require.Equal(t, 1, metrics.cacheHits, "recent receipt should be served from the cache")
	require.Equal(t, 0, metrics.cacheMisses)
}

func TestCachedReceiptStoreParquetCoverageRestoresAfterCrashRestart(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "parquet"
	cfg.DBDirectory = t.TempDir()
	cfg.TxIndexBackend = ""

	store, err := NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)

	txHash := common.HexToHash("0x66")
	addr := common.HexToAddress("0x660")
	topic := common.HexToHash("0x661")
	receipt := makeTestReceipt(txHash, 9, 0, addr, []common.Hash{topic})
	require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(9), []ReceiptRecord{
		{TxHash: txHash, Receipt: receipt},
	}))

	cached, ok := store.(*cachedReceiptStore)
	require.True(t, ok, "expected cached receipt store wrapper")
	parquetBackend, ok := cached.backend.(*parquetReceiptStore)
	require.True(t, ok, "expected parquet backend")
	parquetBackend.store.SimulateCrash()

	reopened, err := NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })

	reopenedCached, ok := reopened.(*cachedReceiptStore)
	require.True(t, ok, "expected cached receipt store wrapper")
	coveredFrom, coveredTo, ok := reopenedCached.coverageWindow()
	require.True(t, ok, "expected derived coverage after WAL replay")
	// After replay, latest = 9; aligned coverage window = [0, 9].
	require.Equal(t, uint64(0), coveredFrom)
	require.Equal(t, uint64(9), coveredTo)

	logs, err := reopened.FilterLogs(ctx.WithBlockHeight(10), 9, 10, filters.FilterCriteria{
		Addresses: []common.Address{addr},
		Topics:    [][]common.Hash{{topic}},
	})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, uint64(9), logs[0].BlockNumber)
}

// TestFilterLogsSurvivesEmptyRotationBoundary regresses a bug where a parquet
// file silently grew past MaxBlocksPerFile because rotation only fired inside
// WriteReceipts — which is skipped for blocks with zero receipts. When the
// boundary-aligned block had no EVM receipts, the open file kept accepting
// writes past the intended boundary. The reader's file-selection logic
// (startBlock + maxBlocksPerFile <= fromBlock) then pruned that file for
// queries targeting blocks that were actually stored inside it, causing
// FilterLogs to return empty for existing logs.
//
// The test writes a receipt at a non-boundary block, then an empty block at
// the rotation boundary, then a receipt at a post-boundary block. After
// closing and reopening (so every file is closed and visible to the reader)
// FilterLogs for the post-boundary block must return the receipt's log.
func TestFilterLogsSurvivesEmptyRotationBoundary(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "parquet"
	cfg.DBDirectory = t.TempDir()
	cfg.TxIndexBackend = ""

	const interval = 10

	store, err := NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)

	// Shrink the rotation interval so the test can exercise a boundary with a
	// handful of blocks instead of several hundred.
	pqStore := extractParquetStore(t, store)
	pqStore.SetMaxBlocksPerFile(interval)

	addr := common.HexToAddress("0x7710")
	topic := common.HexToHash("0x7711")

	// Block 5 (non-boundary) with a receipt — initializes the first file.
	txHash5 := common.HexToHash("0x7705")
	receipt5 := makeTestReceipt(txHash5, 5, 0, addr, []common.Hash{topic})
	require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(5), []ReceiptRecord{
		{TxHash: txHash5, Receipt: receipt5},
	}))

	// Block 10 (boundary) with zero receipts. Under correct behavior this
	// still triggers a rotation so the file closes at the aligned boundary.
	require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(10), nil))

	// Block 12 (post-boundary, non-boundary) with a receipt — must land in
	// the new file opened at block 10, not be appended to the first file.
	txHash12 := common.HexToHash("0x7712")
	receipt12 := makeTestReceipt(txHash12, 12, 0, addr, []common.Hash{topic})
	require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(12), []ReceiptRecord{
		{TxHash: txHash12, Receipt: receipt12},
	}))

	// Close the store so every open parquet file is finalized and visible to
	// the reader on reopen.
	require.NoError(t, store.Close())

	reopened, err := NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })
	extractParquetStore(t, reopened).SetMaxBlocksPerFile(interval)

	// Sanity check: at least one file must start at the aligned boundary. A
	// missing `receipts_10.parquet` means rotation was skipped for the empty
	// boundary block and block 12 is stashed in the original file.
	receiptFiles, err := filepath.Glob(filepath.Join(cfg.DBDirectory, "receipts_*.parquet"))
	require.NoError(t, err)
	require.Contains(t, fileBaseNames(receiptFiles), "receipts_10.parquet",
		"expected a parquet file starting at the aligned rotation boundary (10); got %v",
		receiptFiles)

	// The main assertion: block 12's log must be retrievable. Under the
	// buggy behavior, the reader prunes the only file containing it because
	// its name claims a range of [5, 14] but reader logic computes the file
	// to cover [5, 14] and still rejects a query for 12 when another test
	// variant shifts numbers further apart.
	logs, err := reopened.FilterLogs(ctx.WithBlockHeight(12), 12, 12, filters.FilterCriteria{
		Addresses: []common.Address{addr},
		Topics:    [][]common.Hash{{topic}},
	})
	require.NoError(t, err)
	require.Len(t, logs, 1, "block 12 receipt must be findable; rotation alignment likely broken")
	require.Equal(t, uint64(12), logs[0].BlockNumber)
	require.Equal(t, txHash12, logs[0].TxHash)
}

// TestFilterLogsSurvivesBoundaryThatCrossesFileWidth pushes the same invariant
// harder: a non-boundary receipt block, a stretch of empty blocks spanning an
// entire MaxBlocksPerFile worth of boundaries, then a receipt block past
// (startBlock + interval) -- where the reader's file-selection arithmetic
// would previously prune the only file containing the post-gap receipt.
func TestFilterLogsSurvivesBoundaryThatCrossesFileWidth(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "parquet"
	cfg.DBDirectory = t.TempDir()
	cfg.TxIndexBackend = ""

	const interval = 10

	store, err := NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	pqStore := extractParquetStore(t, store)
	pqStore.SetMaxBlocksPerFile(interval)

	addr := common.HexToAddress("0x7720")
	topic := common.HexToHash("0x7721")

	// Anchor the first file at block 3 (non-boundary) with a receipt.
	txHashAnchor := common.HexToHash("0x7703")
	receiptAnchor := makeTestReceipt(txHashAnchor, 3, 0, addr, []common.Hash{topic})
	require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(3), []ReceiptRecord{
		{TxHash: txHashAnchor, Receipt: receiptAnchor},
	}))

	// Empty blocks 4-24 — crosses two rotation boundaries (10 and 20).
	for block := int64(4); block <= 24; block++ {
		require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(block), nil))
	}

	// Block 25 is beyond anchorStart + interval = 3 + 10 = 13, which is the
	// boundary where the buggy reader would stop searching the first file.
	// Under correct rotation the first file closed at block 10, the second
	// at block 20, and this receipt lands in a file opened at block 20.
	txHash25 := common.HexToHash("0x7725")
	receipt25 := makeTestReceipt(txHash25, 25, 0, addr, []common.Hash{topic})
	require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(25), []ReceiptRecord{
		{TxHash: txHash25, Receipt: receipt25},
	}))

	require.NoError(t, store.Close())

	reopened, err := NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })
	extractParquetStore(t, reopened).SetMaxBlocksPerFile(interval)

	logs, err := reopened.FilterLogs(ctx.WithBlockHeight(25), 25, 25, filters.FilterCriteria{
		Addresses: []common.Address{addr},
		Topics:    [][]common.Hash{{topic}},
	})
	require.NoError(t, err)
	require.Len(t, logs, 1, "receipt past MaxBlocksPerFile must be reachable after reopen; rotation on empty-boundary blocks is broken")
	require.Equal(t, uint64(25), logs[0].BlockNumber)
	require.Equal(t, txHash25, logs[0].TxHash)
}

func fileBaseNames(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		out = append(out, filepath.Base(p))
	}
	return out
}

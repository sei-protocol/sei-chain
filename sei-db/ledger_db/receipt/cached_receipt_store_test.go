package receipt

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

type fakeReceiptBackend struct {
	receipts            map[common.Hash]*types.Receipt
	logs                []*ethtypes.Log
	getReceiptCalls     int
	filterLogCalls      int
	lastFilterFromBlock uint64
	lastFilterToBlock   uint64
}

func newFakeReceiptBackend() *fakeReceiptBackend {
	return &fakeReceiptBackend{
		receipts: make(map[common.Hash]*types.Receipt),
	}
}

func (f *fakeReceiptBackend) LatestVersion() int64 {
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

func (f *fakeReceiptBackend) SetReceipts(_ sdk.Context, receipts []ReceiptRecord) error {
	for _, record := range receipts {
		if record.Receipt == nil {
			continue
		}
		f.receipts[record.TxHash] = record.Receipt
	}
	return nil
}

func (f *fakeReceiptBackend) FilterLogs(_ sdk.Context, from, to uint64, _ filters.FilterCriteria) ([]*ethtypes.Log, error) {
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
	store := newCachedReceiptStore(backend)

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

func TestCachedReceiptStoreFilterLogsSkipsBackendWhenCacheCovers(t *testing.T) {
	ctx, _ := newTestContext()
	backend := newFakeReceiptBackend()
	store := newCachedReceiptStore(backend)

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
	require.Equal(t, 0, backend.filterLogCalls, "backend should not be called when cache covers the range")
}

func TestCachedReceiptStoreFilterLogsSkipsBackendWhenCacheCoversGenesis(t *testing.T) {
	ctx, _ := newTestContext()
	ctx = ctx.WithBlockHeight(0)
	backend := newFakeReceiptBackend()
	store := newCachedReceiptStore(backend)

	txHash := common.HexToHash("0x21")
	addr := common.HexToAddress("0x201")
	topic := common.HexToHash("0xaaa")
	receipt := makeTestReceipt(txHash, 0, 0, addr, []common.Hash{topic})

	require.NoError(t, store.SetReceipts(ctx, []ReceiptRecord{{TxHash: txHash, Receipt: receipt}}))

	backend.filterLogCalls = 0
	logs, err := store.FilterLogs(ctx, 0, 0, filters.FilterCriteria{
		Addresses: []common.Address{addr},
		Topics:    [][]common.Hash{{topic}},
	})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, 0, backend.filterLogCalls, "backend should not be called when cache fully covers genesis")
}

func TestCachedReceiptStoreFilterLogsReturnsSortedLogs(t *testing.T) {
	ctx, _ := newTestContext()
	backend := newFakeReceiptBackend()
	// Backend holds older blocks that predate the cache window.
	backend.logs = []*ethtypes.Log{
		{BlockNumber: 8, TxIndex: 1, Index: 2},
		{BlockNumber: 5, TxIndex: 0, Index: 0},
	}
	store := newCachedReceiptStore(backend)

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

func TestFilterLogsPartialCacheNarrowsBackendRange(t *testing.T) {
	ctx, _ := newTestContext()
	backend := newFakeReceiptBackend()
	backend.logs = []*ethtypes.Log{
		{BlockNumber: 5, TxIndex: 0, Index: 0},
		{BlockNumber: 8, TxIndex: 0, Index: 0},
	}
	store := newCachedReceiptStore(backend)

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
	require.Equal(t, uint64(9), backend.lastFilterToBlock, "backend toBlock should be narrowed to cacheMin-1")

	require.Len(t, logs, 4)
	require.Equal(t, uint64(5), logs[0].BlockNumber)
	require.Equal(t, uint64(8), logs[1].BlockNumber)
	require.Equal(t, uint64(10), logs[2].BlockNumber)
	require.Equal(t, uint64(11), logs[3].BlockNumber)
}

func TestFilterLogsFallsBackToBackendWhenCacheEmpty(t *testing.T) {
	ctx, _ := newTestContext()
	backend := newFakeReceiptBackend()
	backend.logs = []*ethtypes.Log{
		{BlockNumber: 1, TxIndex: 0, Index: 0},
		{BlockNumber: 2, TxIndex: 0, Index: 0},
	}
	store := newCachedReceiptStore(backend)

	backend.filterLogCalls = 0
	logs, err := store.FilterLogs(ctx, 1, 5, filters.FilterCriteria{})
	require.NoError(t, err)
	require.Equal(t, 1, backend.filterLogCalls)
	require.Equal(t, uint64(1), backend.lastFilterFromBlock)
	require.Equal(t, uint64(5), backend.lastFilterToBlock, "full range passed when cache is empty")
	require.Len(t, logs, 2)
}

func TestFilterLogsMultipleBlocksCacheOnly(t *testing.T) {
	ctx, _ := newTestContext()
	backend := newFakeReceiptBackend()
	store := newCachedReceiptStore(backend)

	for block := uint64(100); block <= 105; block++ {
		txHash := common.BigToHash(new(big.Int).SetUint64(block))
		r := makeTestReceipt(txHash, block, 0, common.HexToAddress("0x1"), nil)
		require.NoError(t, store.SetReceipts(ctx, []ReceiptRecord{{TxHash: txHash, Receipt: r}}))
	}

	backend.filterLogCalls = 0
	logs, err := store.FilterLogs(ctx, 101, 104, filters.FilterCriteria{})
	require.NoError(t, err)
	require.Equal(t, 0, backend.filterLogCalls, "backend not called when entire range in cache")
	require.Len(t, logs, 4)
	for i, blockNum := range []uint64{101, 102, 103, 104} {
		require.Equal(t, blockNum, logs[i].BlockNumber)
	}
}

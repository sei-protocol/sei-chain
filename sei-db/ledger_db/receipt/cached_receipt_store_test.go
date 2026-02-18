package receipt

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

type fakeReceiptBackend struct {
	receipts        map[common.Hash]*types.Receipt
	logs            []*ethtypes.Log
	getReceiptCalls int
	filterLogCalls  int
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

func (f *fakeReceiptBackend) FilterLogs(_ sdk.Context, _, _ uint64, _ filters.FilterCriteria) ([]*ethtypes.Log, error) {
	f.filterLogCalls++
	return append([]*ethtypes.Log(nil), f.logs...), nil
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

func TestCachedReceiptStoreFilterLogsDelegates(t *testing.T) {
	ctx, _ := newTestContext()
	backend := newFakeReceiptBackend()
	store := newCachedReceiptStore(backend)

	txHash := common.HexToHash("0x2")
	addr := common.HexToAddress("0x200")
	topic := common.HexToHash("0xdef")
	receipt := makeTestReceipt(txHash, 9, 0, addr, []common.Hash{topic})

	require.NoError(t, store.SetReceipts(ctx, []ReceiptRecord{{TxHash: txHash, Receipt: receipt}}))

	backend.filterLogCalls = 0
	// FilterLogs checks both backend and cache
	logs, err := store.FilterLogs(ctx, 9, 9, filters.FilterCriteria{
		Addresses: []common.Address{addr},
		Topics:    [][]common.Hash{{topic}},
	})
	require.NoError(t, err)
	require.Len(t, logs, 1)                     // cache has the log (backend returns empty)
	require.Equal(t, 1, backend.filterLogCalls) // still delegates to backend
}

func TestCachedReceiptStoreFilterLogsReturnsSortedLogs(t *testing.T) {
	ctx, _ := newTestContext()
	backend := newFakeReceiptBackend()
	backend.logs = []*ethtypes.Log{
		{
			BlockNumber: 12,
			TxIndex:     1,
			Index:       2,
		},
		{
			BlockNumber: 10,
			TxIndex:     0,
			Index:       0,
		},
	}
	store := newCachedReceiptStore(backend)

	receiptA := makeTestReceipt(common.HexToHash("0xa"), 11, 1, common.HexToAddress("0x210"), []common.Hash{common.HexToHash("0x1")})
	receiptB := makeTestReceipt(common.HexToHash("0xb"), 11, 0, common.HexToAddress("0x220"), []common.Hash{common.HexToHash("0x2")})
	require.NoError(t, store.SetReceipts(ctx, []ReceiptRecord{
		{TxHash: common.HexToHash("0xa"), Receipt: receiptA},
		{TxHash: common.HexToHash("0xb"), Receipt: receiptB},
	}))

	logs, err := store.FilterLogs(ctx, 10, 12, filters.FilterCriteria{})
	require.NoError(t, err)
	require.Len(t, logs, 4)
	require.Equal(t, uint64(10), logs[0].BlockNumber)
	require.Equal(t, uint64(11), logs[1].BlockNumber)
	require.Equal(t, uint(0), logs[1].TxIndex)
	require.Equal(t, uint64(11), logs[2].BlockNumber)
	require.Equal(t, uint(1), logs[2].TxIndex)
	require.Equal(t, uint64(12), logs[3].BlockNumber)
}

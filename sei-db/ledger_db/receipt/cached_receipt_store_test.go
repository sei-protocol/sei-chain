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
	return []*ethtypes.Log{}, nil
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
	// FilterLogs now delegates to the backend for range queries
	logs, err := store.FilterLogs(ctx, 9, 9, filters.FilterCriteria{
		Addresses: []common.Address{addr},
		Topics:    [][]common.Hash{{topic}},
	})
	require.NoError(t, err)
	require.Len(t, logs, 0) // fake backend returns empty
	require.Equal(t, 1, backend.filterLogCalls)
}

package evmonly

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

func TestMemoryReceiptStoreIndexesOwnedReceiptCopies(t *testing.T) {
	store := NewMemoryReceiptStore()
	txHash := common.Hash{1}
	record := receipt.ReceiptRecord{
		TxHash: txHash,
		Receipt: &evmtypes.Receipt{
			TxHashHex:   txHash.Hex(),
			BlockNumber: 5,
			LogsBloom:   []byte{2},
			Logs: []*evmtypes.Log{{
				Address: common.Address{3}.Hex(),
				Topics:  []string{common.Hash{4}.Hex()},
				Data:    []byte{5},
			}},
		},
	}

	receiptCtx := newReceiptContext(t.Context(), 5)
	require.NoError(t, store.SetReceipts(receiptCtx, []receipt.ReceiptRecord{record}))
	record.Receipt.LogsBloom[0] = 12
	record.Receipt.Logs[0].Address = common.Address{13}.Hex()
	record.Receipt.Logs[0].Topics[0] = common.Hash{14}.Hex()
	record.Receipt.Logs[0].Data[0] = 15

	stored, err := store.GetReceipt(receiptCtx, txHash)
	require.NoError(t, err)
	require.Equal(t, []byte{2}, stored.LogsBloom)
	require.Equal(t, common.Address{3}.Hex(), stored.Logs[0].Address)
	require.Equal(t, []string{common.Hash{4}.Hex()}, stored.Logs[0].Topics)
	require.Equal(t, []byte{5}, stored.Logs[0].Data)

	stored.Status = 1
	stored.Logs[0].Data[0] = 16
	storedAgain, err := store.GetReceipt(receiptCtx, txHash)
	require.NoError(t, err)
	require.Zero(t, storedAgain.Status)
	require.Equal(t, []byte{5}, storedAgain.Logs[0].Data)
	require.Equal(t, int64(5), store.LatestVersion())
}

func TestMemoryReceiptStoreMovesReceiptsAndRecordsEmptyBlocks(t *testing.T) {
	store := NewMemoryReceiptStore()
	txHash := common.Hash{1}
	first := &evmtypes.Receipt{TxHashHex: txHash.Hex(), BlockNumber: 7}
	second := &evmtypes.Receipt{TxHashHex: txHash.Hex(), BlockNumber: 8}

	require.NoError(t, store.SetReceipts(newReceiptContext(t.Context(), 7), []receipt.ReceiptRecord{{TxHash: txHash, Receipt: first}}))
	require.NoError(t, store.SetReceipts(newReceiptContext(t.Context(), 8), []receipt.ReceiptRecord{{TxHash: txHash, Receipt: second}}))

	stored, err := store.GetReceipt(newReceiptContext(t.Context(), 8), txHash)
	require.NoError(t, err)
	require.Equal(t, uint64(8), stored.BlockNumber)
	require.NotContains(t, store.blocks, uint64(7))
	require.Equal(t, int64(8), store.LatestVersion())

	require.NoError(t, store.SetReceipts(newReceiptContext(t.Context(), 9), nil))
	require.Equal(t, int64(9), store.LatestVersion())
}

func TestMemoryReceiptStorePrunesHistory(t *testing.T) {
	store := NewMemoryReceiptStore()
	oldHash := common.Hash{1}
	newHash := common.Hash{2}
	records := []receipt.ReceiptRecord{
		{TxHash: oldHash, Receipt: &evmtypes.Receipt{TxHashHex: oldHash.Hex(), BlockNumber: 3}},
		{TxHash: newHash, Receipt: &evmtypes.Receipt{TxHashHex: newHash.Hex(), BlockNumber: 4}},
	}
	require.NoError(t, store.SetReceipts(newReceiptContext(t.Context(), 4), records))

	require.NoError(t, store.PruneHistory(4))
	_, err := store.GetReceipt(newReceiptContext(t.Context(), 4), oldHash)
	require.ErrorIs(t, err, receipt.ErrNotFound)
	_, err = store.GetReceipt(newReceiptContext(t.Context(), 4), newHash)
	require.NoError(t, err)
	require.Equal(t, int64(4), store.EarliestVersion())
	require.Equal(t, uint64(2), store.GetRollbackFloor(2))
}

func TestMemoryReceiptStoreHonorsCanceledContext(t *testing.T) {
	store := NewMemoryReceiptStore()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	receiptCtx := newReceiptContext(ctx, 1)

	require.ErrorIs(t, store.SetReceipts(receiptCtx, nil), context.Canceled)
	_, err := store.GetReceipt(receiptCtx, common.Hash{})
	require.ErrorIs(t, err, context.Canceled)
	_, err = store.FilterLogs(receiptCtx, 1, 1, filters.FilterCriteria{}, nil)
	require.ErrorIs(t, err, context.Canceled)
}

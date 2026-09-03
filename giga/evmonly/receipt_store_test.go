package evmonly

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

func TestMemoryReceiptStoreIndexesOwnedReceiptCopies(t *testing.T) {
	store := NewMemoryReceiptStore()
	txHash := common.Hash{1}
	receipt := &ethtypes.Receipt{
		PostState:         []byte{2},
		Status:            ethtypes.ReceiptStatusSuccessful,
		TxHash:            txHash,
		EffectiveGasPrice: big.NewInt(3),
		BlobGasPrice:      big.NewInt(4),
		BlockNumber:       big.NewInt(5),
		Logs: []*ethtypes.Log{{
			Address: common.Address{6},
			Topics:  []common.Hash{{7}},
			Data:    []byte{8},
		}},
	}

	require.NoError(t, store.SetReceipts(t.Context(), 5, ethtypes.Receipts{receipt}))
	receipt.PostState[0] = 12
	receipt.EffectiveGasPrice.SetUint64(13)
	receipt.BlobGasPrice.SetUint64(14)
	receipt.BlockNumber.SetUint64(15)
	receipt.Logs[0].Address = common.Address{16}
	receipt.Logs[0].Topics[0] = common.Hash{17}
	receipt.Logs[0].Data[0] = 18

	stored, found, err := store.GetReceipt(t.Context(), txHash)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte{2}, stored.PostState)
	require.Equal(t, big.NewInt(3), stored.EffectiveGasPrice)
	require.Equal(t, big.NewInt(4), stored.BlobGasPrice)
	require.Equal(t, big.NewInt(5), stored.BlockNumber)
	require.Equal(t, common.Address{6}, stored.Logs[0].Address)
	require.Equal(t, []common.Hash{{7}}, stored.Logs[0].Topics)
	require.Equal(t, []byte{8}, stored.Logs[0].Data)

	blockReceipts, found, err := store.GetBlockReceipts(t.Context(), 5)
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, blockReceipts, 1)
	blockReceipts[0].Status = ethtypes.ReceiptStatusFailed
	storedAgain, found, err := store.GetReceipt(t.Context(), txHash)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, uint64(ethtypes.ReceiptStatusSuccessful), storedAgain.Status)
}

func TestMemoryReceiptStoreReplacesBlocksAndRecordsEmptyBlocks(t *testing.T) {
	store := NewMemoryReceiptStore()
	oldHash := common.Hash{1}
	newHash := common.Hash{2}
	require.NoError(t, store.SetReceipts(t.Context(), 7, ethtypes.Receipts{{TxHash: oldHash}}))
	require.NoError(t, store.SetReceipts(t.Context(), 7, ethtypes.Receipts{{TxHash: newHash}}))

	_, found, err := store.GetReceipt(t.Context(), oldHash)
	require.NoError(t, err)
	require.False(t, found)
	stored, found, err := store.GetReceipt(t.Context(), newHash)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, newHash, stored.TxHash)

	require.NoError(t, store.SetReceipts(t.Context(), 8, nil))
	empty, found, err := store.GetBlockReceipts(t.Context(), 8)
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, empty)
	require.Empty(t, empty)

	_, found, err = store.GetBlockReceipts(t.Context(), 9)
	require.NoError(t, err)
	require.False(t, found)
}

func TestMemoryReceiptStoreHonorsCanceledContext(t *testing.T) {
	store := NewMemoryReceiptStore()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.ErrorIs(t, store.SetReceipts(ctx, 1, nil), context.Canceled)
	_, _, err := store.GetReceipt(ctx, common.Hash{})
	require.ErrorIs(t, err, context.Canceled)
	_, _, err = store.GetBlockReceipts(ctx, 1)
	require.ErrorIs(t, err, context.Canceled)
}

package parquet

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestGetReceiptByTxHashInBlock(t *testing.T) {
	dir := t.TempDir()

	for _, start := range []uint64{0, 500, 1000} {
		require.NoError(t, createTestReceiptFile(dir, start, 500))
	}

	reader, err := NewReaderWithMaxBlocksPerFile(dir, 500)
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	ctx := context.Background()

	txHash := common.BigToHash(new(big.Int).SetUint64(750))
	result, err := reader.GetReceiptByTxHashInBlock(ctx, txHash, 750)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, uint64(750), result.BlockNumber)

	// Query with wrong block number should return nil (file doesn't contain it).
	result, err = reader.GetReceiptByTxHashInBlock(ctx, txHash, 200)
	require.NoError(t, err)
	require.Nil(t, result, "should not find receipt in wrong file")
}

func TestGetReceiptByTxHashInBlockMissingFile(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, createTestReceiptFile(dir, 0, 500))

	reader, err := NewReaderWithMaxBlocksPerFile(dir, 500)
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	ctx := context.Background()

	// Block 999 is in file range 500-999, but that file doesn't exist.
	txHash := common.BigToHash(new(big.Int).SetUint64(999))
	result, err := reader.GetReceiptByTxHashInBlock(ctx, txHash, 999)
	require.NoError(t, err)
	require.Nil(t, result)
}

func TestStoreGetReceiptByTxHashUsesIndex(t *testing.T) {
	dir := t.TempDir()

	for _, start := range []uint64{0, 500, 1000} {
		require.NoError(t, createTestReceiptFile(dir, start, 500))
	}

	store, err := NewStore(StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 500,
	})
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	txHash := common.BigToHash(new(big.Int).SetUint64(750))

	// Populate the index manually for block 750.
	require.NoError(t, store.txIndex.SetBatch([]TxIndexEntry{
		{TxHash: txHash, BlockNumber: 750},
	}))

	ctx := context.Background()
	result, err := store.GetReceiptByTxHash(ctx, txHash)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, uint64(750), result.BlockNumber)
}

func TestStoreWriteReceiptsPopulatesIndex(t *testing.T) {
	dir := t.TempDir()

	store, err := NewStore(StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 500,
	})
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	txHash := common.HexToHash("0xdeadbeef")
	require.NoError(t, store.WriteReceipts([]ReceiptInput{{
		BlockNumber: 42,
		Receipt: ReceiptRecord{
			TxHash:       txHash[:],
			BlockNumber:  42,
			ReceiptBytes: []byte{0x1},
		},
		ReceiptBytes: []byte{0x1},
	}}))

	blockNum, ok := store.txIndex.GetBlockNumber(txHash)
	require.True(t, ok, "tx hash should be in the index after WriteReceipts")
	require.Equal(t, uint64(42), blockNum)
}

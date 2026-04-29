package parquet_v2

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
	"github.com/stretchr/testify/require"
)

func TestReadByTxHashFallsThroughToClosedFiles(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	txHash := common.HexToHash("0xabc")

	store, err := NewStore(parquet.StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 10,
	})
	require.NoError(t, err)
	require.NoError(t, store.WriteReceipts([]parquet.ReceiptInput{
		testReceiptInput(1, txHash),
		testReceiptInput(2, txHash),
	}))
	require.NoError(t, store.Close())

	reopened, err := NewStore(parquet.StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 10,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })

	result, err := reopened.GetReceiptByTxHash(ctx, txHash)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, uint64(1), result.BlockNumber)

	result, err = reopened.GetReceiptByTxHashInBlock(ctx, txHash, 2)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, uint64(2), result.BlockNumber)
	require.Equal(t, testReceiptInput(2, txHash).ReceiptBytes, result.ReceiptBytes)
}

func TestReadByTxHashAfterRotationUsesClosedFilesAndTempCache(t *testing.T) {
	ctx := context.Background()
	txHash := common.HexToHash("0xabc")

	store, err := NewStore(parquet.StoreConfig{
		DBDirectory:      t.TempDir(),
		MaxBlocksPerFile: 4,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	require.NoError(t, store.WriteReceipts([]parquet.ReceiptInput{
		testReceiptInput(1, txHash),
		testReceiptInput(2, common.HexToHash("0x2")),
		testReceiptInput(3, common.HexToHash("0x3")),
		testReceiptInput(4, common.HexToHash("0x4")),
		testReceiptInput(5, txHash),
	}))

	closedResult, err := store.GetReceiptByTxHashInBlock(ctx, txHash, 1)
	require.NoError(t, err)
	require.NotNil(t, closedResult)
	require.Equal(t, uint64(1), closedResult.BlockNumber)

	openResult, err := store.GetReceiptByTxHashInBlock(ctx, txHash, 5)
	require.NoError(t, err)
	require.NotNil(t, openResult)
	require.Equal(t, uint64(5), openResult.BlockNumber)
	require.Equal(t, testReceiptInput(5, txHash).ReceiptBytes, openResult.ReceiptBytes)
}

func TestGetLogsReadsAcrossClosedFiles(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := NewStore(parquet.StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 4,
	})
	require.NoError(t, err)

	var inputs []parquet.ReceiptInput
	for block := uint64(1); block <= 8; block++ {
		inputs = append(inputs, testReceiptInput(block, common.BigToHash(new(big.Int).SetUint64(block))))
	}
	require.NoError(t, store.WriteReceipts(inputs))
	require.NoError(t, store.Close())

	reopened, err := NewStore(parquet.StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 4,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })

	from, to := uint64(2), uint64(6)
	results, err := reopened.GetLogs(ctx, parquet.LogFilter{
		FromBlock: &from,
		ToBlock:   &to,
	})
	require.NoError(t, err)
	require.Len(t, results, 5)
	require.Equal(t, []uint64{2, 3, 4, 5, 6}, logBlockNumbers(results))

	address := common.BigToAddress(new(big.Int).SetUint64(5))
	results, err = reopened.GetLogs(ctx, parquet.LogFilter{
		FromBlock: &from,
		ToBlock:   &to,
		Addresses: []common.Address{address},
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, uint64(5), results[0].BlockNumber)
}

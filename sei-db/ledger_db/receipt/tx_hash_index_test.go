package receipt

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
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

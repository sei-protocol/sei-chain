package parquet

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestTxIndexSetAndGet(t *testing.T) {
	idx, err := OpenTxIndex(t.TempDir())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	txHash := common.HexToHash("0xabc123")
	blockNum, ok := idx.GetBlockNumber(txHash)
	require.False(t, ok, "empty index should return not found")
	require.Equal(t, uint64(0), blockNum)

	require.NoError(t, idx.SetBatch([]TxIndexEntry{
		{TxHash: txHash, BlockNumber: 42},
	}))

	blockNum, ok = idx.GetBlockNumber(txHash)
	require.True(t, ok)
	require.Equal(t, uint64(42), blockNum)
}

func TestTxIndexBatchWrite(t *testing.T) {
	idx, err := OpenTxIndex(t.TempDir())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	entries := make([]TxIndexEntry, 100)
	for i := range entries {
		var h common.Hash
		h[0] = byte(i)
		entries[i] = TxIndexEntry{TxHash: h, BlockNumber: uint64(1000 + i)}
	}

	require.NoError(t, idx.SetBatch(entries))

	for _, e := range entries {
		blockNum, ok := idx.GetBlockNumber(e.TxHash)
		require.True(t, ok)
		require.Equal(t, e.BlockNumber, blockNum)
	}
}

func TestTxIndexPruneBefore(t *testing.T) {
	idx, err := OpenTxIndex(t.TempDir())
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	require.NoError(t, idx.SetBatch([]TxIndexEntry{
		{TxHash: common.HexToHash("0x01"), BlockNumber: 100},
		{TxHash: common.HexToHash("0x02"), BlockNumber: 200},
		{TxHash: common.HexToHash("0x03"), BlockNumber: 300},
		{TxHash: common.HexToHash("0x04"), BlockNumber: 400},
	}))

	require.NoError(t, idx.PruneBefore(250))

	_, ok := idx.GetBlockNumber(common.HexToHash("0x01"))
	require.False(t, ok, "block 100 should be pruned")
	_, ok = idx.GetBlockNumber(common.HexToHash("0x02"))
	require.False(t, ok, "block 200 should be pruned")

	blockNum, ok := idx.GetBlockNumber(common.HexToHash("0x03"))
	require.True(t, ok, "block 300 should survive pruning")
	require.Equal(t, uint64(300), blockNum)

	blockNum, ok = idx.GetBlockNumber(common.HexToHash("0x04"))
	require.True(t, ok, "block 400 should survive pruning")
	require.Equal(t, uint64(400), blockNum)
}

func TestTxIndexNilSafety(t *testing.T) {
	var idx *TxIndex
	require.NoError(t, idx.Close())
	require.NoError(t, idx.SetBatch([]TxIndexEntry{{BlockNumber: 1}}))
	require.NoError(t, idx.PruneBefore(100))
	_, ok := idx.GetBlockNumber(common.Hash{})
	require.False(t, ok)
}

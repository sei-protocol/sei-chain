package parquet

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	pqgo "github.com/parquet-go/parquet-go"
	"github.com/stretchr/testify/require"
)

func createTestLogFile(dir string, startBlock, count uint64) error {
	path := fmt.Sprintf("%s/logs_%d.parquet", dir, startBlock)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := pqgo.NewGenericWriter[LogRecord](f)
	for i := uint64(0); i < count; i++ {
		block := startBlock + i
		txHash := common.BigToHash(new(big.Int).SetUint64(block))
		if _, err := w.Write([]LogRecord{{
			BlockNumber: block,
			TxHash:      txHash[:],
			Address:     common.HexToAddress("0xdead").Bytes(),
		}}); err != nil {
			return err
		}
	}
	if err := w.Close(); err != nil {
		return err
	}
	return f.Close()
}

func uint64Ptr(v uint64) *uint64 { return &v }

func TestGetLogsPrunesFilesBelowFromBlock(t *testing.T) {
	dir := t.TempDir()

	for _, start := range []uint64{0, 500, 1000, 1500} {
		require.NoError(t, createTestReceiptFile(dir, start, 500))
		require.NoError(t, createTestLogFile(dir, start, 500))
	}

	reader, err := NewReaderWithMaxBlocksPerFile(dir, 500)
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	ctx := context.Background()

	results, err := reader.GetLogs(ctx, LogFilter{
		FromBlock: uint64Ptr(1200),
		ToBlock:   uint64Ptr(1300),
	})
	require.NoError(t, err)

	for _, r := range results {
		require.GreaterOrEqual(t, r.BlockNumber, uint64(1200))
		require.LessOrEqual(t, r.BlockNumber, uint64(1300))
	}
	require.Equal(t, 101, len(results), "should have blocks 1200-1300 inclusive")
}

func TestGetLogsPrunesBothEnds(t *testing.T) {
	dir := t.TempDir()

	for _, start := range []uint64{0, 500, 1000, 1500, 2000} {
		require.NoError(t, createTestReceiptFile(dir, start, 500))
		require.NoError(t, createTestLogFile(dir, start, 500))
	}

	reader, err := NewReaderWithMaxBlocksPerFile(dir, 500)
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	ctx := context.Background()

	// Query blocks 1400-1600: should need overlapping files 1000 and 1500,
	// but still prune non-overlapping files 0, 500, and 2000.
	results, err := reader.GetLogs(ctx, LogFilter{
		FromBlock: uint64Ptr(1400),
		ToBlock:   uint64Ptr(1600),
	})
	require.NoError(t, err)

	for _, r := range results {
		require.GreaterOrEqual(t, r.BlockNumber, uint64(1400))
		require.LessOrEqual(t, r.BlockNumber, uint64(1600))
	}
	require.Equal(t, 201, len(results), "should have blocks 1400-1600 inclusive")
}

func TestStoreGetReceiptByTxHashWithoutIndex(t *testing.T) {
	dir := t.TempDir()

	for _, start := range []uint64{0, 500, 1000} {
		require.NoError(t, createTestReceiptFile(dir, start, 500))
	}

	store, err := NewStore(StoreConfig{
		DBDirectory:          dir,
		MaxBlocksPerFile:     500,
		DisableTxIndexLookup: true,
	})
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	txHash := common.BigToHash(new(big.Int).SetUint64(750))

	ctx := context.Background()
	result, err := store.GetReceiptByTxHash(ctx, txHash)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, uint64(750), result.BlockNumber)
}

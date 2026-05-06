package coordinator

import (
	"context"
	"errors"
	"math/big"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
	"github.com/stretchr/testify/require"
)

func TestReplayWALAppliesReceiptsAndPreservesDuplicateHashes(t *testing.T) {
	wal := replayWALWithEntries(t,
		parquet.WALEntry{BlockNumber: 1, Receipts: [][]byte{{7, 1}, {7, 2}}},
		parquet.WALEntry{BlockNumber: 2, Receipts: [][]byte{{8, 1}}},
	)
	coord := newReplayCoordinator(t, wal)
	defer func() { require.NoError(t, coord.closeWriters()) }()

	result, err := coord.replayWAL(replayConverterForTest)
	require.NoError(t, err)

	duplicateHash := common.BigToHash(new(big.Int).SetUint64(7))
	require.Len(t, result.WarmupRecords, 3)
	require.Len(t, result.Blocks, 2)
	require.Equal(t, uint64(1), result.Blocks[0].BlockNumber)
	require.Equal(t, []common.Hash{duplicateHash, duplicateHash}, result.Blocks[0].TxHashes)
	require.Len(t, coord.tempWriteCache[duplicateHash], 2)
	require.Equal(t, int64(2), coord.latestVersion)
	require.Empty(t, wal.truncatedBefore)
}

func TestReplayWALSkipsEntriesBeforeFileStartAndTruncates(t *testing.T) {
	wal := replayWALWithEntries(t,
		parquet.WALEntry{BlockNumber: 2, Receipts: [][]byte{{2}}},
		parquet.WALEntry{BlockNumber: 4, Receipts: [][]byte{{4}}},
	)
	coord := newReplayCoordinator(t, wal)
	coord.fileStartBlock = 4
	defer func() { require.NoError(t, coord.closeWriters()) }()

	result, err := coord.replayWAL(func(blockNumber uint64, receiptBytes []byte, logStartIndex uint) (parquet.ReplayReceipt, error) {
		require.NotEqual(t, uint64(2), blockNumber)
		return replayConverterForTest(blockNumber, receiptBytes, logStartIndex)
	})
	require.NoError(t, err)

	require.Len(t, result.WarmupRecords, 1)
	require.Equal(t, uint64(4), result.WarmupRecords[0].BlockNumber)
	require.Equal(t, []uint64{2}, wal.truncatedBefore)
	require.Equal(t, int64(4), coord.latestVersion)
}

func TestReplayWALRotatesBoundaryWithoutClearingWAL(t *testing.T) {
	wal := replayWALWithEntries(t,
		parquet.WALEntry{BlockNumber: 1, Receipts: [][]byte{{1}}},
		parquet.WALEntry{BlockNumber: 4, Receipts: [][]byte{{4}}},
	)
	coord := newReplayCoordinator(t, wal)
	defer func() { require.NoError(t, coord.closeWriters()) }()

	_, err := coord.replayWAL(replayConverterForTest)
	require.NoError(t, err)

	require.Len(t, coord.closedFiles, 1)
	require.Equal(t, uint64(0), coord.closedFiles[0].startBlock)
	require.Equal(t, uint64(4), coord.fileStartBlock)
	require.FileExists(t, filepath.Join(coord.basePath, "receipts_0.parquet"))
	require.FileExists(t, filepath.Join(coord.basePath, "receipts_4.parquet"))
	require.Equal(t, []uint64{2}, wal.truncatedBefore)
	require.Len(t, coord.tempWriteCache, 1)
	require.Contains(t, coord.tempWriteCache, common.BigToHash(new(big.Int).SetUint64(4)))
}

func TestNewClosesReplayWritersOnReplayError(t *testing.T) {
	dir := t.TempDir()
	wal, err := parquet.NewWAL(filepath.Join(dir, "parquet-wal"))
	require.NoError(t, err)
	require.NoError(t, wal.Write(parquet.WALEntry{BlockNumber: 1, Receipts: [][]byte{{1}}}))
	require.NoError(t, wal.Write(parquet.WALEntry{BlockNumber: 2, Receipts: [][]byte{{2}}}))
	require.NoError(t, wal.Close())

	calls := 0
	_, err = New(parquet.StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 4,
		WALConverter: func(blockNumber uint64, receiptBytes []byte, logStartIndex uint) (parquet.ReplayReceipt, error) {
			calls++
			if calls == 2 {
				return parquet.ReplayReceipt{}, errors.New("injected replay failure")
			}
			return replayConverterForTest(blockNumber, receiptBytes, logStartIndex)
		},
	})
	require.ErrorContains(t, err, "injected replay failure")
	require.Equal(t, 2, calls)

	reader, err := NewReaderWithMaxBlocksPerFile(dir, 4)
	require.NoError(t, err)
	defer func() { require.NoError(t, reader.Close()) }()

	ctx := context.Background()
	_, err = reader.QueryReceiptByTxHash(ctx, []string{filepath.Join(dir, "receipts_0.parquet")}, common.BigToHash(new(big.Int).SetUint64(1)))
	require.NoError(t, err)
	_, err = reader.QueryLogs(ctx, []string{filepath.Join(dir, "logs_0.parquet")}, parquet.LogFilter{})
	require.NoError(t, err)
}

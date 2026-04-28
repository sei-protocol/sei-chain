package parquet_v2

import (
	"context"
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

	result, err := coord.replayWAL(func(blockNumber uint64, receiptBytes []byte, logStartIndex uint) (ReplayReceipt, error) {
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

func TestReplayWALRequiresConverter(t *testing.T) {
	store, err := NewStore(parquet.StoreConfig{DBDirectory: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	_, err = store.ReplayWAL(nil)
	require.ErrorContains(t, err, "converter is nil")
}

func replayWALWithEntries(t *testing.T, entries ...parquet.WALEntry) *recordingWAL {
	t.Helper()

	wal := &recordingWAL{}
	for _, entry := range entries {
		require.NoError(t, wal.Write(entry))
	}
	return wal
}

func newReplayCoordinator(t *testing.T, wal *recordingWAL) *coordinator {
	t.Helper()

	coord := newWriteCoordinator(t, wal)
	coord.config.MaxBlocksPerFile = 4
	return coord
}

func replayConverterForTest(blockNumber uint64, receiptBytes []byte, _ uint) (ReplayReceipt, error) {
	txHash := common.BigToHash(new(big.Int).SetUint64(uint64(receiptBytes[0])))
	input := testReceiptInput(blockNumber, txHash)
	input.ReceiptBytes = append([]byte(nil), receiptBytes...)
	input.Receipt.ReceiptBytes = append([]byte(nil), receiptBytes...)

	return ReplayReceipt{
		Input:    input,
		TxHash:   txHash,
		Warmup:   input.Receipt,
		LogCount: uint(len(input.Logs)),
	}, nil
}

func TestReplayWALPublicDispatch(t *testing.T) {
	store := newDispatchStore(t)
	_, err := store.ReplayWAL(func(blockNumber uint64, receiptBytes []byte, logStartIndex uint) (ReplayReceipt, error) {
		return replayConverterForTest(blockNumber, receiptBytes, logStartIndex)
	})
	require.NoError(t, err)

	result, err := store.GetReceiptByTxHash(context.Background(), common.HexToHash("0x1"))
	require.NoError(t, err)
	require.Nil(t, result)
}

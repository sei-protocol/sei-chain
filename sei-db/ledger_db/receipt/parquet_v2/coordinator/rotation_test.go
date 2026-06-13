package coordinator

import (
	"math/big"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
	"github.com/stretchr/testify/require"
)

func TestRotationBoundaryPrimitives(t *testing.T) {
	coord := &Coordinator{
		config: parquet.StoreConfig{MaxBlocksPerFile: 500},
	}

	require.True(t, coord.isRotationBoundary(0))
	require.True(t, coord.isRotationBoundary(500))
	require.False(t, coord.isRotationBoundary(501))

	coord.config.MaxBlocksPerFile = 0
	require.False(t, coord.isRotationBoundary(500))
}

func TestAlignedFileStartBlock(t *testing.T) {
	require.Equal(t, uint64(5000), alignedFileStartBlock(5234, 500))
	require.Equal(t, uint64(5000), alignedFileStartBlock(5000, 500))
	require.Equal(t, uint64(0), alignedFileStartBlock(499, 500))
	require.Equal(t, uint64(5234), alignedFileStartBlock(5234, 0))
}

func TestWriteRotatesAtAlignedBoundary(t *testing.T) {
	wal := &recordingWAL{}
	coord := newWriteCoordinator(t, wal)
	coord.config.MaxBlocksPerFile = 4
	defer func() { require.NoError(t, coord.closeWriters()) }()

	for block := uint64(1); block <= 4; block++ {
		require.NoError(t, coord.writeReceipts(block, []parquet.ReceiptInput{
			testReceiptInput(block, common.BigToHash(new(big.Int).SetUint64(block))),
		}))
	}

	require.Len(t, coord.closedFiles, 1)
	require.Equal(t, uint64(0), coord.closedFiles[0].startBlock)
	require.Equal(t, uint64(4), coord.fileStartBlock)
	require.FileExists(t, filepath.Join(coord.basePath, "receipts_0.parquet"))
	require.FileExists(t, filepath.Join(coord.basePath, "logs_0.parquet"))
	require.FileExists(t, filepath.Join(coord.basePath, "receipts_4.parquet"))
	require.FileExists(t, filepath.Join(coord.basePath, "logs_4.parquet"))

	require.Len(t, wal.truncatedBefore, 1)
	require.Equal(t, uint64(4), wal.truncatedBefore[0])
	require.Len(t, coord.tempWriteCache, 1)
	require.Contains(t, coord.tempWriteCache, common.BigToHash(big.NewInt(4)))
}

func TestRotateOpenFilePrunesOnlyOldTempCacheEntries(t *testing.T) {
	txHash := common.HexToHash("0xabc")
	coord := &Coordinator{
		tempWriteCache: map[common.Hash][]tempReceipt{
			txHash: {
				{blockNumber: 1},
				{blockNumber: 4},
			},
			common.HexToHash("0xdef"): {
				{blockNumber: 2},
			},
		},
	}

	coord.dropTempCacheBefore(4)

	require.Len(t, coord.tempWriteCache, 1)
	require.Len(t, coord.tempWriteCache[txHash], 1)
	require.Equal(t, uint64(4), coord.tempWriteCache[txHash][0].blockNumber)
}

func TestWriteEmptyHonorsMonotonicLastSeen(t *testing.T) {
	coord := newWriteCoordinator(t, &recordingWAL{})

	require.NoError(t, coord.writeReceipts(5, nil))
	require.Equal(t, uint64(5), coord.lastSeenBlock)

	require.NoError(t, coord.writeReceipts(4, nil))
	require.Equal(t, uint64(5), coord.lastSeenBlock)
	require.Empty(t, coord.closedFiles)
}

func TestWriteEmptyRotatesAtBoundary(t *testing.T) {
	wal := &recordingWAL{}
	coord := newWriteCoordinator(t, wal)
	coord.config.MaxBlocksPerFile = 4
	defer func() { require.NoError(t, coord.closeWriters()) }()

	require.NoError(t, coord.writeReceipts(1, []parquet.ReceiptInput{
		testReceiptInput(1, common.HexToHash("0x1")),
	}))
	require.NotNil(t, coord.receiptWriter)

	require.NoError(t, coord.writeReceipts(4, nil))

	require.Equal(t, uint64(4), coord.lastSeenBlock)
	require.Equal(t, uint64(4), coord.fileStartBlock)
	require.Len(t, coord.closedFiles, 1)
	require.Equal(t, uint64(0), coord.closedFiles[0].startBlock)
	require.FileExists(t, filepath.Join(coord.basePath, "receipts_0.parquet"))
	require.FileExists(t, filepath.Join(coord.basePath, "receipts_4.parquet"))
	require.Empty(t, coord.tempWriteCache)
}

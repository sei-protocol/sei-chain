package coordinator

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
	"github.com/stretchr/testify/require"
)

func TestWriteReceiptsGroupsWALByBlockEncounterOrder(t *testing.T) {
	wal := &recordingWAL{}
	coord := newWriteCoordinator(t, wal)
	defer func() { require.NoError(t, coord.closeWriters()) }()

	require.NoError(t, coord.writeReceipts([]parquet.ReceiptInput{
		testReceiptInput(2, common.HexToHash("0x22")),
		testReceiptInput(1, common.HexToHash("0x11")),
		testReceiptInput(2, common.HexToHash("0x23")),
	}))

	require.Len(t, wal.entries, 2)
	require.Equal(t, uint64(2), wal.entries[0].BlockNumber)
	require.Len(t, wal.entries[0].Receipts, 2)
	require.Equal(t, uint64(1), wal.entries[1].BlockNumber)
	require.Len(t, wal.entries[1].Receipts, 1)
}

func TestWriteReceiptsKeepsDuplicateHashCacheEntries(t *testing.T) {
	wal := &recordingWAL{}
	coord := newWriteCoordinator(t, wal)
	defer func() { require.NoError(t, coord.closeWriters()) }()

	txHash := common.HexToHash("0xabc")
	require.NoError(t, coord.writeReceipts([]parquet.ReceiptInput{
		testReceiptInput(1, txHash),
		testReceiptInput(2, txHash),
	}))

	require.Len(t, coord.receiptsBuffer, 2)
	require.Equal(t, int64(2), coord.latestVersion)
	require.Len(t, coord.tempWriteCache[txHash], 2)
	require.Equal(t, uint64(1), coord.tempWriteCache[txHash][0].blockNumber)
	require.Equal(t, uint64(2), coord.tempWriteCache[txHash][1].blockNumber)
}

func TestWriteReceiptsFlushesAtConfiguredBlockInterval(t *testing.T) {
	wal := &recordingWAL{}
	coord := newWriteCoordinator(t, wal)
	coord.config.BlockFlushInterval = 1
	defer func() { require.NoError(t, coord.closeWriters()) }()

	require.NoError(t, coord.writeReceipts([]parquet.ReceiptInput{
		testReceiptInput(1, common.HexToHash("0x1")),
		testReceiptInput(2, common.HexToHash("0x2")),
	}))

	require.Empty(t, coord.receiptsBuffer)
	require.Empty(t, coord.logsBuffer)
	require.Zero(t, coord.blocksSinceFlush)
	require.Equal(t, int64(2), coord.latestVersion)
}

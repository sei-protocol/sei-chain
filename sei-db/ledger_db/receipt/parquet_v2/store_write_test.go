package parquet_v2

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
	"github.com/stretchr/testify/require"
)

func TestWriteReceiptsUpdatesLatestAndReopens(t *testing.T) {
	dir := t.TempDir()

	store, err := NewStore(parquet.StoreConfig{
		DBDirectory:          dir,
		MaxBlocksPerFile:     500,
		BlockFlushInterval:   100,
		PruneIntervalSeconds: 0,
	})
	require.NoError(t, err)

	require.NoError(t, store.WriteReceipts([]parquet.ReceiptInput{
		testReceiptInput(1, common.HexToHash("0x1")),
		testReceiptInput(2, common.HexToHash("0x2")),
		testReceiptInput(3, common.HexToHash("0x3")),
	}))
	require.Equal(t, int64(3), store.LatestVersion())
	require.NoError(t, store.Close())

	reopened, err := NewStore(parquet.StoreConfig{
		DBDirectory:          dir,
		MaxBlocksPerFile:     500,
		PruneIntervalSeconds: 0,
	})
	require.NoError(t, err)
	require.Equal(t, int64(3), reopened.LatestVersion())
	require.Equal(t, uint64(4), reopened.FileStartBlock())
	require.NoError(t, reopened.Close())
}

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
	require.Equal(t, uint64(0), coord.tempWriteCache[txHash][0].writeOrdinal)
	require.Equal(t, uint64(2), coord.tempWriteCache[txHash][1].blockNumber)
	require.Equal(t, uint64(1), coord.tempWriteCache[txHash][1].writeOrdinal)
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

func newWriteCoordinator(t *testing.T, wal *recordingWAL) *coordinator {
	t.Helper()

	cfg := parquet.DefaultStoreConfig()
	cfg.DBDirectory = t.TempDir()
	cfg.MaxBlocksPerFile = 500
	cfg.BlockFlushInterval = 0

	return &coordinator{
		config:         cfg,
		basePath:       cfg.DBDirectory,
		receiptsBuffer: make([]parquet.ReceiptRecord, 0, 1000),
		logsBuffer:     make([]parquet.LogRecord, 0, 10000),
		tempWriteCache: make(map[common.Hash][]tempReceipt),
		wal:            wal,
	}
}

func testReceiptInput(blockNumber uint64, txHash common.Hash) parquet.ReceiptInput {
	receiptBytes := []byte{byte(blockNumber), txHash[31]}
	return parquet.ReceiptInput{
		BlockNumber: blockNumber,
		Receipt: parquet.ReceiptRecord{
			TxHash:       txHash[:],
			BlockNumber:  blockNumber,
			ReceiptBytes: receiptBytes,
		},
		Logs: []parquet.LogRecord{{
			BlockNumber: blockNumber,
			TxHash:      txHash[:],
			Address:     common.BigToAddress(new(big.Int).SetUint64(blockNumber)).Bytes(),
		}},
		ReceiptBytes: receiptBytes,
	}
}

type recordingWAL struct {
	entries []parquet.WALEntry
}

func (w *recordingWAL) Write(entry parquet.WALEntry) error {
	w.entries = append(w.entries, entry)
	return nil
}

func (w *recordingWAL) TruncateBefore(uint64) error { return nil }

func (w *recordingWAL) TruncateAfter(uint64) error { return nil }

func (w *recordingWAL) ReadAt(uint64) (parquet.WALEntry, error) { return parquet.WALEntry{}, nil }

func (w *recordingWAL) FirstOffset() (uint64, error) { return 0, nil }

func (w *recordingWAL) LastOffset() (uint64, error) { return 0, nil }

func (w *recordingWAL) Replay(uint64, uint64, func(uint64, parquet.WALEntry) error) error {
	return nil
}

func (w *recordingWAL) Close() error { return nil }

var _ interface {
	Write(parquet.WALEntry) error
	TruncateBefore(uint64) error
	TruncateAfter(uint64) error
	ReadAt(uint64) (parquet.WALEntry, error)
	FirstOffset() (uint64, error)
	LastOffset() (uint64, error)
	Replay(uint64, uint64, func(uint64, parquet.WALEntry) error) error
	Close() error
} = (*recordingWAL)(nil)

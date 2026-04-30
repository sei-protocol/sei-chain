package coordinator

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	parquetgo "github.com/parquet-go/parquet-go"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
	"github.com/stretchr/testify/require"
)

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

func newWriteCoordinator(t *testing.T, wal *recordingWAL) *Coordinator {
	t.Helper()

	cfg := parquet.DefaultStoreConfig()
	cfg.DBDirectory = t.TempDir()
	cfg.MaxBlocksPerFile = 500
	cfg.BlockFlushInterval = 0

	c := &Coordinator{
		config:         cfg,
		basePath:       cfg.DBDirectory,
		receiptsBuffer: make([]parquet.ReceiptRecord, 0, 1000),
		logsBuffer:     make([]parquet.LogRecord, 0, 10000),
		tempWriteCache: make(map[common.Hash][]tempReceipt),
		wal:            wal,
	}
	bootstrapWorkersForTest(c)
	t.Cleanup(func() { c.shutdownWorkers() })
	return c
}

func newReplayCoordinator(t *testing.T, wal *recordingWAL) *Coordinator {
	t.Helper()

	coord := newWriteCoordinator(t, wal)
	coord.config.MaxBlocksPerFile = 4
	return coord
}

func replayWALWithEntries(t *testing.T, entries ...parquet.WALEntry) *recordingWAL {
	t.Helper()

	wal := &recordingWAL{}
	for _, entry := range entries {
		require.NoError(t, wal.Write(entry))
	}
	return wal
}

func replayConverterForTest(blockNumber uint64, receiptBytes []byte, _ uint) (parquet.ReplayReceipt, error) {
	txHash := common.BigToHash(new(big.Int).SetUint64(uint64(receiptBytes[0])))
	input := testReceiptInput(blockNumber, txHash)
	input.ReceiptBytes = append([]byte(nil), receiptBytes...)
	input.Receipt.ReceiptBytes = append([]byte(nil), receiptBytes...)

	return parquet.ReplayReceipt{
		Input:    input,
		TxHash:   txHash,
		Warmup:   input.Receipt,
		LogCount: uint(len(input.Logs)),
	}, nil
}

func writeReceiptFile(t *testing.T, dir string, startBlock uint64, blocks []uint64) {
	t.Helper()

	path := filepath.Join(dir, fmt.Sprintf("receipts_%d.parquet", startBlock))
	f, err := os.Create(path)
	require.NoError(t, err)

	w := parquetgo.NewGenericWriter[parquet.ReceiptRecord](f)
	for _, block := range blocks {
		txHash := common.BigToHash(new(big.Int).SetUint64(block))
		_, err := w.Write([]parquet.ReceiptRecord{{
			TxHash:       txHash[:],
			BlockNumber:  block,
			ReceiptBytes: []byte{byte(block)},
		}})
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	require.NoError(t, f.Close())
}

func writeLogFile(t *testing.T, dir string, startBlock uint64) {
	t.Helper()

	path := filepath.Join(dir, fmt.Sprintf("logs_%d.parquet", startBlock))
	f, err := os.Create(path)
	require.NoError(t, err)

	w := parquetgo.NewGenericWriter[parquet.LogRecord](f)
	txHash := common.BigToHash(new(big.Int).SetUint64(startBlock))
	_, err = w.Write([]parquet.LogRecord{{
		BlockNumber: startBlock,
		TxHash:      txHash[:],
	}})
	require.NoError(t, err)
	require.NoError(t, w.Close())
	require.NoError(t, f.Close())
}

func writeClosedFileSet(t *testing.T, dir string, starts ...uint64) []closedFile {
	t.Helper()

	closed := make([]closedFile, 0, len(starts))
	for _, start := range starts {
		block := start + 1
		writeReceiptFile(t, dir, start, []uint64{block})
		writeLogFile(t, dir, start)
		closed = append(closed, closedFile{
			startBlock:  start,
			receiptPath: filepath.Join(dir, "receipts_"+strconv.FormatUint(start, 10)+".parquet"),
			logPath:     filepath.Join(dir, "logs_"+strconv.FormatUint(start, 10)+".parquet"),
		})
	}
	return closed
}

func readClosedReceiptForTest(t *testing.T, coord *Coordinator, txHash common.Hash, blockNumber uint64) *parquet.ReceiptResult {
	t.Helper()

	resp := make(chan readReceiptResp, 1)
	coord.handleReadByTxHashInBlock(readByTxHashInBlockReq{
		ctx:         context.Background(),
		txHash:      txHash,
		blockNumber: blockNumber,
		resp:        resp,
	})
	result := <-resp
	require.NoError(t, result.err)
	quiesceWorkersForTest(coord)
	return result.result
}

type recordingWAL struct {
	entries         []parquet.WALEntry
	firstOffset     uint64
	lastOffset      uint64
	truncatedBefore []uint64
}

func (w *recordingWAL) Write(entry parquet.WALEntry) error {
	if w.firstOffset == 0 {
		w.firstOffset = 1
	}
	w.lastOffset++
	w.entries = append(w.entries, entry)
	return nil
}

func (w *recordingWAL) TruncateBefore(offset uint64) error {
	w.truncatedBefore = append(w.truncatedBefore, offset)
	return nil
}

func (w *recordingWAL) TruncateAfter(uint64) error { return nil }

func (w *recordingWAL) ReadAt(uint64) (parquet.WALEntry, error) { return parquet.WALEntry{}, nil }

func (w *recordingWAL) FirstOffset() (uint64, error) { return w.firstOffset, nil }

func (w *recordingWAL) LastOffset() (uint64, error) { return w.lastOffset, nil }

func (w *recordingWAL) Replay(firstOffset, lastOffset uint64, fn func(uint64, parquet.WALEntry) error) error {
	for i, entry := range w.entries {
		offset := uint64(i) + 1
		if offset < firstOffset || offset > lastOffset {
			continue
		}
		if err := fn(offset, entry); err != nil {
			return err
		}
	}
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

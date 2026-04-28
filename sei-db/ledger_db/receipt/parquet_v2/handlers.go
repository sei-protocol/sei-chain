package parquet_v2

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/common"
	parquetgo "github.com/parquet-go/parquet-go"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
)

func (c *coordinator) handleWrite(req writeReq) {
	req.resp <- writeResp{err: c.writeReceipts(req.inputs)}
}

func (c *coordinator) handleReadByTxHash(req readByTxHashReq) {
	_ = c
	req.resp <- readReceiptResp{err: ErrNotImplemented}
}

func (c *coordinator) handleReadByTxHashInBlock(req readByTxHashInBlockReq) {
	_ = c
	req.resp <- readReceiptResp{err: ErrNotImplemented}
}

func (c *coordinator) handleGetLogs(req getLogsReq) {
	_ = c
	req.resp <- getLogsResp{err: ErrNotImplemented}
}

func (c *coordinator) handleObserveEmptyBlock(req observeEmptyBlockReq) {
	_ = c
	req.resp <- ErrNotImplemented
}

func (c *coordinator) handleFlush(req flushReq) {
	req.resp <- c.flushOpenFile()
}

func (c *coordinator) handleLatestVersion(req latestVersionReq) {
	req.resp <- c.latestVersion
}

func (c *coordinator) handleSetLatestVersion(req setLatestVersionReq) {
	c.latestVersion = req.version
	req.resp <- nil
}

func (c *coordinator) handleSetEarliestVersion(req setEarliestVersionReq) {
	c.earliestVersion = req.version
	req.resp <- nil
}

func (c *coordinator) handleUpdateLatestVersion(req updateLatestVersionReq) {
	if req.version > c.latestVersion {
		c.latestVersion = req.version
	}
	req.resp <- nil
}

func (c *coordinator) handleCacheRotateInterval(req cacheRotateIntervalReq) {
	req.resp <- c.config.MaxBlocksPerFile
}

func (c *coordinator) handleFileStartBlock(req fileStartBlockReq) {
	req.resp <- c.fileStartBlock
}

func (c *coordinator) handleIsRotationBoundary(req isRotationBoundaryReq) {
	if c.config.MaxBlocksPerFile == 0 {
		req.resp <- false
		return
	}
	req.resp <- req.blockNumber%c.config.MaxBlocksPerFile == 0
}

func (c *coordinator) handleSetBlockFlushInterval(req setBlockFlushIntervalReq) {
	c.config.BlockFlushInterval = req.interval
	req.resp <- nil
}

func (c *coordinator) handleSetMaxBlocksPerFile(req setMaxBlocksPerFileReq) {
	c.config.MaxBlocksPerFile = req.maxBlocksPerFile
	if c.reader != nil {
		c.reader.setMaxBlocksPerFile(req.maxBlocksPerFile)
	}
	req.resp <- nil
}

func (c *coordinator) handleSetFaultHooks(req setFaultHooksReq) {
	c.faultHooks = req.hooks
	req.resp <- nil
}

func (c *coordinator) handleReplayWAL(req replayWALReq) {
	_ = c
	req.resp <- replayWALResp{err: ErrNotImplemented}
}

func (c *coordinator) handlePruneTick() {
	_ = c
}

func (c *coordinator) handleClose(req closeReq) {
	c.stopPruneTicker()
	if err := c.flushOpenFile(); err != nil {
		req.resp <- err
		return
	}
	if err := c.closeWriters(); err != nil {
		req.resp <- err
		return
	}
	if c.wal != nil {
		if err := c.wal.Close(); err != nil {
			req.resp <- err
			return
		}
		c.wal = nil
	}
	if c.reader != nil {
		if err := c.reader.Close(); err != nil {
			req.resp <- err
			return
		}
		c.reader = nil
	}
	req.resp <- nil
}

func (c *coordinator) handleSimulateCrash(req simulateCrashReq) {
	c.stopPruneTicker()
	if c.receiptFile != nil {
		_ = c.receiptFile.Close()
		c.receiptFile = nil
	}
	if c.logFile != nil {
		_ = c.logFile.Close()
		c.logFile = nil
	}
	c.receiptWriter = nil
	c.logWriter = nil
	if c.wal != nil {
		_ = c.wal.Close()
	}
	if c.reader != nil {
		_ = c.reader.Close()
	}
	req.resp <- struct{}{}
}

func (c *coordinator) writeReceipts(inputs []parquet.ReceiptInput) error {
	if len(inputs) == 0 {
		return nil
	}
	if c.wal == nil {
		return fmt.Errorf("parquet WAL is not initialized")
	}

	type blockBatch struct {
		blockNumber uint64
		receipts    [][]byte
		inputs      []parquet.ReceiptInput
	}
	var batches []blockBatch
	batchIdx := make(map[uint64]int)

	for i := range inputs {
		bn := inputs[i].BlockNumber
		if idx, ok := batchIdx[bn]; ok {
			batches[idx].receipts = append(batches[idx].receipts, inputs[i].ReceiptBytes)
			batches[idx].inputs = append(batches[idx].inputs, inputs[i])
			continue
		}
		batchIdx[bn] = len(batches)
		batches = append(batches, blockBatch{
			blockNumber: bn,
			receipts:    [][]byte{inputs[i].ReceiptBytes},
			inputs:      []parquet.ReceiptInput{inputs[i]},
		})
	}

	maxBlock := inputs[0].BlockNumber
	for _, b := range batches {
		entry := parquet.WALEntry{
			BlockNumber: b.blockNumber,
			Receipts:    b.receipts,
		}
		if err := c.wal.Write(entry); err != nil {
			return err
		}

		if h := c.faultHooks; h != nil && h.AfterWALWrite != nil {
			if err := h.AfterWALWrite(b.blockNumber); err != nil {
				return err
			}
		}

		for i := range b.inputs {
			if err := c.applyReceipt(b.inputs[i]); err != nil {
				return err
			}
			if b.inputs[i].BlockNumber > maxBlock {
				maxBlock = b.inputs[i].BlockNumber
			}
		}
	}

	latest, err := int64FromUint64(maxBlock)
	if err != nil {
		return err
	}
	if latest > c.latestVersion {
		c.latestVersion = latest
	}
	return nil
}

func (c *coordinator) applyReceipt(input parquet.ReceiptInput) error {
	if c.receiptWriter == nil {
		aligned := alignedFileStartBlock(input.BlockNumber, c.config.MaxBlocksPerFile)
		if aligned >= c.fileStartBlock {
			c.fileStartBlock = aligned
		}
		if err := c.initWriters(); err != nil {
			return err
		}
	}

	blockNumber := input.BlockNumber
	if blockNumber != c.lastSeenBlock {
		if c.lastSeenBlock != 0 {
			c.blocksSinceFlush++
		}
		c.lastSeenBlock = blockNumber
	}

	c.receiptsBuffer = append(c.receiptsBuffer, input.Receipt)
	if len(input.Logs) > 0 {
		c.logsBuffer = append(c.logsBuffer, input.Logs...)
	}

	txHash := common.BytesToHash(input.Receipt.TxHash)
	c.tempWriteCache[txHash] = append(c.tempWriteCache[txHash], tempReceipt{
		blockNumber:  input.BlockNumber,
		writeOrdinal: c.nextWriteOrdinal,
		receiptBytes: input.ReceiptBytes,
	})
	c.nextWriteOrdinal++

	if c.config.BlockFlushInterval > 0 && c.blocksSinceFlush >= c.config.BlockFlushInterval {
		if err := c.flushOpenFile(); err != nil {
			return err
		}
		c.blocksSinceFlush = 0
	}

	return nil
}

func alignedFileStartBlock(blockNumber, maxBlocksPerFile uint64) uint64 {
	if maxBlocksPerFile == 0 {
		return blockNumber
	}
	return (blockNumber / maxBlocksPerFile) * maxBlocksPerFile
}

func (c *coordinator) initWriters() error {
	receiptPath := filepath.Join(c.basePath, fmt.Sprintf("receipts_%d.parquet", c.fileStartBlock))
	logPath := filepath.Join(c.basePath, fmt.Sprintf("logs_%d.parquet", c.fileStartBlock))

	// #nosec G304 -- paths are constructed from configured base directory.
	receiptFile, err := os.Create(receiptPath)
	if err != nil {
		return fmt.Errorf("failed to create receipt parquet file: %w", err)
	}

	// #nosec G304 -- paths are constructed from configured base directory.
	logFile, err := os.Create(logPath)
	if err != nil {
		if closeErr := receiptFile.Close(); closeErr != nil {
			return fmt.Errorf("failed to create log parquet file: %w; close receipt file error: %v", err, closeErr)
		}
		return fmt.Errorf("failed to create log parquet file: %w", err)
	}

	blockNumberSorting := parquetgo.SortingWriterConfig(
		parquetgo.SortingColumns(parquetgo.Ascending("block_number")),
	)

	c.receiptFile = receiptFile
	c.logFile = logFile
	c.receiptWriter = parquetgo.NewGenericWriter[parquet.ReceiptRecord](receiptFile,
		parquetgo.Compression(&parquetgo.Snappy),
		blockNumberSorting,
	)
	c.logWriter = parquetgo.NewGenericWriter[parquet.LogRecord](logFile,
		parquetgo.Compression(&parquetgo.Snappy),
		blockNumberSorting,
	)

	return nil
}

func (c *coordinator) flushOpenFile() error {
	if len(c.receiptsBuffer) == 0 {
		return nil
	}
	if c.receiptWriter == nil {
		return fmt.Errorf("cannot flush receipts: receipt writer is not initialized")
	}

	if h := c.faultHooks; h != nil && h.BeforeFlush != nil {
		if err := h.BeforeFlush(c.lastSeenBlock); err != nil {
			return err
		}
	}

	if _, err := c.receiptWriter.Write(c.receiptsBuffer); err != nil {
		return fmt.Errorf("failed to write receipts to parquet: %w", err)
	}
	if err := c.receiptWriter.Flush(); err != nil {
		return fmt.Errorf("failed to flush receipt parquet writer: %w", err)
	}

	if len(c.logsBuffer) > 0 {
		if c.logWriter == nil {
			return fmt.Errorf("cannot flush logs: log writer is not initialized")
		}
		if _, err := c.logWriter.Write(c.logsBuffer); err != nil {
			return fmt.Errorf("failed to write logs to parquet: %w", err)
		}
		if err := c.logWriter.Flush(); err != nil {
			return fmt.Errorf("failed to flush log parquet writer: %w", err)
		}
	}

	if h := c.faultHooks; h != nil && h.AfterFlush != nil {
		if err := h.AfterFlush(c.lastSeenBlock); err != nil {
			return err
		}
	}

	c.receiptsBuffer = c.receiptsBuffer[:0]
	c.logsBuffer = c.logsBuffer[:0]
	return nil
}

func (c *coordinator) closeWriters() error {
	var errs []error

	if c.receiptWriter != nil {
		if err := c.receiptWriter.Close(); err != nil {
			errs = append(errs, fmt.Errorf("receipt writer: %w", err))
		}
		c.receiptWriter = nil
	}
	if c.logWriter != nil {
		if err := c.logWriter.Close(); err != nil {
			errs = append(errs, fmt.Errorf("log writer: %w", err))
		}
		c.logWriter = nil
	}
	if c.receiptFile != nil {
		if err := c.receiptFile.Sync(); err != nil {
			errs = append(errs, fmt.Errorf("receipt file sync: %w", err))
		}
		if err := c.receiptFile.Close(); err != nil {
			errs = append(errs, fmt.Errorf("receipt file: %w", err))
		}
		c.receiptFile = nil
	}
	if c.logFile != nil {
		if err := c.logFile.Sync(); err != nil {
			errs = append(errs, fmt.Errorf("log file sync: %w", err))
		}
		if err := c.logFile.Close(); err != nil {
			errs = append(errs, fmt.Errorf("log file: %w", err))
		}
		c.logFile = nil
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}
	return nil
}

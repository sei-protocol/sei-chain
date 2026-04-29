package coordinator

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/common"
	parquetgo "github.com/parquet-go/parquet-go"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
)

func (c *Coordinator) handleWrite(req writeReq) {
	req.resp <- writeResp{err: c.writeReceipts(req.inputs)}
}

func (c *Coordinator) handleReadByTxHash(req readByTxHashReq) {
	if result := c.cachedReceiptByTxHash(req.txHash); result != nil {
		req.resp <- readReceiptResp{result: result}
		return
	}
	if c.reader == nil {
		req.resp <- readReceiptResp{err: fmt.Errorf("parquet reader is not initialized")}
		return
	}

	result, err := c.reader.QueryReceiptByTxHash(req.ctx, c.receiptFilesSnapshot(), req.txHash)
	req.resp <- readReceiptResp{result: result, err: err}
}

func (c *Coordinator) handleReadByTxHashInBlock(req readByTxHashInBlockReq) {
	if result := c.cachedReceiptByTxHashInBlock(req.txHash, req.blockNumber); result != nil {
		req.resp <- readReceiptResp{result: result}
		return
	}
	if c.reader == nil {
		req.resp <- readReceiptResp{err: fmt.Errorf("parquet reader is not initialized")}
		return
	}

	result, err := c.reader.QueryReceiptByTxHashInBlock(req.ctx, c.receiptFileSnapshotForBlock(req.blockNumber), req.txHash, req.blockNumber)
	req.resp <- readReceiptResp{result: result, err: err}
}

func (c *Coordinator) handleGetLogs(req getLogsReq) {
	if c.reader == nil {
		req.resp <- getLogsResp{err: fmt.Errorf("parquet reader is not initialized")}
		return
	}

	results, err := c.reader.QueryLogs(req.ctx, c.logFilesSnapshot(), req.filter)
	req.resp <- getLogsResp{results: results, err: err}
}

func (c *Coordinator) handleObserveEmptyBlock(req observeEmptyBlockReq) {
	req.resp <- c.observeEmptyBlock(req.height)
}

func (c *Coordinator) handleFlush(req flushReq) {
	req.resp <- c.flushOpenFile()
}

func (c *Coordinator) handleLatestVersion(req latestVersionReq) {
	req.resp <- c.latestVersion
}

func (c *Coordinator) handleSetLatestVersion(req setLatestVersionReq) {
	c.latestVersion = req.version
	req.resp <- nil
}

func (c *Coordinator) handleSetEarliestVersion(req setEarliestVersionReq) {
	c.earliestVersion = req.version
	req.resp <- nil
}

func (c *Coordinator) handleUpdateLatestVersion(req updateLatestVersionReq) {
	if req.version > c.latestVersion {
		c.latestVersion = req.version
	}
	req.resp <- nil
}

func (c *Coordinator) handleCacheRotateInterval(req cacheRotateIntervalReq) {
	req.resp <- c.config.MaxBlocksPerFile
}

func (c *Coordinator) handleFileStartBlock(req fileStartBlockReq) {
	req.resp <- c.fileStartBlock
}

func (c *Coordinator) handleIsRotationBoundary(req isRotationBoundaryReq) {
	req.resp <- c.isRotationBoundary(req.blockNumber)
}

func (c *Coordinator) handleSetBlockFlushInterval(req setBlockFlushIntervalReq) {
	c.config.BlockFlushInterval = req.interval
	req.resp <- nil
}

func (c *Coordinator) handleSetMaxBlocksPerFile(req setMaxBlocksPerFileReq) {
	c.config.MaxBlocksPerFile = req.maxBlocksPerFile
	if c.reader != nil {
		c.reader.setMaxBlocksPerFile(req.maxBlocksPerFile)
	}
	req.resp <- nil
}

func (c *Coordinator) handleSetFaultHooks(req setFaultHooksReq) {
	c.faultHooks = req.hooks
	req.resp <- nil
}

func (c *Coordinator) handleReplayWAL(req replayWALReq) {
	result, err := c.replayWAL(req.converter)
	req.resp <- replayWALResp{result: result, err: err}
}

func (c *Coordinator) handlePruneTick() {
	// TODO(future-async): if read I/O moves to a worker pool, gate prune on
	// map[fileID]int reference counts that the coordinator increments on
	// dispatch and decrements on completion.
	if c.config.KeepRecent <= 0 {
		return
	}
	pruneBeforeBlock := c.latestVersion - c.config.KeepRecent
	if pruneBeforeBlock <= 0 {
		return
	}
	c.pruneOldFiles(uint64(pruneBeforeBlock))
}

func (c *Coordinator) handleClose(req closeReq) {
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

func (c *Coordinator) handleSimulateCrash(req simulateCrashReq) {
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

func (c *Coordinator) writeReceipts(inputs []parquet.ReceiptInput) error {
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

		if c.receiptWriter != nil && b.blockNumber != c.lastSeenBlock && c.isRotationBoundary(b.blockNumber) {
			if err := c.rotateOpenFile(b.blockNumber); err != nil {
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

func (c *Coordinator) applyReceipt(input parquet.ReceiptInput) error {
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

func (c *Coordinator) cachedReceiptByTxHash(txHash common.Hash) *parquet.ReceiptResult {
	entries := c.tempWriteCache[txHash]
	if len(entries) == 0 {
		return nil
	}
	return receiptResultFromTemp(txHash, entries[0])
}

func (c *Coordinator) cachedReceiptByTxHashInBlock(txHash common.Hash, blockNumber uint64) *parquet.ReceiptResult {
	for _, entry := range c.tempWriteCache[txHash] {
		if entry.blockNumber == blockNumber {
			return receiptResultFromTemp(txHash, entry)
		}
	}
	return nil
}

func receiptResultFromTemp(txHash common.Hash, entry tempReceipt) *parquet.ReceiptResult {
	return &parquet.ReceiptResult{
		TxHash:       append([]byte(nil), txHash[:]...),
		BlockNumber:  entry.blockNumber,
		ReceiptBytes: append([]byte(nil), entry.receiptBytes...),
	}
}

func (c *Coordinator) receiptFilesSnapshot() []string {
	files := make([]string, 0, len(c.closedFiles))
	for _, f := range c.closedFiles {
		files = append(files, f.receiptPath)
	}
	return files
}

func (c *Coordinator) receiptFileSnapshotForBlock(blockNumber uint64) []string {
	var best string
	for _, f := range c.closedFiles {
		if f.startBlock > blockNumber {
			break
		}
		best = f.receiptPath
	}
	if best == "" {
		return nil
	}
	return []string{best}
}

func (c *Coordinator) logFilesSnapshot() []string {
	files := make([]string, 0, len(c.closedFiles))
	for _, f := range c.closedFiles {
		files = append(files, f.logPath)
	}
	return files
}

func (c *Coordinator) isRotationBoundary(blockNumber uint64) bool {
	return c.config.MaxBlocksPerFile > 0 && blockNumber%c.config.MaxBlocksPerFile == 0
}

func alignedFileStartBlock(blockNumber, maxBlocksPerFile uint64) uint64 {
	if maxBlocksPerFile == 0 {
		return blockNumber
	}
	return (blockNumber / maxBlocksPerFile) * maxBlocksPerFile
}

func (c *Coordinator) initWriters() error {
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

func (c *Coordinator) rotateOpenFile(newBlockNumber uint64) error {
	if err := c.rotateOpenFileWithoutWAL(newBlockNumber); err != nil {
		return err
	}
	if err := c.clearWALPreservingLast(); err != nil {
		return err
	}
	if h := c.faultHooks; h != nil && h.AfterWALClear != nil {
		if err := h.AfterWALClear(newBlockNumber); err != nil {
			return err
		}
	}
	c.dropTempCacheBefore(c.fileStartBlock)
	return nil
}

func (c *Coordinator) rotateOpenFileWithoutWAL(newBlockNumber uint64) error {
	if c.receiptWriter == nil {
		return nil
	}
	if err := c.flushOpenFile(); err != nil {
		return err
	}

	oldStartBlock := c.fileStartBlock
	oldReceiptPath := filepath.Join(c.basePath, fmt.Sprintf("receipts_%d.parquet", oldStartBlock))
	oldLogPath := filepath.Join(c.basePath, fmt.Sprintf("logs_%d.parquet", oldStartBlock))

	if err := c.closeWriters(); err != nil {
		return err
	}

	if h := c.faultHooks; h != nil && h.AfterCloseWriters != nil {
		if err := h.AfterCloseWriters(newBlockNumber); err != nil {
			return err
		}
	}

	c.closedFiles = append(c.closedFiles, closedFile{
		startBlock:  oldStartBlock,
		receiptPath: oldReceiptPath,
		logPath:     oldLogPath,
	})
	c.fileStartBlock = newBlockNumber
	if err := c.initWriters(); err != nil {
		return err
	}
	c.blocksSinceFlush = 0
	return nil
}

func (c *Coordinator) dropTempCacheBefore(blockNumber uint64) {
	for txHash, entries := range c.tempWriteCache {
		kept := entries[:0]
		for _, entry := range entries {
			if entry.blockNumber >= blockNumber {
				kept = append(kept, entry)
			}
		}
		if len(kept) == 0 {
			delete(c.tempWriteCache, txHash)
			continue
		}
		c.tempWriteCache[txHash] = kept
	}
}

func (c *Coordinator) observeEmptyBlock(height uint64) error {
	if height <= c.lastSeenBlock {
		return nil
	}
	if c.receiptWriter == nil || !c.isRotationBoundary(height) {
		c.lastSeenBlock = height
		return nil
	}
	if err := c.rotateOpenFile(height); err != nil {
		return err
	}
	c.lastSeenBlock = height
	return nil
}

func (c *Coordinator) flushOpenFile() error {
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

func (c *Coordinator) closeWriters() error {
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

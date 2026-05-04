package coordinator

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/common"
	parquetgo "github.com/parquet-go/parquet-go"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
)

// handleWrite serves a writeReq by appending receipts for a single block and
// replying with any error encountered during WAL append, rotation, or buffer
// staging.
func (c *Coordinator) handleWrite(req writeReq) {
	req.resp <- writeResp{err: c.writeReceipts(req.height, req.inputs)}
}

// handleReadByTxHash serves a readByTxHashReq by checking the in-memory write
// cache first, then falling back to a DuckDB query over closed parquet files.
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

// handleReadByTxHashInBlock serves a readByTxHashInBlockReq, narrowing the
// parquet file scan to the single closed file that contains the requested
// block (if any).
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

// handleGetLogs serves a getLogsReq by querying logs across the closed log
// parquet files using the supplied filter.
func (c *Coordinator) handleGetLogs(req getLogsReq) {
	if c.reader == nil {
		req.resp <- getLogsResp{err: fmt.Errorf("parquet reader is not initialized")}
		return
	}

	results, err := c.reader.QueryLogs(req.ctx, c.logFilesSnapshot(), req.filter)
	req.resp <- getLogsResp{results: results, err: err}
}

// handleFlush serves a flushReq by flushing buffered receipts/logs for the
// open parquet file to disk.
func (c *Coordinator) handleFlush(req flushReq) {
	req.resp <- c.flushOpenFile()
}

// handleLatestVersion returns the highest block height the coordinator has
// observed via WriteReceipts or WAL replay.
func (c *Coordinator) handleLatestVersion(req latestVersionReq) {
	req.resp <- c.latestVersion
}

// handleSetLatestVersion overwrites latestVersion. Used by callers that
// authoritatively know the chain height (e.g., genesis/init paths).
func (c *Coordinator) handleSetLatestVersion(req setLatestVersionReq) {
	c.latestVersion = req.version
	req.resp <- nil
}

// handleSetEarliestVersion records the lowest retained block height. Pruning
// uses this as a hint about the visible window.
func (c *Coordinator) handleSetEarliestVersion(req setEarliestVersionReq) {
	c.earliestVersion = req.version
	req.resp <- nil
}

// handleUpdateLatestVersion advances latestVersion only when the supplied
// value is greater, preventing accidental rewinds.
func (c *Coordinator) handleUpdateLatestVersion(req updateLatestVersionReq) {
	if req.version > c.latestVersion {
		c.latestVersion = req.version
	}
	req.resp <- nil
}

// handleFileStartBlock returns the start block of the currently open parquet
// file (the next file's name will derive from this).
func (c *Coordinator) handleFileStartBlock(req fileStartBlockReq) {
	req.resp <- c.fileStartBlock
}

// handleSetBlockFlushInterval updates how often (in blocks) the buffered
// receipt/log writer is flushed to disk.
func (c *Coordinator) handleSetBlockFlushInterval(req setBlockFlushIntervalReq) {
	c.config.BlockFlushInterval = req.interval
	req.resp <- nil
}

// handleSetMaxBlocksPerFile updates the rotation interval and propagates it
// to the reader so log-file pruning by block range stays consistent.
func (c *Coordinator) handleSetMaxBlocksPerFile(req setMaxBlocksPerFileReq) {
	c.config.MaxBlocksPerFile = req.maxBlocksPerFile
	if c.reader != nil {
		c.reader.setMaxBlocksPerFile(req.maxBlocksPerFile)
	}
	req.resp <- nil
}

// handleSetFaultHooks installs the supplied test hooks. In production the
// hooks pointer is nil and all hook checks become no-ops.
func (c *Coordinator) handleSetFaultHooks(req setFaultHooksReq) {
	c.faultHooks = req.hooks
	req.resp <- nil
}

// handlePruneTick fires on the prune ticker and removes closed parquet pairs
// whose end block falls below latestVersion - KeepRecent.
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

// handleClose performs a graceful shutdown: flush and close the open writers,
// then close the WAL and reader. Returns the first non-nil error encountered
// along the way. The prune ticker is stopped via defer in run().
func (c *Coordinator) handleClose(req closeReq) {
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

// handleSimulateCrash drops in-memory writer state without flushing — the
// open parquet files remain truncated/partial on disk so subsequent recovery
// paths can be exercised. Test-only.
func (c *Coordinator) handleSimulateCrash(req simulateCrashReq) {
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

// writeReceipts records a committed block at height. When inputs is empty it
// degenerates to the rotation/cursor-advance path (formerly ObserveEmptyBlock):
// no WAL entry is written, but if height lands on a rotation boundary the
// open file is rotated so it never spans more than MaxBlocksPerFile blocks.
// height is authoritative; inputs[i].BlockNumber is ignored.
func (c *Coordinator) writeReceipts(height uint64, inputs []parquet.ReceiptInput) error {
	if len(inputs) == 0 {
		return c.observeBlock(height)
	}
	if c.wal == nil {
		return fmt.Errorf("parquet WAL is not initialized")
	}

	receiptBytes := make([][]byte, len(inputs))
	for i := range inputs {
		receiptBytes[i] = inputs[i].ReceiptBytes
	}
	if err := c.wal.Write(parquet.WALEntry{BlockNumber: height, Receipts: receiptBytes}); err != nil {
		return err
	}

	if h := c.faultHooks; h != nil && h.AfterWALWrite != nil {
		if err := h.AfterWALWrite(height); err != nil {
			return err
		}
	}

	if c.receiptWriter != nil && height != c.lastSeenBlock && c.isRotationBoundary(height) {
		if err := c.rotateOpenFile(height); err != nil {
			return err
		}
	}

	for i := range inputs {
		if err := c.applyReceipt(height, inputs[i]); err != nil {
			return err
		}
	}

	latest, err := int64FromUint64(height)
	if err != nil {
		return err
	}
	if latest > c.latestVersion {
		c.latestVersion = latest
	}
	return nil
}

// applyReceipt stages a single receipt into the open parquet writer's
// in-memory buffers and the temp write cache, lazily creating writers if this
// is the first receipt for the current file. Triggers a flush when
// blocksSinceFlush has reached BlockFlushInterval.
func (c *Coordinator) applyReceipt(blockNumber uint64, input parquet.ReceiptInput) error {
	if c.receiptWriter == nil {
		aligned := alignedFileStartBlock(blockNumber, c.config.MaxBlocksPerFile)
		if aligned >= c.fileStartBlock {
			c.fileStartBlock = aligned
		}
		if err := c.initWriters(); err != nil {
			return err
		}
	}

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
		blockNumber:  blockNumber,
		receiptBytes: input.ReceiptBytes,
	})

	if c.config.BlockFlushInterval > 0 && c.blocksSinceFlush >= c.config.BlockFlushInterval {
		if err := c.flushOpenFile(); err != nil {
			return err
		}
		c.blocksSinceFlush = 0
	}

	return nil
}

// cachedReceiptByTxHash returns the earliest cached receipt for txHash, or
// nil if the temp write cache has no entry. Used to serve reads for receipts
// that are still buffered and not yet flushed to a closed parquet file.
func (c *Coordinator) cachedReceiptByTxHash(txHash common.Hash) *parquet.ReceiptResult {
	entries := c.tempWriteCache[txHash]
	if len(entries) == 0 {
		return nil
	}
	return receiptResultFromTemp(txHash, entries[0])
}

// cachedReceiptByTxHashInBlock returns the cached receipt for txHash at the
// given block, or nil if the temp write cache has no matching entry.
func (c *Coordinator) cachedReceiptByTxHashInBlock(txHash common.Hash, blockNumber uint64) *parquet.ReceiptResult {
	for _, entry := range c.tempWriteCache[txHash] {
		if entry.blockNumber == blockNumber {
			return receiptResultFromTemp(txHash, entry)
		}
	}
	return nil
}

// receiptResultFromTemp converts a tempReceipt cache entry into the public
// ReceiptResult shape, copying byte slices to decouple from cache storage.
func receiptResultFromTemp(txHash common.Hash, entry tempReceipt) *parquet.ReceiptResult {
	return &parquet.ReceiptResult{
		TxHash:       append([]byte(nil), txHash[:]...),
		BlockNumber:  entry.blockNumber,
		ReceiptBytes: append([]byte(nil), entry.receiptBytes...),
	}
}

// receiptFilesSnapshot returns the receipt parquet paths for all closed
// files. Reads use this list as the file set for full-range queries.
func (c *Coordinator) receiptFilesSnapshot() []string {
	files := make([]string, 0, len(c.closedFiles))
	for _, f := range c.closedFiles {
		files = append(files, f.receiptPath)
	}
	return files
}

// receiptFileSnapshotForBlock returns the single closed receipt file whose
// start block is the largest one not exceeding blockNumber, or nil if no
// such file exists. Used to narrow point lookups by block.
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

// logFilesSnapshot returns the log parquet paths for all closed files. Log
// queries use this list as the file set, which the Reader further narrows
// by from/to-block range.
func (c *Coordinator) logFilesSnapshot() []string {
	files := make([]string, 0, len(c.closedFiles))
	for _, f := range c.closedFiles {
		files = append(files, f.logPath)
	}
	return files
}

// isRotationBoundary reports whether blockNumber lands on a MaxBlocksPerFile
// boundary, in which case the open parquet file should rotate before this
// block's receipts are written.
func (c *Coordinator) isRotationBoundary(blockNumber uint64) bool {
	return c.config.MaxBlocksPerFile > 0 && blockNumber%c.config.MaxBlocksPerFile == 0
}

// alignedFileStartBlock floors blockNumber to the nearest multiple of
// maxBlocksPerFile, used to derive a parquet file's start-block name.
func alignedFileStartBlock(blockNumber, maxBlocksPerFile uint64) uint64 {
	if maxBlocksPerFile == 0 {
		return blockNumber
	}
	return (blockNumber / maxBlocksPerFile) * maxBlocksPerFile
}

// initWriters creates the receipt and log parquet files for the current
// fileStartBlock and constructs sorted parquet writers over them. If the log
// file fails to open, the receipt file is closed before returning.
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

// rotateOpenFile closes the current parquet file pair, opens a new one
// starting at newBlockNumber, truncates the WAL up to (but preserving) the
// most recent entry, and drops cached entries that are now durably stored.
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

// rotateOpenFileWithoutWAL performs the file-side rotation steps (flush,
// close, register closed pair, open new pair) without touching the WAL.
// Used during replay where the WAL drives rotation timing externally.
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

// dropTempCacheBefore evicts temp-cache entries for blocks below
// blockNumber, freeing memory once those receipts are durably persisted in
// a closed parquet file.
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

// observeBlock advances the cursor for a committed block without writing to
// the WAL. Called by writeReceipts when inputs is empty. Out-of-order
// observations must not move the cursor backward — WriteReceipts could
// otherwise mis-handle rotation for a height already seen.
func (c *Coordinator) observeBlock(height uint64) error {
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

// flushOpenFile drains the in-memory receipt and log buffers into the open
// parquet writers and forces them to disk. No-op when nothing is buffered.
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

// closeWriters finalizes the parquet writers (writing the trailer/footer)
// and fsync+closes the underlying files. All errors encountered are
// collected and returned together so partial cleanup still happens.
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

package coordinator

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

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

	snapshot := c.receiptFilesSnapshot()
	if len(snapshot) == 0 {
		req.resp <- readReceiptResp{}
		return
	}
	c.acquireReadRefs(snapshot)
	reader := c.reader
	control := c.controlChan
	c.dispatchRead(func() {
		result, err := reader.QueryReceiptByTxHash(req.ctx, snapshot, req.txHash)
		req.resp <- readReceiptResp{result: result, err: err}
		control <- readDoneMsg{paths: snapshot}
	})
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

	snapshot := c.receiptFileSnapshotForBlock(req.blockNumber)
	if len(snapshot) == 0 {
		req.resp <- readReceiptResp{}
		return
	}
	c.acquireReadRefs(snapshot)
	reader := c.reader
	control := c.controlChan
	c.dispatchRead(func() {
		result, err := reader.QueryReceiptByTxHashInBlock(req.ctx, snapshot, req.txHash, req.blockNumber)
		req.resp <- readReceiptResp{result: result, err: err}
		control <- readDoneMsg{paths: snapshot}
	})
}

// handleGetLogs serves a getLogsReq by querying logs across the closed log
// parquet files using the supplied filter.
func (c *Coordinator) handleGetLogs(req getLogsReq) {
	if c.reader == nil {
		req.resp <- getLogsResp{err: fmt.Errorf("parquet reader is not initialized")}
		return
	}

	snapshot := c.filteredLogFilesSnapshot(req.filter)
	if len(snapshot) == 0 {
		req.resp <- getLogsResp{}
		return
	}
	c.acquireReadRefs(snapshot)
	reader := c.reader
	control := c.controlChan
	c.dispatchRead(func() {
		results, err := reader.QueryLogs(req.ctx, snapshot, req.filter)
		req.resp <- getLogsResp{results: results, err: err}
		control <- readDoneMsg{paths: snapshot}
	})
}

// acquireReadRefs increments inFlightReads for each path in the snapshot.
// Must run on the coordinator goroutine — it mutates coordinator-owned
// state that workers never touch.
func (c *Coordinator) acquireReadRefs(paths []string) {
	for _, p := range paths {
		c.inFlightReads[p]++
	}
}

// releaseReadRefs decrements inFlightReads and dispatches any deferred
// prunes whose files have just dropped to zero. Must run on the
// coordinator goroutine.
func (c *Coordinator) releaseReadRefs(paths []string) {
	for _, p := range paths {
		n := c.inFlightReads[p]
		if n <= 1 {
			delete(c.inFlightReads, p)
		} else {
			c.inFlightReads[p] = n - 1
		}
	}
	c.flushPendingPrune()
}

// flushPendingPrune dispatches prune jobs for any pendingPrune entries
// whose receipt and log paths both have zero refcount. Must run on the
// coordinator goroutine.
func (c *Coordinator) flushPendingPrune() {
	if len(c.pendingPrune) == 0 {
		return
	}
	kept := c.pendingPrune[:0]
	for _, f := range c.pendingPrune {
		if c.inFlightReads[f.receiptPath] > 0 || c.inFlightReads[f.logPath] > 0 {
			kept = append(kept, f)
			continue
		}
		c.dispatchPruneJob(f)
	}
	c.pendingPrune = kept
}

// handleControl processes a worker completion message. Always runs on
// the coordinator goroutine.
func (c *Coordinator) handleControl(msg controlMsg) {
	switch m := msg.(type) {
	case readDoneMsg:
		c.releaseReadRefs(m.paths)
	case pruneDoneMsg:
		if !m.ok {
			c.reinsertFailedPrune(m.paths)
		}
	case writerDoneMsg:
		// Reserved for future async writer paths; awaitWriter uses its
		// own per-call result channel.
	}
}

// reinsertFailedPrune re-adds a file to closedFiles after a prune lambda
// failed to delete it from disk. Best-effort — preserves the prior
// behavior of pruneOldFiles which kept the entry on remove failure.
// Inserts in sorted position by startBlock because receiptFileSnapshotForBlock
// relies on closedFiles being ascending to early-break correctly.
func (c *Coordinator) reinsertFailedPrune(paths []string) {
	if len(paths) != 2 {
		return
	}
	receiptPath, logPath := paths[0], paths[1]
	startBlock := parquet.ExtractBlockNumber(receiptPath)
	entry := closedFile{
		startBlock:  startBlock,
		receiptPath: receiptPath,
		logPath:     logPath,
	}
	idx := sort.Search(len(c.closedFiles), func(i int) bool {
		return c.closedFiles[i].startBlock >= startBlock
	})
	c.closedFiles = append(c.closedFiles, closedFile{})
	copy(c.closedFiles[idx+1:], c.closedFiles[idx:])
	c.closedFiles[idx] = entry
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
// to the reader so log-file pruning by block range stays consistent. Mirrors
// the new value into cacheRotateInterval so external readers see it without
// going through the request channel.
func (c *Coordinator) handleSetMaxBlocksPerFile(req setMaxBlocksPerFileReq) {
	if req.maxBlocksPerFile == 0 {
		req.resp <- fmt.Errorf("max blocks per file must be greater than 0")
		return
	}
	c.config.MaxBlocksPerFile = req.maxBlocksPerFile
	c.cacheRotateInterval.Store(req.maxBlocksPerFile)
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
	// Eligibility walk: for each closedFile that should age out, remove
	// from closedFiles (so no future snapshot can include it) and either
	// dispatch a prune job immediately (refcount==0) or defer it via
	// pendingPrune (refcount>0). The deferred prune fires from
	// flushPendingPrune when readDoneMsg drives the refcount to zero.
	if c.config.KeepRecent <= 0 {
		return
	}
	pruneBeforeBlock := c.latestVersion - c.config.KeepRecent
	if pruneBeforeBlock <= 0 {
		return
	}
	c.pruneEligibleFiles(uint64(pruneBeforeBlock))
}

// pruneEligibleFiles walks closedFiles and removes any file whose chunk
// has fully aged out. Each removed file is either dispatched to the
// pruner immediately or deferred to pendingPrune if there are active
// readers.
func (c *Coordinator) pruneEligibleFiles(pruneBeforeBlock uint64) {
	if len(c.closedFiles) == 0 {
		return
	}
	kept := c.closedFiles[:0]
	for _, f := range c.closedFiles {
		if !c.shouldPruneClosedFile(f, pruneBeforeBlock) {
			kept = append(kept, f)
			continue
		}
		if c.inFlightReads[f.receiptPath] > 0 || c.inFlightReads[f.logPath] > 0 {
			c.pendingPrune = append(c.pendingPrune, f)
			continue
		}
		c.dispatchPruneJob(f)
	}
	c.closedFiles = kept
}

// dispatchPruneJob hands a closure to the pruner that deletes both the
// receipt and log file for f, then reports back via controlChan. Must
// run on the coordinator goroutine.
func (c *Coordinator) dispatchPruneJob(f closedFile) {
	receiptPath := f.receiptPath
	logPath := f.logPath
	control := c.controlChan
	c.dispatchPrune(func() {
		ok := removePrunedFile(receiptPath) && removePrunedFile(logPath)
		control <- pruneDoneMsg{paths: []string{receiptPath, logPath}, ok: ok}
	})
}

// handleClose performs a graceful shutdown: flush and close the open writers,
// then close the WAL and reader. Each step runs even if an earlier one
// errors so resources (file descriptors, WAL background goroutines, DuckDB
// connections) are always released. Errors from every step are joined and
// returned together. The prune ticker is stopped via defer in run().
func (c *Coordinator) handleClose(req closeReq) {
	var errs []error
	if err := c.flushOpenFile(); err != nil {
		errs = append(errs, fmt.Errorf("flush: %w", err))
	}
	if err := c.closeWriters(); err != nil {
		errs = append(errs, err)
	}
	c.shutdownWorkers()
	if c.wal != nil {
		if err := c.wal.Close(); err != nil {
			errs = append(errs, fmt.Errorf("wal close: %w", err))
		}
		c.wal = nil
	}
	if c.reader != nil {
		if err := c.reader.Close(); err != nil {
			errs = append(errs, fmt.Errorf("reader close: %w", err))
		}
		c.reader = nil
	}
	req.resp <- errors.Join(errs...)
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
	c.shutdownWorkers()
	if c.wal != nil {
		_ = c.wal.Close()
	}
	if c.reader != nil {
		_ = c.reader.Close()
	}
	req.resp <- struct{}{}
}

// shutdownWorkers closes the dispatch channels in dependency order and
// waits for each pool to drain. Coordinator-side state has already been
// updated by the caller; workers only execute remaining queued lambdas
// before exiting. Idempotent via shutdownOnce so retry paths in
// handleClose do not double-close the channels. Tolerates nil channels
// for bare-Coordinator constructions used in unit tests.
//
// IMPORTANT: this runs on the coordinator goroutine — the same goroutine
// that normally drains controlChan in c.run(). Each in-flight reader and
// pruner lambda finishes with a blocking send on controlChan
// (handlers.go readDoneMsg/pruneDoneMsg sites). controlChan is buffered
// to 64, but the reader pool can have up to 2*NumCPU workers and readChan
// can hold up to 1024 queued jobs. If we naively call wg.Wait() here,
// nothing is reading controlChan; once the buffer fills, additional
// workers block on the send forever, never call wg.Done(), and shutdown
// hangs. drainControlUntilDone keeps controlChan moving in parallel with
// the wait so workers can always complete their send and exit.
//
// We deliberately drop the drained messages instead of routing them
// through handleControl: the coordinator state they update (inFlightReads,
// pendingPrune) is about to be discarded, and dispatching a fresh prune
// from a late readDoneMsg would race with the close(c.pruneChan) that
// happens later in this same function.
//
// This deadlock is hard to reproduce in unit tests (existing tests fire
// 4–8 reads, the threshold is 64+ in-flight reads at the moment of
// shutdown), so the fix is documented here rather than guarded by a
// regression test.
func (c *Coordinator) shutdownWorkers() {
	c.shutdownOnce.Do(func() {
		if c.readChan != nil {
			close(c.readChan)
			c.drainControlUntilDone(&c.readerWG)
		}
		if c.writerChan != nil {
			close(c.writerChan)
			c.drainControlUntilDone(&c.writerWG)
		}
		if c.pruneChan != nil {
			close(c.pruneChan)
			c.drainControlUntilDone(&c.prunerWG)
		}
	})
}

// drainControlUntilDone blocks until wg signals done, while concurrently
// receiving (and discarding) anything queued on controlChan. See
// shutdownWorkers for why discarding is correct during teardown. Spawns
// one short-lived goroutine that closes a sentinel when the wait group
// drains; the loop in this function is the only consumer of controlChan
// during shutdown, replacing c.run()'s usual select.
func (c *Coordinator) drainControlUntilDone(wg *sync.WaitGroup) {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	for {
		select {
		case <-done:
			return
		case <-c.controlChan:
			// Drop. Coordinator state is being torn down; refcounts and
			// pendingPrune entries do not need to stay consistent past
			// this point.
		}
	}
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

// filteredLogFilesSnapshot builds a snapshot of log file paths whose
// block range overlaps the filter's [FromBlock, ToBlock] window. Filtering
// happens here on the coordinator so refcounts only get incremented on
// files that workers will actually touch.
func (c *Coordinator) filteredLogFilesSnapshot(filter parquet.LogFilter) []string {
	files := make([]string, 0, len(c.closedFiles))
	for _, f := range c.closedFiles {
		if filter.ToBlock != nil && f.startBlock > *filter.ToBlock {
			continue
		}
		if filter.FromBlock != nil && f.startBlock+c.config.MaxBlocksPerFile <= *filter.FromBlock {
			continue
		}
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

	var (
		receiptFile   *os.File
		logFile       *os.File
		receiptWriter *parquetgo.GenericWriter[parquet.ReceiptRecord]
		logWriter     *parquetgo.GenericWriter[parquet.LogRecord]
	)
	err := c.awaitWriter(func() error {
		// #nosec G304 -- paths are constructed from configured base directory.
		rf, err := os.Create(receiptPath)
		if err != nil {
			return fmt.Errorf("failed to create receipt parquet file: %w", err)
		}

		// #nosec G304 -- paths are constructed from configured base directory.
		lf, err := os.Create(logPath)
		if err != nil {
			if closeErr := rf.Close(); closeErr != nil {
				return fmt.Errorf("failed to create log parquet file: %w; close receipt file error: %v", err, closeErr)
			}
			return fmt.Errorf("failed to create log parquet file: %w", err)
		}

		blockNumberSorting := parquetgo.SortingWriterConfig(
			parquetgo.SortingColumns(parquetgo.Ascending("block_number")),
		)

		receiptFile = rf
		logFile = lf
		receiptWriter = parquetgo.NewGenericWriter[parquet.ReceiptRecord](rf,
			parquetgo.Compression(&parquetgo.Snappy),
			blockNumberSorting,
		)
		logWriter = parquetgo.NewGenericWriter[parquet.LogRecord](lf,
			parquetgo.Compression(&parquetgo.Snappy),
			blockNumberSorting,
		)
		return nil
	})
	if err != nil {
		return err
	}

	c.receiptFile = receiptFile
	c.logFile = logFile
	c.receiptWriter = receiptWriter
	c.logWriter = logWriter
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

	receiptWriter := c.receiptWriter
	logWriter := c.logWriter
	receiptsBuf := c.receiptsBuffer
	logsBuf := c.logsBuffer
	err := c.awaitWriter(func() error {
		if _, err := receiptWriter.Write(receiptsBuf); err != nil {
			return fmt.Errorf("failed to write receipts to parquet: %w", err)
		}
		if err := receiptWriter.Flush(); err != nil {
			return fmt.Errorf("failed to flush receipt parquet writer: %w", err)
		}

		if len(logsBuf) > 0 {
			if logWriter == nil {
				return fmt.Errorf("cannot flush logs: log writer is not initialized")
			}
			if _, err := logWriter.Write(logsBuf); err != nil {
				return fmt.Errorf("failed to write logs to parquet: %w", err)
			}
			if err := logWriter.Flush(); err != nil {
				return fmt.Errorf("failed to flush log parquet writer: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return err
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
	receiptWriter := c.receiptWriter
	logWriter := c.logWriter
	receiptFile := c.receiptFile
	logFile := c.logFile
	if receiptWriter == nil && logWriter == nil && receiptFile == nil && logFile == nil {
		return nil
	}
	c.receiptWriter = nil
	c.logWriter = nil
	c.receiptFile = nil
	c.logFile = nil

	return c.awaitWriter(func() error {
		var errs []error
		if receiptWriter != nil {
			if err := receiptWriter.Close(); err != nil {
				errs = append(errs, fmt.Errorf("receipt writer: %w", err))
			}
		}
		if logWriter != nil {
			if err := logWriter.Close(); err != nil {
				errs = append(errs, fmt.Errorf("log writer: %w", err))
			}
		}
		if receiptFile != nil {
			if err := receiptFile.Sync(); err != nil {
				errs = append(errs, fmt.Errorf("receipt file sync: %w", err))
			}
			if err := receiptFile.Close(); err != nil {
				errs = append(errs, fmt.Errorf("receipt file: %w", err))
			}
		}
		if logFile != nil {
			if err := logFile.Sync(); err != nil {
				errs = append(errs, fmt.Errorf("log file sync: %w", err))
			}
			if err := logFile.Close(); err != nil {
				errs = append(errs, fmt.Errorf("log file: %w", err))
			}
		}
		if len(errs) > 0 {
			return fmt.Errorf("close errors: %v", errs)
		}
		return nil
	})
}

// discardReplayFiles tears down parquet output created during a failed WAL
// replay without finalizing the open writer. The WAL is left intact, so the
// next startup can replay the affected blocks from scratch.
func (c *Coordinator) discardReplayFiles(initialClosedFileCount int) {
	c.discardOpenFile()

	for _, f := range c.closedFiles[initialClosedFileCount:] {
		_ = os.Remove(f.receiptPath)
		_ = os.Remove(f.logPath)
	}
	c.closedFiles = c.closedFiles[:initialClosedFileCount]
}

func (c *Coordinator) discardOpenFile() {
	receiptPath := filepath.Join(c.basePath, fmt.Sprintf("receipts_%d.parquet", c.fileStartBlock))
	logPath := filepath.Join(c.basePath, fmt.Sprintf("logs_%d.parquet", c.fileStartBlock))

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
	c.receiptsBuffer = c.receiptsBuffer[:0]
	c.logsBuffer = c.logsBuffer[:0]

	_ = os.Remove(receiptPath)
	_ = os.Remove(logPath)
}

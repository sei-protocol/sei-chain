package parquet_v2

import "fmt"

func (c *coordinator) handleWrite(req writeReq) {
	_ = c
	req.resp <- writeResp{err: ErrNotImplemented}
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

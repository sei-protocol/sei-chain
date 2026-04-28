package parquet_v2

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
	_ = c
	req.resp <- ErrNotImplemented
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
	req.resp <- ErrNotImplemented
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

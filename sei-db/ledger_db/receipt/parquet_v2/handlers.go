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
	_ = c
	req.resp <- 0
}

func (c *coordinator) handleSetLatestVersion(req setLatestVersionReq) {
	_ = c
	req.resp <- ErrNotImplemented
}

func (c *coordinator) handleSetEarliestVersion(req setEarliestVersionReq) {
	_ = c
	req.resp <- ErrNotImplemented
}

func (c *coordinator) handleUpdateLatestVersion(req updateLatestVersionReq) {
	_ = c
	req.resp <- ErrNotImplemented
}

func (c *coordinator) handleCacheRotateInterval(req cacheRotateIntervalReq) {
	_ = c
	req.resp <- 0
}

func (c *coordinator) handleFileStartBlock(req fileStartBlockReq) {
	_ = c
	req.resp <- 0
}

func (c *coordinator) handleIsRotationBoundary(req isRotationBoundaryReq) {
	_ = c
	req.resp <- false
}

func (c *coordinator) handleSetBlockFlushInterval(req setBlockFlushIntervalReq) {
	_ = c
	req.resp <- ErrNotImplemented
}

func (c *coordinator) handleSetMaxBlocksPerFile(req setMaxBlocksPerFileReq) {
	_ = c
	req.resp <- ErrNotImplemented
}

func (c *coordinator) handleSetFaultHooks(req setFaultHooksReq) {
	_ = c
	req.resp <- ErrNotImplemented
}

func (c *coordinator) handleReplayWAL(req replayWALReq) {
	_ = c
	req.resp <- replayWALResp{err: ErrNotImplemented}
}

func (c *coordinator) handlePruneTick() {
	_ = c
}

func (c *coordinator) handleClose(req closeReq) {
	_ = c
	req.resp <- ErrNotImplemented
}

func (c *coordinator) handleSimulateCrash(req simulateCrashReq) {
	_ = c
	req.resp <- struct{}{}
}

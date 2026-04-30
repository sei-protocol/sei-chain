package coordinator

import (
	"os"

	"github.com/sei-protocol/seilog"
)

var (
	removeFile = os.Remove
	logger     = seilog.NewLogger("db", "ledger-db", "parquet-v2")
)

func (c *Coordinator) pruneOldFiles(pruneBeforeBlock uint64) int {
	if len(c.closedFiles) == 0 {
		return 0
	}

	prunedCount := 0
	kept := c.closedFiles[:0]
	for _, f := range c.closedFiles {
		if !c.shouldPruneClosedFile(f, pruneBeforeBlock) {
			kept = append(kept, f)
			continue
		}

		receiptRemoved := removePrunedFile(f.receiptPath)
		if !receiptRemoved {
			kept = append(kept, f)
			continue
		}
		logRemoved := removePrunedFile(f.logPath)
		if logRemoved {
			prunedCount++
			continue
		}
		kept = append(kept, f)
	}
	c.closedFiles = kept
	return prunedCount
}

func (c *Coordinator) shouldPruneClosedFile(f closedFile, pruneBeforeBlock uint64) bool {
	fileEndBlock := f.startBlock + c.config.MaxBlocksPerFile
	if fileEndBlock < f.startBlock {
		fileEndBlock = ^uint64(0)
	}
	return fileEndBlock <= pruneBeforeBlock
}

func removePrunedFile(path string) bool {
	if path == "" {
		return true
	}
	if err := removeFile(path); err != nil && !os.IsNotExist(err) {
		logger.Error("failed to prune parquet file", "file", path, "err", err)
		return false
	}
	return true
}

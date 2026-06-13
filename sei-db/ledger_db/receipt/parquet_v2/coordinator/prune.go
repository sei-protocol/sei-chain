package coordinator

import (
	"os"

	"github.com/sei-protocol/seilog"
)

var (
	removeFile = os.Remove
	logger     = seilog.NewLogger("db", "ledger-db", "parquet-v2")
)

// pruneOldFiles deletes closed parquet pairs whose entire block range falls
// below pruneBeforeBlock. Each path (receipt, log) is deleted independently:
// on success the path is cleared on the in-memory entry, on failure it is
// left set so the next tick retries. The entry is dropped from c.closedFiles
// only when both paths are cleared, which keeps snapshot helpers from ever
// handing a deleted path to DuckDB while still allowing transient failures
// on one side to be retried without losing track of the surviving file.
// Returns the number of pairs fully removed on this tick.
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

		if removePrunedFile(f.receiptPath) {
			f.receiptPath = ""
		}
		if removePrunedFile(f.logPath) {
			f.logPath = ""
		}

		if f.receiptPath == "" && f.logPath == "" {
			prunedCount++
			continue
		}
		kept = append(kept, f)
	}
	c.closedFiles = kept
	return prunedCount
}

// shouldPruneClosedFile reports whether the file's full block range
// (startBlock + MaxBlocksPerFile) lies entirely below pruneBeforeBlock.
// Saturates on overflow rather than wrapping.
func (c *Coordinator) shouldPruneClosedFile(f closedFile, pruneBeforeBlock uint64) bool {
	fileEndBlock := f.startBlock + c.config.MaxBlocksPerFile
	if fileEndBlock < f.startBlock {
		fileEndBlock = ^uint64(0)
	}
	return fileEndBlock <= pruneBeforeBlock
}

// removePrunedFile deletes path. Treats "already gone" as success and logs
// any other failure. The package var removeFile lets tests inject failures.
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

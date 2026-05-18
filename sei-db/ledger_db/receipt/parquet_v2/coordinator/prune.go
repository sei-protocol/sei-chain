package coordinator

import (
	"os"

	"github.com/sei-protocol/seilog"
)

var (
	removeFile = os.Remove
	logger     = seilog.NewLogger("db", "ledger-db", "parquet-v2")
)

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

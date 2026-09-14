//go:build mock_block_validation

package app

import (
	"fmt"
	"os"
	"strconv"
	"sync"
)

// UnsafeMigrationBatchSizeEnv names the environment variable that replaces the
// on-chain NumKeysToMigratePerBlock rate with a node-local one. Only
// mock_block_validation builds read it.
const UnsafeMigrationBatchSizeEnv = "SEI_UNSAFE_MIGRATION_BATCH_SIZE"

var unsafeMigrationBatchSize, unsafeMigrationBatchSizeSet = readUnsafeMigrationBatchSize(
	os.Getenv(UnsafeMigrationBatchSizeEnv))

var logUnsafeMigrationBatchSizeOnce sync.Once

// readUnsafeMigrationBatchSize parses the override value. An empty value means
// unset; any other value must be a base-10 unsigned integer.
func readUnsafeMigrationBatchSize(raw string) (uint64, bool) {
	if raw == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		// Parsed at process start so a typo fails every subcommand loudly rather
		// than leaving a migration silently paused at the on-chain rate of 0.
		panic(fmt.Sprintf("%s=%q is not a base-10 unsigned integer: %v", UnsafeMigrationBatchSizeEnv, raw, err))
	}
	return n, true
}

// unsafeMigrationBatchSizeOverride returns the node-local migration rate when
// SEI_UNSAFE_MIGRATION_BATCH_SIZE is set, and numKeys otherwise.
func unsafeMigrationBatchSizeOverride(numKeys uint64) uint64 {
	if !unsafeMigrationBatchSizeSet {
		return numKeys
	}
	logUnsafeMigrationBatchSizeOnce.Do(func() {
		// A node-local rate makes this node's migration writes, and so its
		// AppHash, differ from the network's from the first migration commit.
		// The mock_block_validation consensus policy swallows that mismatch,
		// which is the only reason this override can exist in this build.
		logger.Error("UNSAFE: SC migration batch size overridden by environment; this node's AppHash "+
			"diverges from the network once migration starts",
			"env", UnsafeMigrationBatchSizeEnv, "batchSize", unsafeMigrationBatchSize, "onChain", numKeys)
	})
	return unsafeMigrationBatchSize
}

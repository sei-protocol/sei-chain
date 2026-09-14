//go:build !mock_block_validation

package app

// unsafeMigrationBatchSizeOverride returns numKeys unchanged. Only a
// mock_block_validation build carries a node-local migration rate.
func unsafeMigrationBatchSizeOverride(numKeys uint64) uint64 {
	return numKeys
}

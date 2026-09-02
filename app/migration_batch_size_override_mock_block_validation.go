//go:build mock_block_validation || mock_chain_validation

package app

import "github.com/sei-protocol/sei-chain/sei-db/config"

func mockBlockValidationMigrationBatchSize(scConfig config.StateCommitConfig) int {
	if scConfig.KeysToMigratePerBlock <= 0 {
		return 0
	}
	return scConfig.KeysToMigratePerBlock
}

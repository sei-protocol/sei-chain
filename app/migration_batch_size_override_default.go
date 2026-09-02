//go:build !mock_block_validation && !mock_chain_validation

package app

import "github.com/sei-protocol/sei-chain/sei-db/config"

func mockBlockValidationMigrationBatchSize(config.StateCommitConfig) int {
	return 0
}

//go:build !mock_block_validation

package app

import servertypes "github.com/sei-protocol/sei-chain/sei-cosmos/server/types"

// mockBlockValidationMigrationBatchSize returns 0: production builds take the
// migration rate from chain state only.
func mockBlockValidationMigrationBatchSize(servertypes.AppOptions) int {
	return 0
}

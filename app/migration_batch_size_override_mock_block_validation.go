//go:build mock_block_validation

package app

import (
	"github.com/spf13/cast"

	servertypes "github.com/sei-protocol/sei-chain/sei-cosmos/server/types"
)

// mockBlockValidationMigrationBatchSize returns the node-local migration rate
// set by state-commit.sc-keys-to-migrate-per-block, or 0 when the key is unset
// or not positive.
func mockBlockValidationMigrationBatchSize(appOpts servertypes.AppOptions) int {
	n := cast.ToInt(appOpts.Get(FlagSCKeysToMigratePerBlock))
	if n <= 0 {
		return 0
	}
	return n
}

//go:build !mock_block_validation

package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultBuildIgnoresLocalMigrationBatchSize(t *testing.T) {
	require.Zero(t, mockBlockValidationMigrationBatchSize(mapAppOpts{FlagSCKeysToMigratePerBlock: 1024}))
}

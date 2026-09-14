//go:build !mock_block_validation

package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnsafeMigrationBatchSizeOverride_ProductionBuildIgnoresEnv(t *testing.T) {
	t.Setenv("SEI_UNSAFE_MIGRATION_BATCH_SIZE", "12345")
	require.Equal(t, uint64(7), unsafeMigrationBatchSizeOverride(7))
	require.Equal(t, uint64(0), unsafeMigrationBatchSizeOverride(0))
}

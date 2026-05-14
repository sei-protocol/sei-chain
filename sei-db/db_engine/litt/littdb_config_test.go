//go:build littdb_wip

package litt

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanityCheckShardingFactorBounds(t *testing.T) {
	t.Parallel()

	t.Run("zero is rejected", func(t *testing.T) {
		t.Parallel()
		config, err := DefaultConfig("/tmp/litt-test")
		require.NoError(t, err)
		config.ShardingFactor = 0
		require.Error(t, config.SanityCheck())
	})

	t.Run("MaxShardingFactor is accepted", func(t *testing.T) {
		t.Parallel()
		config, err := DefaultConfig("/tmp/litt-test")
		require.NoError(t, err)
		config.ShardingFactor = MaxShardingFactor
		require.NoError(t, config.SanityCheck())
	})

	t.Run("MaxShardingFactor + 1 is rejected", func(t *testing.T) {
		t.Parallel()
		config, err := DefaultConfig("/tmp/litt-test")
		require.NoError(t, err)
		config.ShardingFactor = MaxShardingFactor + 1
		require.Error(t, config.SanityCheck())
	})
}

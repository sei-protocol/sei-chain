package litt

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateShardingFactorBounds(t *testing.T) {
	t.Parallel()

	t.Run("zero is rejected", func(t *testing.T) {
		t.Parallel()
		config, err := DefaultConfig("/tmp/litt-test")
		require.NoError(t, err)
		config.ShardingFactor = 0
		require.Error(t, config.Validate())
	})

	t.Run("MaxShardingFactor is accepted", func(t *testing.T) {
		t.Parallel()
		config, err := DefaultConfig("/tmp/litt-test")
		require.NoError(t, err)
		config.ShardingFactor = MaxShardingFactor
		require.NoError(t, config.Validate())
	})

}

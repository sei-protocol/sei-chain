package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
	"github.com/sei-protocol/sei-chain/sei-cosmos/version"

	"github.com/sei-protocol/sei-chain/sei-cosmos/telemetry"
)

func TestInjectTelemetryChainID(t *testing.T) {
	t.Run("appends chain id when missing", func(t *testing.T) {
		cfg := telemetry.Config{
			GlobalLabels: [][]string{{"foo", "bar"}},
		}

		got := injectTelemetryChainID(cfg, "sei-test-1")

		require.Equal(t, [][]string{
			{"foo", "bar"},
			{"chain_id", "sei-test-1"},
		}, got.GlobalLabels)
	})

	t.Run("preserves existing chain id label", func(t *testing.T) {
		cfg := telemetry.Config{
			GlobalLabels: [][]string{
				{"foo", "bar"},
				{"chain_id", "existing-chain"},
			},
		}

		got := injectTelemetryChainID(cfg, "sei-test-1")

		require.Equal(t, cfg.GlobalLabels, got.GlobalLabels)
	})

	t.Run("ignores empty chain id", func(t *testing.T) {
		cfg := telemetry.Config{
			GlobalLabels: [][]string{{"foo", "bar"}},
		}

		got := injectTelemetryChainID(cfg, "")

		require.Equal(t, cfg.GlobalLabels, got.GlobalLabels)
	})
}

func TestEVMOnlyUpgrades(t *testing.T) {
	commit := version.Commit
	t.Cleanup(func() { version.Commit = commit })
	version.Commit = "0123456789abcdef0123456789abcdef01234567"

	upgrades, err := evmOnlyUpgrades([]int{100, 200})
	require.NoError(t, err)
	require.Equal(t, gov.Upgrades{Name: version.Commit, SkipHeights: []uint64{100, 200}}, upgrades)

	upgrades, err = evmOnlyUpgrades(nil)
	require.NoError(t, err)
	require.Equal(t, gov.Upgrades{Name: version.Commit}, upgrades)

	for _, bad := range [][]int{{0}, {-1}, {5, -3}} {
		_, err := evmOnlyUpgrades(bad)
		require.Error(t, err, "heights %v", bad)
	}
}

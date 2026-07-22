package server

import (
	"testing"

	"github.com/stretchr/testify/require"

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

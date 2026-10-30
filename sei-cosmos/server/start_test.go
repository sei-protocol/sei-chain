package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-cosmos/telemetry"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
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

func TestWarnIgnoredStateSyncTrustPeriod(t *testing.T) {
	for name, tc := range map[string]struct {
		chainID     string
		trustPeriod string // config.toml value; empty leaves the key out
		wantWarning bool
	}{
		"unset":                            {chainID: "pacific-1"},
		"matches the chain's period":       {chainID: "pacific-1", trustPeriod: "112h"},
		"old default on pacific-1":         {chainID: "pacific-1", trustPeriod: "168h0m0s", wantWarning: true},
		"old default on an unlisted chain": {chainID: "sei", trustPeriod: "168h0m0s"},
		"override on an unlisted chain":    {chainID: "sei", trustPeriod: "72h", wantWarning: true},
		"not a duration":                   {chainID: "sei", trustPeriod: "one week", wantWarning: true},
	} {
		t.Run(name, func(t *testing.T) {
			configToml := "[statesync]\n"
			if tc.trustPeriod != "" {
				configToml += fmt.Sprintf("trust-period = %q\n", tc.trustPeriod)
			}
			v := viper.New()
			v.SetConfigType("toml")
			require.NoError(t, v.ReadConfig(strings.NewReader(configToml)))

			logs := &bytes.Buffer{}
			prev := logger
			logger = slog.New(slog.NewJSONHandler(logs, nil))
			t.Cleanup(func() { logger = prev })

			warnIgnoredStateSyncTrustPeriod(v, tc.chainID)

			if !tc.wantWarning {
				require.Empty(t, logs.String())
				return
			}
			var record map[string]any
			require.NoError(t, json.Unmarshal(logs.Bytes(), &record))
			require.Equal(t, slog.LevelWarn.String(), record[slog.LevelKey])
			require.Equal(t, tc.chainID, record["chainID"])
			require.Equal(t, tc.trustPeriod, record["configured"])
			require.Equal(t, tmcfg.StateSyncTrustPeriod(tc.chainID).String(), record["trustPeriod"])
		})
	}
}

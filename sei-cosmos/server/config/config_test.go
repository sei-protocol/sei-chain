package config

import (
	"bytes"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	tmcfg "github.com/tendermint/tendermint/config"

	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	require.Equal(t, DefaultMinGasPrices, cfg.MinGasPrices)
	require.False(t, cfg.GetMinGasPrices().IsZero())
}

func TestSetMinimumFees(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SetMinGasPrices(sdk.DecCoins{sdk.NewInt64DecCoin("foo", 5)})
	require.Equal(t, "5.000000000000000000foo", cfg.MinGasPrices)
}

func TestSetSnapshotDirectory(t *testing.T) {
	cfg := DefaultConfig()
	require.Equal(t, "", cfg.StateSync.SnapshotDirectory)
}

func TestSetConcurrencyWorkers(t *testing.T) {
	cfg := DefaultConfig()
	require.Equal(t, DefaultConcurrencyWorkers, cfg.ConcurrencyWorkers)
}

func TestOCCEnabled(t *testing.T) {
	cfg := DefaultConfig()
	require.True(t, cfg.OccEnabled, "OCC should be enabled by default")

	cfg.BaseConfig.OccEnabled = false
	require.False(t, cfg.OccEnabled)
}

func TestDefaultSwaggerConfig(t *testing.T) {
	cfg := DefaultConfig()
	require.True(t, cfg.API.Swagger)
}

func TestDefaultTelemetryConfig(t *testing.T) {
	cfg := DefaultConfig()
	require.True(t, cfg.Telemetry.Enabled, "Telemetry should be enabled by default")
	require.Equal(t, int64(7200), cfg.Telemetry.PrometheusRetentionTime)
	require.Empty(t, cfg.Telemetry.GlobalLabels)
}

func TestDefaultAPIConfig(t *testing.T) {
	cfg := DefaultConfig()
	require.False(t, cfg.API.Enable, "API should be disabled by default")
	require.Equal(t, "tcp://0.0.0.0:1317", cfg.API.Address)
	require.True(t, cfg.API.Swagger)
	require.Equal(t, uint(1000), cfg.API.MaxOpenConnections)
}

func TestDefaultGRPCConfig(t *testing.T) {
	cfg := DefaultConfig()
	require.True(t, cfg.GRPC.Enable)
	require.Equal(t, DefaultGRPCAddress, cfg.GRPC.Address)
}

func TestDefaultGRPCWebConfig(t *testing.T) {
	cfg := DefaultConfig()
	require.True(t, cfg.GRPCWeb.Enable)
	require.Equal(t, DefaultGRPCWebAddress, cfg.GRPCWeb.Address)
}

func TestDefaultRosettaConfig(t *testing.T) {
	cfg := DefaultConfig()
	require.False(t, cfg.Rosetta.Enable)
	require.Equal(t, ":8080", cfg.Rosetta.Address)
}

func TestDefaultStateSyncConfig(t *testing.T) {
	cfg := DefaultConfig()
	require.Equal(t, uint64(0), cfg.StateSync.SnapshotInterval)
	require.Equal(t, uint32(2), cfg.StateSync.SnapshotKeepRecent)
}

func TestDefaultGenesisConfig(t *testing.T) {
	cfg := DefaultConfig()
	require.False(t, cfg.Genesis.StreamImport)
	require.Empty(t, cfg.Genesis.GenesisStreamFile)
}

func TestValidateBasic(t *testing.T) {
	tmConfig := tmcfg.DefaultConfig()

	tests := []struct {
		name      string
		setupCfg  func() *Config
		expectErr bool
	}{
		{
			name: "valid default config",
			setupCfg: func() *Config {
				return DefaultConfig()
			},
			expectErr: false,
		},
		{
			name: "empty min gas prices",
			setupCfg: func() *Config {
				cfg := DefaultConfig()
				cfg.MinGasPrices = ""
				return cfg
			},
			expectErr: true,
		},
		{
			name: "state sync with everything pruning",
			setupCfg: func() *Config {
				cfg := DefaultConfig()
				cfg.Pruning = storetypes.PruningOptionEverything
				cfg.StateSync.SnapshotInterval = 1000
				return cfg
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.setupCfg()
			err := cfg.ValidateBasic(tmConfig)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGetMinGasPrices(t *testing.T) {
	tests := []struct {
		name     string
		minGas   string
		expected int
	}{
		{
			name:     "empty min gas prices",
			minGas:   "",
			expected: 0,
		},
		{
			name:     "single denom",
			minGas:   "0.025usei",
			expected: 1,
		},
		{
			name:     "multiple denoms",
			minGas:   "0.025usei;0.001uatom",
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.MinGasPrices = tt.minGas
			prices := cfg.GetMinGasPrices()
			require.Len(t, prices, tt.expected)
		})
	}
}

func TestGetConfig(t *testing.T) {
	v := viper.New()

	// Set required config values
	v.Set("minimum-gas-prices", DefaultMinGasPrices)
	v.Set("inter-block-cache", true)
	v.Set("pruning", storetypes.PruningOptionNothing)
	v.Set("pruning-keep-recent", "0")
	v.Set("pruning-interval", "0")
	v.Set("telemetry.enabled", true)
	v.Set("telemetry.prometheus-retention-time", int64(7200))
	v.Set("telemetry.global-labels", []interface{}{})
	v.Set("api.enable", false)
	v.Set("api.swagger", true)
	v.Set("api.address", "tcp://0.0.0.0:1317")
	v.Set("grpc.enable", true)
	v.Set("grpc.address", DefaultGRPCAddress)
	v.Set("concurrency-workers", DefaultConcurrencyWorkers)
	v.Set("occ-enabled", DefaultOccEnabled)

	cfg, err := GetConfig(v)
	require.NoError(t, err)
	require.Equal(t, DefaultMinGasPrices, cfg.MinGasPrices)
	require.True(t, cfg.Telemetry.Enabled)
	require.False(t, cfg.API.Enable)
}

func TestConfigTemplate(t *testing.T) {
	cfg := DefaultConfig()
	var buf bytes.Buffer

	err := configTemplate.Execute(&buf, cfg)
	require.NoError(t, err)

	output := buf.String()

	// Verify key sections are present
	require.Contains(t, output, "Telemetry Configuration")
	require.Contains(t, output, "[telemetry]")
	require.Contains(t, output, "API Configuration")
	require.Contains(t, output, "[api]")
	require.Contains(t, output, "enabled = true") // telemetry enabled
	require.Contains(t, output, "enable = false") // api disabled

	// Verify our new documentation is present
	require.Contains(t, output, "When both 'api.enable' and 'telemetry.enabled' are true")
	require.Contains(t, output, "Tendermint metrics (port 26660)")
}

func TestWriteConfigFile(t *testing.T) {
	cfg := DefaultConfig()
	tmpFile := t.TempDir() + "/app.toml"

	// Should not panic
	require.NotPanics(t, func() {
		WriteConfigFile(tmpFile, cfg)
	})
}

func TestParseConfig(t *testing.T) {
	v := viper.New()

	// Set basic required values
	v.Set("minimum-gas-prices", DefaultMinGasPrices)
	v.Set("telemetry.global-labels", []interface{}{})

	cfg, err := ParseConfig(v)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, DefaultMinGasPrices, cfg.MinGasPrices)
}

func TestSetConfigTemplate(t *testing.T) {
	customTemplate := `# Custom Template
[telemetry]
enabled = {{ .Telemetry.Enabled }}
`

	// Should not panic
	require.NotPanics(t, func() {
		SetConfigTemplate(customTemplate)
	})
}

func TestGetConcurrencyWorkers(t *testing.T) {
	workers := getConcurrencyWorkers()

	// Should be at least 10
	require.GreaterOrEqual(t, workers, 10)

	// Should be capped at 128
	require.LessOrEqual(t, workers, 128)
}

func TestSetAndGetMinGasPrices(t *testing.T) {
	cfg := DefaultConfig()

	// Test setting via string (semicolon-separated format used by GetMinGasPrices)
	cfg.MinGasPrices = "0.025usei;0.001uatom"
	parsed := cfg.GetMinGasPrices()
	require.Equal(t, 2, len(parsed))
	require.Equal(t, "usei", parsed[0].Denom)
	require.Equal(t, "uatom", parsed[1].Denom)
}

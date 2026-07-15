package config

import (
	"bytes"
	"testing"
	"time"

	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
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
	require.Equal(t, DefaultGRPCMaxRecvMsgSize, cfg.GRPC.MaxRecvMsgSize)
	require.Equal(t, uint(DefaultGRPCMaxOpenConnections), cfg.GRPC.MaxOpenConnections)
	require.Equal(t, DefaultGRPCMaxConnectionIdle, cfg.GRPC.MaxConnectionIdle)
	require.Equal(t, 5*time.Minute, cfg.GRPC.MaxConnectionIdle)
	require.Equal(t, DefaultGRPCMaxConnectionAge, cfg.GRPC.MaxConnectionAge)
	require.Equal(t, DefaultGRPCMaxConnectionAgeGrace, cfg.GRPC.MaxConnectionAgeGrace)
	require.Equal(t, DefaultGRPCKeepaliveTime, cfg.GRPC.KeepaliveTime)
	require.Equal(t, DefaultGRPCKeepaliveTimeout, cfg.GRPC.KeepaliveTimeout)
	require.Equal(t, DefaultGRPCKeepaliveMinTime, cfg.GRPC.KeepaliveMinTime)
	require.Equal(t, DefaultGRPCKeepalivePermitWithoutStream, cfg.GRPC.KeepalivePermitWithoutStream)
}

// seedViperWithDefaultConfig renders the default app config template and reads
// it into a fresh viper instance, mirroring how seid loads app.toml.
func seedViperWithDefaultConfig(t *testing.T) *viper.Viper {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, configTemplate.Execute(&buf, DefaultConfig()))
	v := viper.New()
	v.SetConfigType("toml")
	require.NoError(t, v.ReadConfig(&buf))
	return v
}

// TestGetConfigGRPCDefaultsWhenAbsent ensures a node upgrading with an older
// app.toml (which lacks the new gRPC keys) still gets the bounded in-code
// defaults rather than zero/unlimited values.
func TestGetConfigGRPCDefaultsWhenAbsent(t *testing.T) {
	// Minimal app.toml that predates the new gRPC keys. global-labels is the
	// only key GetConfig hard-requires.
	const legacyAppToml = `
[telemetry]
global-labels = []

[grpc]
enable = true
address = "0.0.0.0:9090"
`
	v := viper.New()
	v.SetConfigType("toml")
	require.NoError(t, v.ReadConfig(bytes.NewBufferString(legacyAppToml)))
	require.False(t, v.IsSet("grpc.max-recv-msg-size"))
	require.False(t, v.IsSet("grpc.max-open-connections"))
	require.False(t, v.IsSet("grpc.max-connection-idle"))
	require.False(t, v.IsSet("grpc.max-connection-age"))
	require.False(t, v.IsSet("grpc.max-connection-age-grace"))
	require.False(t, v.IsSet("grpc.keepalive-permit-without-stream"))

	cfg, err := GetConfig(v)
	require.NoError(t, err)
	require.Equal(t, DefaultGRPCMaxRecvMsgSize, cfg.GRPC.MaxRecvMsgSize)
	require.Equal(t, uint(DefaultGRPCMaxOpenConnections), cfg.GRPC.MaxOpenConnections)
	// The bounded idle default must survive an older app.toml that omits the key.
	require.Equal(t, DefaultGRPCMaxConnectionIdle, cfg.GRPC.MaxConnectionIdle)
	require.Equal(t, DefaultGRPCKeepaliveTime, cfg.GRPC.KeepaliveTime)
	require.Equal(t, DefaultGRPCKeepaliveTimeout, cfg.GRPC.KeepaliveTimeout)
	require.Equal(t, DefaultGRPCKeepaliveMinTime, cfg.GRPC.KeepaliveMinTime)
	// The directly-read fields (no IsSet guard) must still resolve to their
	// in-code defaults when the keys are absent.
	require.Equal(t, DefaultGRPCMaxConnectionAge, cfg.GRPC.MaxConnectionAge)
	require.Equal(t, DefaultGRPCMaxConnectionAgeGrace, cfg.GRPC.MaxConnectionAgeGrace)
	require.Equal(t, DefaultGRPCKeepalivePermitWithoutStream, cfg.GRPC.KeepalivePermitWithoutStream)
}

// TestGetConfigGRPCClampsNegativeDurations ensures a misconfigured negative
// keepalive/connection-age duration falls back to the safe in-code default
// rather than being passed verbatim to the gRPC server.
func TestGetConfigGRPCClampsNegativeDurations(t *testing.T) {
	v := seedViperWithDefaultConfig(t)
	v.Set("grpc.max-connection-idle", "-1s")
	v.Set("grpc.max-connection-age", "-1s")
	v.Set("grpc.max-connection-age-grace", "-1s")
	v.Set("grpc.keepalive-time", "-1s")
	v.Set("grpc.keepalive-timeout", "-1s")
	v.Set("grpc.keepalive-min-time", "-1s")

	cfg, err := GetConfig(v)
	require.NoError(t, err)
	require.Equal(t, DefaultGRPCMaxConnectionIdle, cfg.GRPC.MaxConnectionIdle)
	require.Equal(t, DefaultGRPCMaxConnectionAge, cfg.GRPC.MaxConnectionAge)
	require.Equal(t, DefaultGRPCMaxConnectionAgeGrace, cfg.GRPC.MaxConnectionAgeGrace)
	require.Equal(t, DefaultGRPCKeepaliveTime, cfg.GRPC.KeepaliveTime)
	require.Equal(t, DefaultGRPCKeepaliveTimeout, cfg.GRPC.KeepaliveTimeout)
	require.Equal(t, DefaultGRPCKeepaliveMinTime, cfg.GRPC.KeepaliveMinTime)
}

// TestGetConfigGRPCOverrides ensures operator-provided values override the
// in-code defaults.
func TestGetConfigGRPCOverrides(t *testing.T) {
	v := seedViperWithDefaultConfig(t)
	v.Set("grpc.max-recv-msg-size", 8*1024*1024)
	v.Set("grpc.max-open-connections", 50)
	v.Set("grpc.max-connection-idle", "5m")
	v.Set("grpc.max-connection-age", "30m")
	v.Set("grpc.max-connection-age-grace", "1m")
	v.Set("grpc.keepalive-time", "1m")
	v.Set("grpc.keepalive-timeout", "10s")
	v.Set("grpc.keepalive-min-time", "30s")
	v.Set("grpc.keepalive-permit-without-stream", true)

	cfg, err := GetConfig(v)
	require.NoError(t, err)
	require.Equal(t, 8*1024*1024, cfg.GRPC.MaxRecvMsgSize)
	require.Equal(t, uint(50), cfg.GRPC.MaxOpenConnections)
	require.Equal(t, 5*time.Minute, cfg.GRPC.MaxConnectionIdle)
	require.Equal(t, 30*time.Minute, cfg.GRPC.MaxConnectionAge)
	require.Equal(t, time.Minute, cfg.GRPC.MaxConnectionAgeGrace)
	require.Equal(t, time.Minute, cfg.GRPC.KeepaliveTime)
	require.Equal(t, 10*time.Second, cfg.GRPC.KeepaliveTimeout)
	require.Equal(t, 30*time.Second, cfg.GRPC.KeepaliveMinTime)
	require.True(t, cfg.GRPC.KeepalivePermitWithoutStream)
}

func TestDefaultGRPCWebConfig(t *testing.T) {
	cfg := DefaultConfig()
	require.True(t, cfg.GRPCWeb.Enable)
	require.Equal(t, DefaultGRPCWebAddress, cfg.GRPCWeb.Address)
	require.Equal(t, uint(DefaultGRPCWebMaxOpenConnections), cfg.GRPCWeb.MaxOpenConnections)
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
	// sc-snapshot-interval / sc-keep-recent are absent here, so FlatKV must keep
	// its in-code defaults rather than being clobbered to 0 (which would disable
	// FlatKV snapshots / drop all old snapshots). The memIAVL mirror only applies
	// when the keys are explicitly set.
	defaultFlatKV := seidbconfig.DefaultStateCommitConfig().FlatKVConfig
	require.Equal(t, defaultFlatKV.SnapshotInterval, cfg.StateCommit.FlatKVConfig.SnapshotInterval)
	require.Equal(t, defaultFlatKV.SnapshotKeepRecent, cfg.StateCommit.FlatKVConfig.SnapshotKeepRecent)
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

func TestGetConfigGRPCWebMaxOpenConnections(t *testing.T) {
	baseViper := func() *viper.Viper {
		v := viper.New()
		v.Set("minimum-gas-prices", DefaultMinGasPrices)
		v.Set("telemetry.global-labels", []interface{}{})
		return v
	}

	t.Run("missing key falls back to the in-code default", func(t *testing.T) {
		// Mirrors a node upgrading with an older app.toml that predates the
		// grpc-web.max-open-connections key
		cfg, err := GetConfig(baseViper())
		require.NoError(t, err)
		require.Equal(t, uint(DefaultGRPCWebMaxOpenConnections), cfg.GRPCWeb.MaxOpenConnections)
	})

	t.Run("explicit zero is preserved as unlimited", func(t *testing.T) {
		v := baseViper()
		v.Set("grpc-web.max-open-connections", 0)
		cfg, err := GetConfig(v)
		require.NoError(t, err)
		require.Equal(t, uint(0), cfg.GRPCWeb.MaxOpenConnections,
			"explicit 0 must remain an opt-in to unlimited connections")
	})

	t.Run("explicit value overrides the default", func(t *testing.T) {
		v := baseViper()
		v.Set("grpc-web.max-open-connections", 250)
		cfg, err := GetConfig(v)
		require.NoError(t, err)
		require.Equal(t, uint(250), cfg.GRPCWeb.MaxOpenConnections)
	})
}

func TestGetConfigStateCommit(t *testing.T) {
	v := viper.New()

	// Set required base values
	v.Set("minimum-gas-prices", DefaultMinGasPrices)
	v.Set("telemetry.global-labels", []interface{}{})

	v.Set("state-commit.sc-enable", true)
	v.Set("state-commit.sc-directory", "/custom/path")
	// Opt out of auto so the explicit sc-write-mode is honored.
	v.Set("state-commit.sc-write-mode-enable-auto", false)
	v.Set("state-commit.sc-write-mode", "test_only_dual_write")
	v.Set("state-commit.sc-async-commit-buffer", 200)
	v.Set("state-commit.sc-keep-recent", 5)
	v.Set("state-commit.sc-snapshot-interval", 5000)
	v.Set("state-commit.sc-snapshot-min-time-interval", 1800)
	v.Set("state-commit.sc-snapshot-writer-limit", 4)
	v.Set("state-commit.sc-snapshot-prefetch-threshold", 0.9)

	cfg, err := GetConfig(v)
	require.NoError(t, err)

	require.True(t, cfg.StateCommit.Enable)
	require.Equal(t, "/custom/path", cfg.StateCommit.Directory)
	require.False(t, cfg.StateCommit.WriteModeEnableAuto)
	require.Equal(t, sctypes.TestOnlyDualWrite, cfg.StateCommit.WriteMode)

	// Verify MemIAVLConfig fields
	require.Equal(t, 200, cfg.StateCommit.MemIAVLConfig.AsyncCommitBuffer)
	require.Equal(t, uint32(5), cfg.StateCommit.MemIAVLConfig.SnapshotKeepRecent)
	require.Equal(t, uint32(5000), cfg.StateCommit.MemIAVLConfig.SnapshotInterval)
	require.Equal(t, uint32(1800), cfg.StateCommit.MemIAVLConfig.SnapshotMinTimeInterval)
	require.Equal(t, 4, cfg.StateCommit.MemIAVLConfig.SnapshotWriterLimit)
	require.Equal(t, 0.9, cfg.StateCommit.MemIAVLConfig.SnapshotPrefetchThreshold)
}

func TestGetConfigParsesRawSnapshotKeepRecent(t *testing.T) {
	v := viper.New()
	v.Set("minimum-gas-prices", DefaultMinGasPrices)
	v.Set("telemetry.global-labels", []interface{}{})
	v.Set("state-commit.sc-keep-recent", 0)

	cfg, err := GetConfig(v)
	require.NoError(t, err)
	// GetConfig is a faithful parse of app.toml/flags: the raw 0 is preserved for
	// memIAVL here and only floored later at store construction. FlatKV does not
	// mirror the sc-* keys in GetConfig (that is composite.alignFlatKVSnapshotWithMemIAVL's
	// job), so it keeps its in-code default.
	require.Equal(t, uint32(0), cfg.StateCommit.MemIAVLConfig.SnapshotKeepRecent)
	require.Equal(t, seidbconfig.DefaultStateCommitConfig().FlatKVConfig.SnapshotKeepRecent, cfg.StateCommit.FlatKVConfig.SnapshotKeepRecent)
}

func TestGetConfigHonorsExplicitFlatKVOverrides(t *testing.T) {
	v := viper.New()
	v.Set("minimum-gas-prices", DefaultMinGasPrices)
	v.Set("telemetry.global-labels", []interface{}{})
	// Explicit (hidden) FlatKV overrides must win over the in-code defaults.
	v.Set("state-commit.flatkv.fsync", true)
	v.Set("state-commit.flatkv.async-write-buffer", 128)
	v.Set("state-commit.flatkv.snapshot-interval", 7000)
	v.Set("state-commit.flatkv.snapshot-keep-recent", 9)
	v.Set("state-commit.flatkv.enable-read-write-metrics", true)

	cfg, err := GetConfig(v)
	require.NoError(t, err)
	fk := cfg.StateCommit.FlatKVConfig
	require.True(t, fk.Fsync)
	require.Equal(t, 128, fk.AsyncWriteBuffer)
	require.Equal(t, uint32(7000), fk.SnapshotInterval)
	require.Equal(t, uint32(9), fk.SnapshotKeepRecent)
	require.True(t, fk.EnableReadWriteMetrics)
}

// TestGetConfigFlatKVDefaultsWhenSCSnapshotAbsent locks in the regression fix:
// GetConfig does not mirror the sc-* keys onto FlatKV (that is
// composite.alignFlatKVSnapshotWithMemIAVL's job at store construction), and an
// absent sc-snapshot-interval / sc-keep-recent must preserve the in-code FlatKV
// defaults rather than reading back 0 (which disables FlatKV snapshots and drops
// all old snapshots).
func TestGetConfigFlatKVDefaultsWhenSCSnapshotAbsent(t *testing.T) {
	v := viper.New()
	v.Set("minimum-gas-prices", DefaultMinGasPrices)
	v.Set("telemetry.global-labels", []interface{}{})

	cfg, err := GetConfig(v)
	require.NoError(t, err)
	defaultFlatKV := seidbconfig.DefaultStateCommitConfig().FlatKVConfig
	require.Equal(t, defaultFlatKV.SnapshotInterval, cfg.StateCommit.FlatKVConfig.SnapshotInterval)
	require.Equal(t, defaultFlatKV.SnapshotKeepRecent, cfg.StateCommit.FlatKVConfig.SnapshotKeepRecent)
	require.NotZero(t, cfg.StateCommit.FlatKVConfig.SnapshotInterval)
}

// TestGetConfigMemIAVLDefaultsWhenSCKeysAbsent asserts that, with no
// state-commit.sc-* keys set, GetConfig resolves the memIAVL config to its
// in-code defaults rather than clobbering the non-zero ones to the zero value.
func TestGetConfigMemIAVLDefaultsWhenSCKeysAbsent(t *testing.T) {
	v := viper.New()
	v.Set("minimum-gas-prices", DefaultMinGasPrices)
	v.Set("telemetry.global-labels", []interface{}{})

	cfg, err := GetConfig(v)
	require.NoError(t, err)

	def := seidbconfig.DefaultStateCommitConfig().MemIAVLConfig
	mem := cfg.StateCommit.MemIAVLConfig
	require.Equal(t, def.AsyncCommitBuffer, mem.AsyncCommitBuffer)
	require.Equal(t, def.SnapshotKeepRecent, mem.SnapshotKeepRecent)
	require.Equal(t, def.SnapshotInterval, mem.SnapshotInterval)
	require.Equal(t, def.SnapshotMinTimeInterval, mem.SnapshotMinTimeInterval)
	require.Equal(t, def.SnapshotWriterLimit, mem.SnapshotWriterLimit)
	require.Equal(t, def.SnapshotPrefetchThreshold, mem.SnapshotPrefetchThreshold)
	// The defaults that matter most are the non-zero ones: an absent key must
	// not silently downgrade the node (e.g. async-commit-buffer to 0 =
	// synchronous commits, or snapshot-interval to 0).
	require.NotZero(t, mem.AsyncCommitBuffer)
	require.NotZero(t, mem.SnapshotInterval)
}

func TestGetConfigRejectsInvalidWriteMode(t *testing.T) {
	v := viper.New()

	v.Set("minimum-gas-prices", DefaultMinGasPrices)
	v.Set("telemetry.global-labels", []interface{}{})

	v.Set("state-commit.sc-write-mode", "bogus_mode")

	_, err := GetConfig(v)
	require.Error(t, err)
	require.Contains(t, err.Error(), "state-commit.sc-write-mode")
	require.Contains(t, err.Error(), "bogus_mode")
}

// TestGetConfigLegacyMemiavlOnlyResolvesToAuto guards the existing-fleet
// upgrade path: a config written by an older binary carries an explicit
// sc-write-mode = "memiavl_only" but no sc-write-mode-enable-auto key. The absent
// key must default to true so the node resolves to auto and can follow a
// governance-driven migration without any app.toml edit.
func TestGetConfigLegacyMemiavlOnlyResolvesToAuto(t *testing.T) {
	v := viper.New()

	v.Set("minimum-gas-prices", DefaultMinGasPrices)
	v.Set("telemetry.global-labels", []interface{}{})
	v.Set("state-commit.sc-write-mode", "memiavl_only")

	cfg, err := GetConfig(v)
	require.NoError(t, err)
	require.True(t, cfg.StateCommit.WriteModeEnableAuto)
	require.Equal(t, sctypes.Auto, cfg.StateCommit.WriteMode,
		"absent sc-write-mode-enable-auto must default to true and override an explicit memiavl_only")
}

func TestGetConfigLegacyCosmosOnlyResolvesToAuto(t *testing.T) {
	v := viper.New()

	v.Set("minimum-gas-prices", DefaultMinGasPrices)
	v.Set("telemetry.global-labels", []interface{}{})
	v.Set("state-commit.sc-write-mode", "cosmos_only")

	cfg, err := GetConfig(v)
	require.NoError(t, err)
	require.True(t, cfg.StateCommit.WriteModeEnableAuto)
	require.Equal(t, sctypes.Auto, cfg.StateCommit.WriteMode,
		"v6.4/v6.5 app.toml files with cosmos_only must parse before auto mode is applied")
}

// TestGetConfigPinnedModeRequiresAutoDisabled verifies that an explicit
// sc-write-mode is only honored when sc-write-mode-enable-auto = false. With auto
// enabled (the default), the explicit mode is ignored and the node runs in auto.
func TestGetConfigPinnedModeRequiresAutoDisabled(t *testing.T) {
	for _, mode := range []sctypes.WriteMode{
		sctypes.FlatKVOnly,
		sctypes.EVMMigrated,
		sctypes.TestOnlyDualWrite,
	} {
		t.Run(string(mode)+"/auto-disabled-pins", func(t *testing.T) {
			v := viper.New()
			v.Set("minimum-gas-prices", DefaultMinGasPrices)
			v.Set("telemetry.global-labels", []interface{}{})
			v.Set("state-commit.sc-write-mode-enable-auto", false)
			v.Set("state-commit.sc-write-mode", string(mode))

			cfg, err := GetConfig(v)
			require.NoError(t, err)
			require.False(t, cfg.StateCommit.WriteModeEnableAuto)
			require.Equal(t, mode, cfg.StateCommit.WriteMode,
				"with auto disabled the explicit mode must be honored as a pin")
		})

		t.Run(string(mode)+"/auto-enabled-overrides", func(t *testing.T) {
			v := viper.New()
			v.Set("minimum-gas-prices", DefaultMinGasPrices)
			v.Set("telemetry.global-labels", []interface{}{})
			v.Set("state-commit.sc-write-mode", string(mode))

			cfg, err := GetConfig(v)
			require.NoError(t, err)
			require.True(t, cfg.StateCommit.WriteModeEnableAuto)
			require.Equal(t, sctypes.Auto, cfg.StateCommit.WriteMode,
				"with auto enabled (default) the explicit mode must be ignored in favor of auto")
		})
	}
}

func TestGetConfigEmptyWriteModeUsesDefault(t *testing.T) {
	v := viper.New()

	v.Set("minimum-gas-prices", DefaultMinGasPrices)
	v.Set("telemetry.global-labels", []interface{}{})

	cfg, err := GetConfig(v)
	require.NoError(t, err)
	require.Equal(t, sctypes.Auto, cfg.StateCommit.WriteMode,
		"unset sc-write-mode must fall back to the in-code default")
}

func TestGetConfigStateStore(t *testing.T) {
	v := viper.New()

	// Set required base values
	v.Set("minimum-gas-prices", DefaultMinGasPrices)
	v.Set("telemetry.global-labels", []interface{}{})

	// Set StateStore values using the TOML key names (ss-* prefix)
	v.Set("state-store.ss-enable", true)
	v.Set("state-store.ss-db-directory", "/custom/ss/path")
	v.Set("state-store.ss-backend", "rocksdb")
	v.Set("state-store.ss-async-write-buffer", 500)
	v.Set("state-store.ss-keep-recent", 50000)
	v.Set("state-store.ss-prune-interval", 1200)
	v.Set("state-store.ss-import-num-workers", 4)
	v.Set("state-store.evm-ss-db-directory", "/custom/evm/ss/path")
	v.Set("state-store.evm-ss-split", true)
	v.Set("state-store.evm-ss-separate-dbs", true)

	cfg, err := GetConfig(v)
	require.NoError(t, err)

	// Verify StateStore fields are correctly parsed
	require.True(t, cfg.StateStore.Enable)
	require.Equal(t, "/custom/ss/path", cfg.StateStore.DBDirectory)
	require.Equal(t, "rocksdb", cfg.StateStore.Backend)
	require.Equal(t, 500, cfg.StateStore.AsyncWriteBuffer)
	require.Equal(t, 50000, cfg.StateStore.KeepRecent)
	require.Equal(t, 1200, cfg.StateStore.PruneIntervalSeconds)
	require.Equal(t, 4, cfg.StateStore.ImportNumWorkers)
	require.Equal(t, "/custom/evm/ss/path", cfg.StateStore.EVMDBDirectory)
	require.True(t, cfg.StateStore.EVMSplit)
	require.True(t, cfg.StateStore.SeparateEVMSubDBs)
}

func TestDefaultStateCommitConfig(t *testing.T) {
	cfg := DefaultConfig()

	require.True(t, cfg.StateCommit.Enable)
	require.Empty(t, cfg.StateCommit.Directory)
	// WriteMode is the fixed fallback (memiavl_only); WriteModeEnableAuto
	// defaults true, so the effective default after resolution is auto.
	require.Equal(t, sctypes.MemiavlOnly, cfg.StateCommit.WriteMode)
	require.True(t, cfg.StateCommit.WriteModeEnableAuto)
}

func TestDefaultStateStoreConfig(t *testing.T) {
	cfg := DefaultConfig()

	// Verify default StateStore values
	require.True(t, cfg.StateStore.Enable)
	require.Empty(t, cfg.StateStore.DBDirectory)
	require.Equal(t, "pebbledb", cfg.StateStore.Backend)
	require.Equal(t, seidbconfig.DefaultSSAsyncBuffer, cfg.StateStore.AsyncWriteBuffer)
	require.Equal(t, seidbconfig.DefaultSSKeepRecent, cfg.StateStore.KeepRecent)
	require.Equal(t, seidbconfig.DefaultSSPruneInterval, cfg.StateStore.PruneIntervalSeconds)
	require.Equal(t, seidbconfig.DefaultSSImportWorkers, cfg.StateStore.ImportNumWorkers)
	require.False(t, cfg.StateStore.EVMSplit)
	require.False(t, cfg.StateStore.SeparateEVMSubDBs)
}

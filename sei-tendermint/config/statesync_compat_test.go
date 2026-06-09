package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/cmd/tendermint/commands"
	tmconfig "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

// This test (and TestFreshStateSyncConfigValidatesWithoutHiddenKnobs) mutate the
// global viper singleton via commands.ParseConfig, so they must not run in
// parallel with other tests in this package.

func TestHiddenStateSyncKnobsStillParseFromExistingConfig(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	configPath := filepath.Join(t.TempDir(), "config.toml")
	err := os.WriteFile(configPath, []byte(`
[statesync]
enable = true
rpc-servers = "localhost:26657,localhost:26659"
trust-height = 10
trust-hash = "AABB"
trust-period = "168h0m0s"
backfill-blocks = "100"
backfill-duration = "5m0s"
discovery-time = "20s"
temp-dir = "/tmp/statesync"
chunk-request-timeout = "30s"
fetchers = "5"
verify-light-block-timeout = "2m0s"
blacklist-ttl = "10m0s"
`), 0600)
	require.NoError(t, err)

	viper.SetConfigFile(configPath)
	require.NoError(t, viper.ReadInConfig())

	cfg, err := commands.ParseConfig(tmconfig.DefaultConfig())
	require.NoError(t, err)
	require.True(t, cfg.StateSync.Enable)
	require.Equal(t, []string{"localhost:26657", "localhost:26659"}, cfg.StateSync.RPCServers)
	require.Equal(t, int64(10), cfg.StateSync.TrustHeight)
	require.Equal(t, "AABB", cfg.StateSync.TrustHash)
	require.Equal(t, int64(100), cfg.StateSync.BackfillBlocks)
	require.Equal(t, 5*time.Minute, cfg.StateSync.BackfillDuration)
	require.Equal(t, 20*time.Second, cfg.StateSync.DiscoveryTime)
	require.Equal(t, "/tmp/statesync", cfg.StateSync.TempDir)
	require.Equal(t, 30*time.Second, cfg.StateSync.ChunkRequestTimeout)
	require.Equal(t, int32(5), cfg.StateSync.Fetchers)
	require.Equal(t, 2*time.Minute, cfg.StateSync.VerifyLightBlockTimeout)
	require.Equal(t, 10*time.Minute, cfg.StateSync.BlacklistTTL)
}

// TestFreshStateSyncConfigValidatesWithoutHiddenKnobs mirrors the freshly-rendered
// template (no hidden tuning knobs in the file) and verifies that ParseConfig
// still produces a valid StateSyncConfig that passes ValidateBasic. This guards
// against future regressions where someone removes or zeros a default in
// DefaultStateSyncConfig without realizing the template no longer carries it.
func TestFreshStateSyncConfigValidatesWithoutHiddenKnobs(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	configPath := filepath.Join(t.TempDir(), "config.toml")
	err := os.WriteFile(configPath, []byte(`
[statesync]
enable = true
rpc-servers = "localhost:26657,localhost:26659"
trust-height = 10
trust-hash = "AABB"
trust-period = "168h0m0s"
`), 0600)
	require.NoError(t, err)

	viper.SetConfigFile(configPath)
	require.NoError(t, viper.ReadInConfig())

	cfg, err := commands.ParseConfig(tmconfig.DefaultConfig())
	require.NoError(t, err)

	defaults := tmconfig.DefaultStateSyncConfig()
	require.Equal(t, defaults.BackfillBlocks, cfg.StateSync.BackfillBlocks)
	require.Equal(t, defaults.BackfillDuration, cfg.StateSync.BackfillDuration)
	require.Equal(t, defaults.DiscoveryTime, cfg.StateSync.DiscoveryTime)
	require.Equal(t, defaults.ChunkRequestTimeout, cfg.StateSync.ChunkRequestTimeout)
	require.Equal(t, defaults.Fetchers, cfg.StateSync.Fetchers)
	require.Equal(t, defaults.VerifyLightBlockTimeout, cfg.StateSync.VerifyLightBlockTimeout)
	require.Equal(t, defaults.BlacklistTTL, cfg.StateSync.BlacklistTTL)

	require.NoError(t, cfg.StateSync.ValidateBasic())
}

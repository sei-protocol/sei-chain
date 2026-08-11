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

// This test (and TestFreshP2PConfigKeepsDefaultPacing) mutate the global viper
// singleton via commands.ParseConfig, so they must not run in parallel with
// other tests in this package.

// The p2p pacing knobs are deliberately absent from the generated template
// (see checkConfig in toml_test.go), so nothing else proves an operator can
// actually set them. Without this, "not in the template" and "not readable"
// are indistinguishable.
func TestHiddenP2PKnobsStillParseFromExistingConfig(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	configPath := filepath.Join(t.TempDir(), "config.toml")
	err := os.WriteFile(configPath, []byte(`
[p2p]
laddr = "tcp://0.0.0.0:26656"
dial-interval = "5s"
accept-interval = "20ms"
`), 0600)
	require.NoError(t, err)

	viper.SetConfigFile(configPath)
	require.NoError(t, viper.ReadInConfig())

	cfg, err := commands.ParseConfig(tmconfig.DefaultConfig())
	require.NoError(t, err)
	require.Equal(t, 5*time.Second, cfg.P2P.DialInterval)
	require.Equal(t, 20*time.Millisecond, cfg.P2P.AcceptInterval)
	require.NoError(t, cfg.P2P.ValidateBasic())
}

// TestFreshP2PConfigKeepsDefaultPacing mirrors the freshly-rendered template
// (no pacing knobs in the file) and verifies ParseConfig still produces the
// defaults. Both directions matter: a zeroed AcceptInterval means
// rate.Every(0) == rate.Inf, i.e. no accept pacing at all, while a value large
// enough to matter throttles the accept loop below the rate at which peers
// arrive. Neither is visible in the rendered config, so pin it here.
func TestFreshP2PConfigKeepsDefaultPacing(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	configPath := filepath.Join(t.TempDir(), "config.toml")
	err := os.WriteFile(configPath, []byte(`
[p2p]
laddr = "tcp://0.0.0.0:26656"
`), 0600)
	require.NoError(t, err)

	viper.SetConfigFile(configPath)
	require.NoError(t, viper.ReadInConfig())

	cfg, err := commands.ParseConfig(tmconfig.DefaultConfig())
	require.NoError(t, err)

	defaults := tmconfig.DefaultP2PConfig()
	require.Equal(t, defaults.AcceptInterval, cfg.P2P.AcceptInterval)
	require.Equal(t, defaults.DialInterval, cfg.P2P.DialInterval)
	require.NotZero(t, cfg.P2P.AcceptInterval, "a zero accept-interval disables accept pacing entirely")
	require.NoError(t, cfg.P2P.ValidateBasic())
}

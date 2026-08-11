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

// This test (and TestP2PConfigPredatingPacingKnobsKeepsDefaults) mutate the
// global viper singleton via commands.ParseConfig, so they must not run in
// parallel with other tests in this package.

// accept-interval is rendered in the template, but dial-interval is not (see
// checkConfig in toml_test.go), so for that key "absent from the template" and
// "not readable at all" would otherwise be indistinguishable. Cover both, since
// an operator sets them the same way.
func TestP2PPacingKnobsParseFromExistingConfig(t *testing.T) {
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

// TestP2PConfigPredatingPacingKnobsKeepsDefaults mirrors a config.toml written
// before these keys existed — the case every already-deployed node is in, since
// seid does not rewrite an existing config.toml — and verifies ParseConfig still
// produces the defaults. The failure it guards is silent: a zeroed AcceptInterval
// means rate.Every(0) == rate.Inf, disabling accept pacing entirely, and an
// absent key must not land there.
func TestP2PConfigPredatingPacingKnobsKeepsDefaults(t *testing.T) {
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

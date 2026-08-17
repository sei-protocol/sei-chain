package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	tmconfig "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

// readP2PConfig decodes a config.toml into a default Config: absent keys keep
// the value already in the struct.
func readP2PConfig(t *testing.T, body string) *tmconfig.P2PConfig {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0600))

	v := viper.New()
	v.SetConfigFile(path)
	require.NoError(t, v.ReadInConfig())

	cfg := tmconfig.DefaultConfig()
	require.NoError(t, v.Unmarshal(cfg))
	return cfg.P2P
}

// dial-interval is absent from the generated template (see checkConfig in
// toml_test.go), so nothing else shows it is readable at all.
func TestP2PPacingKnobsParseFromExistingConfig(t *testing.T) {
	p2p := readP2PConfig(t, `
[p2p]
laddr = "tcp://0.0.0.0:26656"
dial-interval = "5s"
accept-interval = "20ms"
`)

	require.Equal(t, 5*time.Second, p2p.DialInterval)
	require.Equal(t, 20*time.Millisecond, p2p.AcceptInterval)
	require.NoError(t, p2p.ValidateBasic())
}

// TestP2PConfigPredatingPacingKnobsKeepsDefaults asserts a config.toml written
// before these keys existed still parses to the defaults rather than to zero,
// which rate.Every would read as "no pacing".
func TestP2PConfigPredatingPacingKnobsKeepsDefaults(t *testing.T) {
	p2p := readP2PConfig(t, `
[p2p]
laddr = "tcp://0.0.0.0:26656"
`)

	defaults := tmconfig.DefaultP2PConfig()
	require.Equal(t, defaults.AcceptInterval, p2p.AcceptInterval)
	require.Equal(t, defaults.DialInterval, p2p.DialInterval)
	require.NotZero(t, p2p.AcceptInterval, "a zero accept-interval disables accept pacing entirely")
	require.NoError(t, p2p.ValidateBasic())
}

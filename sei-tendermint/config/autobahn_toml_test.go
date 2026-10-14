package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/cmd/tendermint/commands"
	tmconfig "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

// These tests mutate the global viper singleton via commands.ParseConfig,
// so they must not run in parallel with other tests in this package.

// TestAutobahnKeysParseFromTopLevel guards against the trap where TOML keys
// authored after a [section] header get silently nested under that section.
// AutobahnConfigFile and HashVaultDisabledUnsafe are top-level fields on
// Config, so they must appear before any [section] header in the on-disk file.
func TestAutobahnKeysParseFromTopLevel(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	const content = `
autobahn-config-file = "/etc/sei/autobahn.json"
hash-vault-disabled-unsafe = true

[rpc]
laddr = "tcp://127.0.0.1:26657"
`
	configPath := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0600))

	viper.SetConfigFile(configPath)
	require.NoError(t, viper.ReadInConfig())

	cfg, err := commands.ParseConfig(tmconfig.DefaultConfig())
	require.NoError(t, err)
	require.Equal(t, "/etc/sei/autobahn.json", cfg.AutobahnConfigFile)
	require.True(t, cfg.HashVaultDisabledUnsafe)
}

// TestAutobahnKeysIgnoredUnderSectionHeader documents what breaks if the
// key is placed after a [section] header: TOML semantics nest it inside
// that section, viper looks for it at the root via mapstructure, and it
// silently parses as empty.
func TestAutobahnKeysIgnoredUnderSectionHeader(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	const content = `
[self-remediation]
autobahn-config-file = "/etc/sei/autobahn.json"
hash-vault-disabled-unsafe = true
`
	configPath := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0600))

	viper.SetConfigFile(configPath)
	require.NoError(t, viper.ReadInConfig())

	cfg, err := commands.ParseConfig(tmconfig.DefaultConfig())
	require.NoError(t, err)
	// The field ends up empty — viper saw self-remediation.autobahn-config-file
	// instead of the top-level key mapstructure was looking for.
	require.Empty(t, cfg.AutobahnConfigFile)
	require.False(t, cfg.HashVaultDisabledUnsafe)
}

// TestRenderedTemplateAutobahnKeysAtTopLevel verifies that the freshly
// rendered config template puts Autobahn keys at top level (i.e.
// above every [section] header). Pure structural check — guards against
// future edits accidentally moving it back under a section.
func TestRenderedTemplateAutobahnKeysAtTopLevel(t *testing.T) {
	tmpDir := t.TempDir()
	tmconfig.EnsureRoot(tmpDir)
	require.NoError(t, tmconfig.WriteConfigFile(tmpDir, tmconfig.DefaultConfig()))

	data, err := os.ReadFile(filepath.Join(tmpDir, "config", "config.toml"))
	require.NoError(t, err)
	rendered := string(data)

	for _, key := range []string{"autobahn-config-file", "hash-vault-disabled-unsafe"} {
		keyIdx := strings.Index(rendered, key)
		require.NotEqual(t, -1, keyIdx, "key %q must appear in rendered template", key)
		// Find the nearest [section] header above keyIdx, if any.
		preamble := rendered[:keyIdx]
		if lastSection := strings.LastIndex(preamble, "\n["); lastSection != -1 {
			// There's a [section] above; reject — TOML would nest the key.
			t.Fatalf("key %q follows a [section] header (parsed as nested):\n%s",
				key, preamble[lastSection:])
		}
	}
}

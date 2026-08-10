package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/seeds"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/stretchr/testify/require"
)

// Covers the init-time wiring itself, not just the seed data. Without this,
// removing the applyDefaultBootstrapPeers call from InitCmd leaves every test
// in the tree passing.
func TestApplyDefaultBootstrapPeers(t *testing.T) {
	tests := []struct {
		name    string
		chainID string
		want    string
	}{
		{"mainnet gets seeds", "pacific-1", seeds.BootstrapPeers("pacific-1")},
		{"testnet gets seeds", "atlantic-2", seeds.BootstrapPeers("atlantic-2")},
		// arctic-1 is a devnet and deliberately ships no seeds.
		{"devnet gets none", "arctic-1", ""},
		{"unknown chain gets none", "my-private-chain", ""},
		{"empty chain-id gets none", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tmcfg.DefaultConfig()
			applyDefaultBootstrapPeers(cfg, tt.chainID)
			if cfg.P2P.BootstrapPeers != tt.want {
				t.Errorf("bootstrap-peers = %q, want %q", cfg.P2P.BootstrapPeers, tt.want)
			}
		})
	}
}

// The public networks must actually get a usable set, not just a non-empty
// string — a silently truncated table would still satisfy the table test above.
func TestApplyDefaultBootstrapPeersPopulatesPublicNetworks(t *testing.T) {
	for _, chainID := range []string{"pacific-1", "atlantic-2"} {
		cfg := tmcfg.DefaultConfig()
		applyDefaultBootstrapPeers(cfg, chainID)
		if n := len(strings.Split(cfg.P2P.BootstrapPeers, ",")); n != 3 {
			t.Errorf("%s: got %d bootstrap peers, want 3 (%q)", chainID, n, cfg.P2P.BootstrapPeers)
		}
	}
}

// A pre-populated value is never overwritten. Not reachable through `seid init`
// today (it has no bootstrap-peers flag), but the guard is the reason this stays
// true for any future caller.
func TestApplyDefaultBootstrapPeersPreservesExistingValue(t *testing.T) {
	const existing = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef@peer.example.com:26656"

	cfg := tmcfg.DefaultConfig()
	cfg.P2P.BootstrapPeers = existing
	applyDefaultBootstrapPeers(cfg, "pacific-1")

	if cfg.P2P.BootstrapPeers != existing {
		t.Errorf("existing bootstrap-peers was overwritten: got %q, want %q", cfg.P2P.BootstrapPeers, existing)
	}
}

// runInit executes the real InitCmd against a temp home and returns the written
// config.toml. Testing the helper alone does not cover the call site — without
// this, deleting applyDefaultBootstrapPeers from RunE leaves the suite green.
func runInit(t *testing.T, chainID string) string {
	t.Helper()
	home := t.TempDir()
	// The root command creates the home layout and client.toml before init runs.
	// Standing InitCmd up directly, the test owns that scaffolding.
	configDir := filepath.Join(home, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "client.toml"), []byte(
		"chain-id = \"\"\nkeyring-backend = \"test\"\noutput = \"text\"\nnode = \"tcp://localhost:26657\"\nbroadcast-mode = \"sync\"\n",
	), 0o644))

	encCfg := app.MakeEncodingConfig()
	clientCtx := client.Context{}.WithCodec(encCfg.Marshaler).WithHomeDir(home).WithViper("")

	cmd := InitCmd(app.ModuleBasics, home)
	cmd.SetArgs([]string{"testnode", "--chain-id", chainID, "--home", home})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	ctx := context.WithValue(context.Background(), client.ClientContextKey, &clientCtx)
	require.NoError(t, cmd.ExecuteContext(ctx))

	data, err := os.ReadFile(filepath.Join(home, "config", "config.toml"))
	require.NoError(t, err)
	return string(data)
}

// bootstrapPeersLine returns the rendered `bootstrap-peers = "..."` value.
func bootstrapPeersLine(t *testing.T, configToml string) string {
	t.Helper()
	for _, line := range strings.Split(configToml, "\n") {
		if after, ok := strings.CutPrefix(strings.TrimSpace(line), "bootstrap-peers = "); ok {
			return strings.Trim(after, `"`)
		}
	}
	t.Fatal("config.toml has no bootstrap-peers line")
	return ""
}

// The end-to-end assertion: `seid init` on a public network writes the seeds.
func TestInitCmdWritesDefaultBootstrapPeers(t *testing.T) {
	for _, chainID := range []string{"pacific-1", "atlantic-2"} {
		t.Run(chainID, func(t *testing.T) {
			got := bootstrapPeersLine(t, runInit(t, chainID))
			require.Equal(t, seeds.BootstrapPeers(chainID), got)
			require.Len(t, strings.Split(got, ","), 3)
		})
	}
}

// arctic-1 is a devnet and ships no seeds, so init must leave the field empty.
func TestInitCmdLeavesDevnetBootstrapPeersEmpty(t *testing.T) {
	require.Empty(t, bootstrapPeersLine(t, runInit(t, "arctic-1")))
}

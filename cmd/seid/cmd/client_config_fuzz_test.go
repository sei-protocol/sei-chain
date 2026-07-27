package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/config"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// The server viper is not the only one a seid process runs. Two more resolve
// configuration with different prefixes and different rules, and the differences
// are what make the environment story hard to reason about:
//
//	server viper — prefix path.Base(os.Executable()), "."/"-" to "_" replacer
//	client viper — prefix "SEI", NO replacer
//	global viper — empty prefix, with the replacer
//
// The client viper is where chain-id comes from, and it is the universe with the
// missing replacer. That omission has a consequence no operator would predict, so
// it gets pinned here rather than described in a comment.

// FuzzClientViperDashedKeyHasNoUsableEnvSpelling pins the effect of the client
// viper having no key replacer.
//
// viper builds an env name by upper-casing prefix + "_" + key and then applying the
// replacer. With no replacer installed, the key chain-id yields the literal name
// SEI_CHAIN-ID — a name with a hyphen, which no shell can export through ordinary
// syntax and which nobody writes. The name an operator actually reaches for,
// SEI_CHAIN_ID, is not the name viper looks up, so it does nothing at all.
//
// Silence is the whole problem: the variable is accepted by the environment,
// ignored by the resolver, and the node starts on the chain-id from client.toml.
// This is pinned as behavior, not fixed — adding a replacer would make
// SEI_CHAIN_ID suddenly live on every node that already sets it, which is a
// migration.
func FuzzClientViperDashedKeyHasNoUsableEnvSpelling(f *testing.F) {
	f.Add("sei-chain-from-env")
	f.Add("")
	f.Add("atlantic-2")

	f.Fuzz(func(t *testing.T, envChainID string) {
		if envChainID == "" || !configtest.EnvValueIsSettable(envChainID) {
			return // an empty variable reads as unset, and a NUL cannot be exported
		}
		configtest.Isolate(t)
		home := configtest.NewHome(t)

		// The spelling an operator writes.
		if err := os.Setenv("SEI_CHAIN_ID", envChainID); err != nil {
			t.Fatalf("set SEI_CHAIN_ID: %v", err)
		}

		ctx := client.Context{}.WithHomeDir(home.Root).WithViper("SEI")
		got, err := config.ReadFromClientConfig(ctx)
		if err != nil {
			t.Fatalf("ReadFromClientConfig: %v", err)
		}

		if got.ChainID == envChainID {
			t.Fatalf("SEI_CHAIN_ID took effect (chain-id = %q). The client viper installs no "+
				"key replacer, so the only name it reads is %q; if a replacer was added on "+
				"purpose, that activates SEI_CHAIN_ID on every node already setting it and "+
				"needs a migration", got.ChainID, configtest.ClientEnvKey("chain-id"))
		}
		// ReadFromClientConfig creates client.toml when absent, with chain-id "".
		if got.ChainID != "" {
			t.Fatalf("chain-id resolved to %q, want the empty value from the freshly created "+
				"client.toml", got.ChainID)
		}
	})
}

// TestClientViperEnvNameIsTheUnreplacedSpelling states the mechanism the fuzz target
// above observes: the only env name that can reach a dashed client key is the unreplaced
// one, hyphen and all.
//
// It asserts that against a real client viper rather than against configtest.ClientEnvKey.
// Comparing the helper to a literal would only prove the helper agrees with itself: if
// viper changed how it builds names, or the client viper gained a key replacer, the helper
// would keep returning the same string and the row would keep passing. Setting the name
// and observing it resolve pins the behavior instead of the restatement.
func TestClientViperEnvNameIsTheUnreplacedSpelling(t *testing.T) {
	configtest.Isolate(t)

	const want = "resolved-through-the-hyphenated-name"
	name := configtest.ClientEnvKey("chain-id")
	if err := os.Setenv(name, want); err != nil {
		t.Fatalf("set %s: %v", name, err)
	}

	ctx := client.Context{}.WithViper("SEI")
	if got := ctx.Viper.GetString("chain-id"); got != want {
		t.Fatalf("%s did not reach the client viper (chain-id = %q); if the helper and viper have "+
			"diverged on how the name is built, the helper is what needs updating", name, got)
	}
	if !strings.Contains(name, "-") {
		t.Fatalf("the name that resolves (%q) has no hyphen, so a key replacer is now installed on "+
			"the client viper. That activates SEI_CHAIN_ID on every node already setting it and "+
			"needs a migration", name)
	}

	// And the spelling an operator reaches for still does nothing, even alongside the one
	// that works.
	if err := os.Setenv("SEI_CHAIN_ID", "resolved-through-the-underscored-name"); err != nil {
		t.Fatalf("set SEI_CHAIN_ID: %v", err)
	}
	if got := ctx.Viper.GetString("chain-id"); got != want {
		t.Fatalf("SEI_CHAIN_ID took effect (chain-id = %q); only %q is read", got, name)
	}
}

// TestGlobalViperEnvNameHasNoPrefix pins the third universe's naming. The global
// viper is wired by tmcli.PrepareBaseCmd through InitEnv with an empty prefix and
// the "."/"-" to "_" replacer, so a key's env name is just the upper-cased,
// replaced key with nothing in front of it.
//
// The empty prefix is the sharp part: any bare variable in the environment whose
// name collides with a bound key becomes a configuration source. HOME matches
// home — which is why $HOME outranks the --home flag default in that viper and
// several subcommands resolve the operator's real home directory instead of the
// one they were pointed at — and TRACE, common in CI, matches trace.
func TestGlobalViperEnvNameHasNoPrefix(t *testing.T) {
	cases := map[string]string{
		"home":     "HOME",
		"trace":    "TRACE",
		"output":   "OUTPUT",
		"chain-id": "CHAIN_ID",
	}
	for key, want := range cases {
		if got := configtest.GlobalEnvKey(key); got != want {
			t.Errorf("global env name for %q = %q, want %q", key, got, want)
		}
	}
	// Stated as a relationship so it cannot drift: the global name is the server
	// name with the prefix removed.
	prefix, err := configtest.ServerEnvPrefix()
	if err != nil {
		t.Fatalf("resolve env prefix: %v", err)
	}
	serverName := configtest.ServerEnvKey(prefix, "chain-id")
	if !strings.HasSuffix(serverName, configtest.GlobalEnvKey("chain-id")) {
		t.Fatalf("server name %q does not end in the global name %q; the two universes' "+
			"key transforms have diverged", serverName, configtest.GlobalEnvKey("chain-id"))
	}

	// The names above are the helper's own output, so they are checked against a real
	// global viper too. FuzzGlobalViperBareEnvVarsBecomeConfigSources in
	// sei-tendermint/libs/cli covers home and trace end to end against the singleton
	// PrepareBaseCmd wires, which is the pairing that keeps this row from only proving the
	// helper agrees with itself.
}

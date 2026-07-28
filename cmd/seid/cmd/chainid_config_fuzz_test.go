package cmd

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	clientconfig "github.com/sei-protocol/sei-chain/sei-cosmos/client/config"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// chain-id is the one value whose source of truth is client.toml rather than
// app.toml or config.toml, and the path it takes is the least guessable in the
// surface: start reads client.toml, compares it against --chain-id, and writes the
// winner into the server viper with viper.Set — override precedence, above flags,
// env and both files. Every later appOpts.Get("chain-id") therefore sees that one
// value, and SEID_CHAIN_ID or an app.toml chain-id is silently shadowed.
//
// The file itself has two different missing-file behaviors depending on which
// function opens it, which is what makes "why does my chain-id not apply" hard to
// answer from the outside.

// FuzzClientConfigChainIDRoundTrip pins client.toml as the authority for chain-id.
//
// The value is read verbatim: no trimming, no normalization, no rejection of a
// chain-id that could never exist. That matters because this is the value start
// pins into the server viper at override precedence, so whatever is in this file is
// what the whole process runs with.
//
// It drives GetClientConfig rather than ReadFromClientConfig deliberately.
// ReadFromClientConfig also constructs a keyring from the file, which fails on a
// fixture that names no backend — that is keyring behavior, not chain-id
// resolution, and mixing the two would make a chain-id assertion fail for an
// unrelated reason.
func FuzzClientConfigChainIDRoundTrip(f *testing.F) {
	f.Add("sei-chain")
	f.Add("")
	f.Add("atlantic-2")
	f.Add("  padded  ") // preserved verbatim, not trimmed
	f.Add("unicode-Ω")
	f.Add("123")

	f.Fuzz(func(t *testing.T, chainID string) {
		if !configtest.IsTOMLWritable(chainID) {
			return
		}
		configtest.Isolate(t)
		home := configtest.NewHome(t)
		home.WriteClientTOML(t, []byte("chain-id = \""+chainID+"\"\n"))

		ctx := client.Context{}.WithHomeDir(home.Root).WithViper("SEI")
		got, err := clientconfig.GetClientConfig(home.ConfigDir(), ctx.Viper)
		if err != nil {
			t.Fatalf("GetClientConfig: %v", err)
		}
		if got.ChainID != chainID {
			t.Fatalf("client.toml chain-id = %q resolved to %q; the file is the authority and is "+
				"read verbatim", chainID, got.ChainID)
		}
	})
}

// TestGetClientConfigSearchPathsAccumulate pins the cumulative-AddConfigPath edge on
// the client viper.
//
// GetClientConfig calls AddConfigPath every time it runs, and viper accumulates
// search paths on the instance rather than replacing them. The client viper is
// long-lived — one per client.Context — so a second call for a different home adds
// a path instead of switching to it, and viper resolves the *first* path that
// contains a client.toml. A process that touches two homes therefore reads the
// first one's chain-id for the second one, with nothing to indicate it.
func TestGetClientConfigSearchPathsAccumulate(t *testing.T) {
	configtest.Isolate(t)
	first := configtest.NewHome(t)
	second := configtest.NewHome(t)
	first.WriteClientTOML(t, []byte("chain-id = \"first-home\"\n"))
	second.WriteClientTOML(t, []byte("chain-id = \"second-home\"\n"))

	// One viper, reused across both homes, as a client.Context's viper is.
	ctx := client.Context{}.WithViper("SEI")

	got, err := clientconfig.GetClientConfig(first.ConfigDir(), ctx.Viper)
	if err != nil {
		t.Fatalf("GetClientConfig(first): %v", err)
	}
	if got.ChainID != "first-home" {
		t.Fatalf("first read resolved %q, want first-home", got.ChainID)
	}

	got, err = clientconfig.GetClientConfig(second.ConfigDir(), ctx.Viper)
	if err != nil {
		t.Fatalf("GetClientConfig(second): %v", err)
	}
	if got.ChainID != "first-home" {
		t.Fatalf("the second read resolved %q; search paths accumulate on the viper and the "+
			"first path that holds a client.toml wins, so pointing at a new home does not "+
			"switch away from the old one", got.ChainID)
	}
}

// TestClientConfigAutoCreatesWithEmptyChainID pins the create-when-absent behavior
// and its consequence. A node that has never had a client.toml gets one written for
// it, holding an empty chain-id — so the authoritative chain-id for a fresh home is
// "", and the failure surfaces much later from BaseApp rather than here.
func TestClientConfigAutoCreatesWithEmptyChainID(t *testing.T) {
	configtest.Isolate(t)
	home := configtest.NewHome(t)

	if home.Exists("client.toml") {
		t.Fatal("the fixture must start without a client.toml")
	}

	ctx := client.Context{}.WithHomeDir(home.Root).WithViper("SEI")
	got, readErr := clientconfig.ReadFromClientConfig(ctx)

	// The file is written before the keyring is opened, so the creation this row is about is
	// observable whether or not the keyring is. That separation matters: a freshly created
	// client.toml names the "os" backend, and whether that opens depends on the machine, not
	// on anything pinned here. Asserting on the returned context alone would let a keyring
	// failure be reported as a failure to create the file, which is the same mixing
	// FuzzClientConfigChainIDRoundTrip avoids by not calling this function at all.
	if !home.Exists("client.toml") {
		t.Fatalf("ReadFromClientConfig must write client.toml when it is absent (returned %v)", readErr)
	}

	// The chain-id comes from the created file, through the reader that does not build a
	// keyring.
	conf, err := clientconfig.GetClientConfig(home.ConfigDir(), ctx.Viper)
	if err != nil {
		t.Fatalf("GetClientConfig on the created file: %v", err)
	}
	if conf.ChainID != "" {
		t.Fatalf("a freshly created client.toml must carry an empty chain-id, got %q", conf.ChainID)
	}

	// When the keyring did open, the context it returned has to agree with the file. This is
	// the part that would otherwise go unchecked once the assertion moved off the context.
	if readErr == nil && got.ChainID != conf.ChainID {
		t.Fatalf("ReadFromClientConfig returned chain-id %q while the file it created holds %q",
			got.ChainID, conf.ChainID)
	}
}

// TestGetClientConfigFailsWhenTheFileIsAbsent pins the asymmetry between the two
// readers of one file. ReadFromClientConfig creates client.toml when it is missing;
// GetClientConfig, which `seid config get`/`set` go through, errors instead. Same
// file, same absence, two outcomes — so whether a missing client.toml is a problem
// depends on which command an operator happens to run.
func TestGetClientConfigFailsWhenTheFileIsAbsent(t *testing.T) {
	configtest.Isolate(t)
	home := configtest.NewHome(t)

	ctx := client.Context{}.WithHomeDir(home.Root).WithViper("SEI")
	if _, err := clientconfig.GetClientConfig(home.ConfigDir(), ctx.Viper); err == nil {
		t.Fatal("GetClientConfig must fail on an absent client.toml; it does not create the file, " +
			"unlike ReadFromClientConfig")
	}

	// And the same call succeeds once the file exists, so the difference is the
	// absence itself rather than the directory or the viper.
	home.WriteClientTOML(t, []byte("chain-id = \"sei-chain\"\n"))
	conf, err := clientconfig.GetClientConfig(home.ConfigDir(), ctx.Viper)
	if err != nil {
		t.Fatalf("GetClientConfig on an existing file: %v", err)
	}
	if conf.ChainID != "sei-chain" {
		t.Fatalf("chain-id = %q, want sei-chain", conf.ChainID)
	}
}

// FuzzChainIDSetOverridesEveryOtherLayer pins the precedence consequence of start
// writing chain-id with viper.Set.
//
// Set is override precedence in viper — above a changed flag, above the
// environment, above both files. start resolves chain-id from client.toml and then
// Sets it, so from that point on every appOpts.Get("chain-id") sees the client.toml
// value no matter what else supplied one. An operator who puts chain-id in app.toml,
// or exports SEID_CHAIN_ID, gets no error and no effect.
//
// The Set itself lives in start's PreRunE rather than in Apply, so this target
// reproduces the one line against an Apply-produced viper instead of booting start.
func FuzzChainIDSetOverridesEveryOtherLayer(f *testing.F) {
	f.Add("from-client-toml", "from-app-toml", "from-env")
	f.Add("from-client-toml", "", "")
	f.Add("a", "b", "")
	f.Add("a", "", "c")

	f.Fuzz(func(t *testing.T, fromClient, fromAppTOML, fromEnv string) {
		if fromClient == "" {
			return // start would resolve an empty chain-id, covered above
		}
		for _, v := range []string{fromClient, fromAppTOML, fromEnv} {
			if !configtest.IsTOMLWritable(v) || !configtest.EnvValueIsSettable(v) {
				return
			}
		}
		configtest.Isolate(t)
		home := configtest.NewHome(t)

		if fromAppTOML != "" {
			home.WriteAppTOML(t, []byte("chain-id = \""+fromAppTOML+"\"\n"))
		}
		if fromEnv != "" {
			setServerEnv(t, "chain-id", fromEnv)
		}

		got := applyLegacy(t, home, nil)
		if got.err != nil {
			t.Fatalf("Apply: %v", got.err)
		}

		// The one line start runs after Apply.
		got.ctx.Viper.Set("chain-id", fromClient)

		if resolved := got.ctx.Viper.GetString("chain-id"); resolved != fromClient {
			t.Fatalf("chain-id resolved to %q after Set(%q); Set is override precedence and must "+
				"outrank app.toml (%q) and the environment (%q)", resolved, fromClient, fromAppTOML, fromEnv)
		}
	})
}

// The mismatch panic itself, and the default-home path in its message, are pinned by
// TestStartChainIDMismatchPanics — which drives start's RunE far enough to trip it.

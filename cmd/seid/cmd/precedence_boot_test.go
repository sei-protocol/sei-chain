package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"go.opentelemetry.io/otel/sdk/trace"

	"github.com/sei-protocol/sei-chain/cmd/seid/cmd/configmanager"
	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// Every channel that can carry a value for a declared key has to reach the resolution, in the order
// Precedence declares.
//
// This exists because the same omission happened twice. Both FileLayer and EnvLayer were built and then
// not passed to Resolve, and each time the effect was silent: a value an operator supplied through that
// channel lost to a lower layer, and nothing reported it. A layer that is not wired does not fail, it
// just stops applying, so the only way to know is to supply a value through each channel and require the
// declared order to hold.

// declaredProbeKey is a declared key that no mode varies, so a difference is the channel and not the mode.
const declaredProbeKey = "evm.max_tx_pool_txs"

// bootWithSeiToml writes a sei.toml and returns the source a node would read.
func bootWithSeiToml(t *testing.T, body string) *server.Context {
	t.Helper()
	home := configtest.NewHome(t)
	if err := os.MkdirAll(filepath.Join(home.Root, "config"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if body != "" {
		path := filepath.Join(home.Root, "config", "sei.toml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write sei.toml: %v", err)
		}
	}
	cmd := server.StartCmd(nil, home.Root, []trace.TracerProviderOption{})
	if err := cmd.Flags().Set("home", home.Root); err != nil {
		t.Fatalf("set --home: %v", err)
	}
	ctx, err := runManager(t, configmanager.SeiConfigManager{}, cmd)
	if err != nil {
		t.Fatalf("Apply refused the boot: %v", err)
	}
	return ctx
}

// TestEachChannelWinsOverTheOneBelowIt drives the declared order end to end through a real boot.
func TestEachChannelWinsOverTheOneBelowIt(t *testing.T) {
	baseline := func(t *testing.T) any {
		t.Helper()
		resolved, err := registry.Resolve(registry.ModeValidator)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		res, ok := resolved.From(declaredProbeKey)
		if !ok {
			t.Fatalf("%s is not declared, so this test measures nothing", declaredProbeKey)
		}
		return res.Value
	}

	t.Run("nothing written takes the baseline", func(t *testing.T) {
		configtest.Isolate(t)
		want := baseline(t)
		ctx := bootWithSeiToml(t, "schema_version = 1\nnode_mode = \"validator\"\n")

		if got := ctx.Viper.Get(declaredProbeKey); !sameSetting(got, want) {
			t.Errorf("%s reads %#v with nothing written, want the baseline %#v", declaredProbeKey, got, want)
		}
	})

	t.Run("the file beats the baseline", func(t *testing.T) {
		configtest.Isolate(t)
		ctx := bootWithSeiToml(t, "schema_version = 1\nnode_mode = \"validator\"\n\n[evm]\nmax_tx_pool_txs = 111\n")

		if got := ctx.Viper.Get(declaredProbeKey); !sameSetting(got, int64(111)) {
			t.Errorf("%s reads %#v with 111 written in sei.toml, want 111. A file layer that is not "+
				"passed to the resolution leaves the operator's value silently losing to the baseline",
				declaredProbeKey, got)
		}
	})

	t.Run("the environment beats the file", func(t *testing.T) {
		configtest.Isolate(t)
		// The registry's own spelling, which pins the prefix rather than deriving it from the running
		// binary's name. The legacy path derives it, so the two differ for any binary not called seid.
		t.Setenv(registry.EnvName(declaredProbeKey), "222")
		ctx := bootWithSeiToml(t, "schema_version = 1\nnode_mode = \"validator\"\n\n[evm]\nmax_tx_pool_txs = 111\n")

		if got := ctx.Viper.Get(declaredProbeKey); !sameSetting(got, "222") {
			t.Errorf("%s reads %#v with 111 in sei.toml and 222 in the environment, want 222. An "+
				"environment layer that is not passed leaves the variable silently losing to the file",
				declaredProbeKey, got)
		}
	})
}

// TestNoDeclaredKeyIsAlsoACommandFlag guards the one channel that carries nothing today.
//
// Precedence puts a flag above everything, and the installed value sits in viper's override layer, which
// beats a bound flag. So a declared key that is also a flag would have the declared order inverted: the
// resolution would win and the operator's flag would be ignored.
//
// No declared key is a bound flag today, which is why nothing passes a flag layer. This fails if that
// changes, so the inversion is dealt with deliberately rather than discovered on a node.
func TestNoDeclaredKeyIsAlsoACommandFlag(t *testing.T) {
	declared := registry.Keys()
	if len(declared) == 0 {
		t.Skip("no sections are declared, so there is nothing to compare")
	}

	cmd := server.StartCmd(nil, t.TempDir(), []trace.TracerProviderOption{})
	bound := map[string]bool{}
	collect := func(f *pflag.Flag) { bound[strings.ToLower(f.Name)] = true }
	cmd.Flags().VisitAll(collect)
	cmd.PersistentFlags().VisitAll(collect)
	if len(bound) == 0 {
		t.Fatal("the start command reported no flags, so this comparison holds for any declared set")
	}

	for _, key := range declared {
		if bound[key] {
			t.Errorf("%q is declared and is also a bound command flag. The installed value sits in the "+
				"override layer, which beats a flag, so an operator passing --%s would be ignored while "+
				"Precedence says a flag wins. Pass a flag layer to the resolution, or stop binding the "+
				"flag", key, key)
		}
	}
}

// TestEveryChannelThatCanCarryAValueIsAccountedFor names what the tests above cover, against Precedence.
//
// The point is that adding a channel to Precedence without wiring it is silent. This lists the ones with
// a home and fails when Precedence grows one that has none.
func TestEveryChannelThatCanCarryAValueIsAccountedFor(t *testing.T) {
	covered := map[string]string{
		"default": "the baseline, resolved from each section and covered by the baseline subtest",
		"file":    "sei.toml, covered by the file subtest",
		"env":     "the environment, covered by the environment subtest",
		"flag":    "carries nothing for a declared key, held by TestNoDeclaredKeyIsAlsoACommandFlag",
	}

	for _, source := range registry.Precedence {
		if _, ok := covered[source]; !ok {
			t.Errorf("Precedence declares the %q layer and nothing here covers it. A layer that is not "+
				"passed to the resolution does not fail, it stops applying, so an operator's value in "+
				"that channel would be silently ignored", source)
		}
	}
	for source := range covered {
		if !holds(registry.Precedence, source) {
			t.Errorf("%q is covered here and is not in Precedence, so this test describes a layer that "+
				"no longer exists", source)
		}
	}
}

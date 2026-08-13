package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace"

	"github.com/sei-protocol/sei-chain/cmd/seid/cmd/configmanager"
	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	srvconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
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
	return bootWithSeiTomlAndFlags(t, body, nil)
}

// bootWithSeiTomlAndFlags is bootWithSeiToml with flags the operator typed on the command line.
//
// Set through the command rather than supplied to the resolution directly, because it is the flag being
// marked as changed that the flag layer reads. A value poked into the resolution would hold even if the
// boot never looked at the command.
func bootWithSeiTomlAndFlags(t *testing.T, body string, typed map[string]string) *server.Context {
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
	for name, value := range typed {
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatalf("set --%s=%s: %v", name, value, err)
		}
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

// TestEveryDeclaredKeyAFlagCanDeliverTakesTheFlag is what the flag layer exists to make true.
//
// Precedence puts a flag above everything, and the installed value sits in viper's override layer, which
// beats a bound flag. So a declared key that a flag also delivers has to reach the resolution through the
// flag layer; without it the resolution never sees the command line and then buries it, and an operator
// passing the flag would be silently ignored.
//
// This used to forbid the overlap instead, because nothing filled that channel and no declared key was a
// flag. Declaring state-sync made two of them flags, so the guard became a requirement: every such key is
// booted with the flag set against a file that says something else, and the flag has to win.
func TestEveryDeclaredKeyAFlagCanDeliverTakesTheFlag(t *testing.T) {
	bound := boundStartFlags(t)
	var delivered []string
	for _, key := range registry.Keys() {
		if bound[key] {
			delivered = append(delivered, key)
		}
	}
	if len(delivered) == 0 {
		t.Fatal("no declared key is a bound command flag, so this test covers nothing. If that is now " +
			"true on purpose, the flag layer has no consumer and this should go back to forbidding the " +
			"overlap rather than passing silently")
	}

	for _, key := range delivered {
		t.Run(key, func(t *testing.T) {
			configtest.Isolate(t)
			// Three values this flag's own type accepts, all different, so the winner is the channel and
			// not the only value that parsed.
			inFile, inEnv, onCommandLine, want := valuesFor(t, key)
			// The environment as well, so the flag is shown beating the highest channel below it rather
			// than only beating the file.
			t.Setenv(registry.EnvName(key), inEnv)

			ctx := bootWithSeiTomlAndFlags(t, seiTomlWriting(key, inFile),
				map[string]string{key: onCommandLine})

			if got := ctx.Viper.Get(key); !sameSetting(got, want) {
				t.Errorf("%s reads %#v with %s in sei.toml, %s in the environment and %s on the command "+
					"line, want %v. A flag layer that is not passed to the resolution leaves the "+
					"operator's flag buried under the installed value",
					key, got, inFile, inEnv, onCommandLine, want)
			}
		})
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
		"flag":    "the command line, covered by TestEveryDeclaredKeyAFlagCanDeliverTakesTheFlag",
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

// TestAppTomlDoesNotReachTheFlagLayer is the guard on where the flag snapshot is taken.
//
// The legacy handler copies configuration values into flags, so that a file can supply a flag's default:
// for every flag whose name its viper knows a value for, it calls Set, and Set marks the flag changed.
// After that has run, a flag the operator typed and a key their app.toml holds are indistinguishable.
//
// A flag layer built from that state puts app.toml at the top of the order, above sei.toml, which is a
// worse inversion than the one the layer exists to prevent: the file the operator is being migrated onto
// would lose to the file they are being migrated off. Taking the snapshot at the entry to Apply, before
// the handler runs, is what keeps the two apart, and there is no later point where the truth survives.
func TestAppTomlDoesNotReachTheFlagLayer(t *testing.T) {
	const key = "state-sync.snapshot-keep-recent"
	bound := boundStartFlags(t)
	if !bound[key] {
		t.Skipf("%s is no longer a bound flag, so this cannot happen through it", key)
	}
	configtest.Isolate(t)

	home := configtest.NewHome(t)
	// app.toml holds one value and sei.toml another, and the operator typed no flag at all.
	home.WriteAppTOML(t, []byte("[state-sync]\nsnapshot-keep-recent = 77\n"))
	if err := os.MkdirAll(filepath.Join(home.Root, "config"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "schema_version = 1\nnode_mode = \"validator\"\n\n[state-sync]\nsnapshot-keep-recent = 111\n"
	if err := os.WriteFile(filepath.Join(home.Root, "config", "sei.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write sei.toml: %v", err)
	}

	cmd := server.StartCmd(nil, home.Root, []trace.TracerProviderOption{})
	if err := cmd.Flags().Set("home", home.Root); err != nil {
		t.Fatalf("set --home: %v", err)
	}
	ctx, err := runManager(t, configmanager.SeiConfigManager{}, cmd)
	if err != nil {
		t.Fatalf("Apply refused the boot: %v", err)
	}

	if got := ctx.Viper.Get(key); !sameSetting(got, int64(111)) {
		t.Errorf("%s reads %#v with 77 in app.toml, 111 in sei.toml and no flag typed, want 111.\n\n"+
			"A value of 77 means app.toml arrived through the flag layer, because the handler marked the "+
			"flag changed on its behalf. The snapshot has to be taken before the handler runs", got, key)
	}
}

// seiTomlWriting returns a sei.toml body that writes one key, wherever that key belongs in the file.
//
// A key with no section goes above every table. Once a table heading is open, every bare key after it
// belongs to that table, so a node-wide setting written after one would be read under the wrong name.
func seiTomlWriting(key, value string) string {
	const header = "schema_version = 1\nnode_mode = \"validator\"\n"
	if section, leaf, ok := strings.Cut(key, "."); ok {
		return header + "\n[" + section + "]\n" + leaf + " = " + value + "\n"
	}
	return header + key + " = " + value + "\n"
}

// valuesFor returns three distinct values this key's flag accepts, and what the flag layer will carry.
//
// Derived from the flag's own type rather than written per key. A boolean takes neither 111 nor 333, and a
// flag carrying a list renders a value differently from the text that was set, so what the layer
// contributes is read back from the flag instead of assumed.
func valuesFor(t *testing.T, key string) (inFile, inEnv, onCommandLine, want string) {
	t.Helper()
	cmd := server.StartCmd(nil, t.TempDir(), []trace.TracerProviderOption{})
	f := cmd.Flags().Lookup(key)
	if f == nil {
		t.Fatalf("%q is not a flag on the start command, so this subtest should not have been built", key)
	}

	inFile, inEnv, onCommandLine = "111", "222", "333"
	if f.Value.Type() == "bool" {
		inFile, inEnv, onCommandLine = "false", "false", "true"
	}
	if err := f.Value.Set(onCommandLine); err != nil {
		t.Fatalf("the flag for %q refuses %q, so this subtest needs a value of its type: %v",
			key, onCommandLine, err)
	}
	return inFile, inEnv, onCommandLine, f.Value.String()
}

// TestANodeStartsDespiteAVariableTheEnvironmentCannotDeliver is the divergence this manager takes on.
//
// The metric label set is a list of untyped rows and its reader takes that exact type rather than casting.
// So a variable holding it is a string the reader refuses, and refusing propagates out of the start path:
// the node does not come up. That is true on the machinery this replaces, today, with or without any of
// this.
//
// Here the key is left out of the environment layer, so the file's value is what resolves and the node
// starts. This is the one place a boot succeeds where the legacy path fails, and it is recorded rather than
// discovered. The operator is told separately, by doctor, that their variable does nothing.
func TestANodeStartsDespiteAVariableTheEnvironmentCannotDeliver(t *testing.T) {
	refused := registry.EnvRefusedKeys()
	if len(refused) == 0 {
		t.Skip("no key is refused from the environment, so there is no divergence to hold")
	}
	configtest.Isolate(t)

	for _, key := range refused {
		t.Setenv(registry.EnvName(key), "not-a-value-this-reader-takes")
	}
	ctx := bootWithSeiToml(t, "schema_version = 1\nnode_mode = \"validator\"\n")

	for _, key := range refused {
		got := ctx.Viper.Get(key)
		if text, isText := got.(string); isText && text == "not-a-value-this-reader-takes" {
			t.Errorf("%s reads as the string the variable held. The environment layer carried a key it "+
				"cannot deliver, and the reader refuses that value, so the node does not start", key)
		}
	}

	// The whole point: the configuration the node would build is usable, which it is not when the variable
	// reaches the reader.
	if _, err := srvconfig.GetConfig(ctx.Viper); err != nil {
		t.Errorf("the server configuration cannot be read after a boot with the variable set: %v\n\nThat is "+
			"the failure refusing this channel exists to prevent", err)
	}
}

package cmd

import (
	"reflect"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace"

	"github.com/sei-protocol/sei-chain/cmd/seid/cmd/configmanager"
	"github.com/sei-protocol/sei-chain/config/appopts"
	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// Installing resolved values into a real boot source, measured on the real thing.
//
// Everything else about this is held on a fixture shaped like a boot source. This is here because the
// property that matters is about a source nobody constructed for a test: a real boot viper answers for
// keys it does not enumerate, and the count of those is a fact about this tree rather than about a
// fixture.

// bootSource drives the configuration handler and returns the source a node would read.
func bootSource(t *testing.T) *server.Context {
	t.Helper()
	home := configtest.NewHome(t)
	cmd := server.StartCmd(nil, home.Root, []trace.TracerProviderOption{})
	if err := cmd.Flags().Set(flags.FlagHome, home.Root); err != nil {
		t.Fatalf("set --home: %v", err)
	}
	ctx, err := runManager(t, configmanager.LegacyConfigManager{}, cmd)
	if err != nil {
		t.Fatalf("the handler refused a fresh home (%v), so no source was built", err)
	}
	if ctx.Viper == nil {
		t.Fatal("the handler left the source nil, so every assertion below would hold for any binary")
	}
	return ctx
}

// envNameFor renders a key the way the boot's own replacer does.
func envNameFor(t *testing.T, key string) string {
	t.Helper()
	prefix, err := configtest.ServerEnvPrefix()
	if err != nil {
		t.Fatalf("ServerEnvPrefix: %v", err)
	}
	return strings.ToUpper(strings.NewReplacer(".", "_", "-", "_").Replace(prefix + "." + key))
}

// TestARealBootAnswersForKeysItDoesNotEnumerate is the fact the whole design turns on.
//
// If a boot source listed everything it could answer, resolved values could be carried into a fresh
// source built from that list. It does not, so they cannot, and the count is worth recording because it
// is what makes layering the only safe shape rather than a preference.
func TestARealBootAnswersForKeysItDoesNotEnumerate(t *testing.T) {
	configtest.Isolate(t)
	// A key the construction reads that a generated app.toml does not carry.
	const unenumerated = "evm.max_tx_pool_txs"
	t.Setenv(envNameFor(t, unenumerated), "4242")

	ctx := bootSource(t)

	if got := ctx.Viper.Get(unenumerated); got != "4242" {
		t.Fatalf("%s reads %#v from a real boot source with the environment set, want 4242. Without "+
			"that this test measures nothing", unenumerated, got)
	}
	for _, key := range ctx.Viper.AllKeys() {
		if strings.EqualFold(key, unenumerated) {
			t.Fatalf("%s is enumerable after all. If that has become true, a fresh source built from "+
				"the enumeration would carry it and layering is no longer the only safe shape", key)
		}
	}
	t.Logf("a real boot source enumerates %d keys and answers for at least one more",
		len(ctx.Viper.AllKeys()))
}

// TestInstallingIntoARealBootKeepsTheEnvironmentValue is the end-to-end proof.
//
// This is the case that made building a fresh source unsafe: the operator's value is readable and
// unlistable, so carrying the key space across dropped it and the reader fell back to a code default
// with nothing to show anything had happened. Installing into the source the boot already built cannot
// do that, because the key is never touched.
func TestInstallingIntoARealBootKeepsTheEnvironmentValue(t *testing.T) {
	configtest.Isolate(t)
	const unenumerated = "evm.max_tx_pool_txs"
	t.Setenv(envNameFor(t, unenumerated), "4242")

	ctx := bootSource(t)
	before := ctx.Viper.Get(unenumerated)

	report, err := appopts.Install(ctx.Viper, resolvedForTest(t))
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	if got := ctx.Viper.Get(unenumerated); got != before {
		t.Errorf("%s read %#v before installing and %#v after. This is the operator's value, delivered "+
			"under a key nothing can enumerate, and losing it replaces their setting with a code "+
			"default that nothing reports", unenumerated, before, got)
	}
	t.Logf("%s", report.Summary())
}

// TestInstallingIntoARealBootChangesOnlyDeclaredKeys is the containment claim.
//
// Every key a node reads today has to answer the same value afterwards, apart from the ones a section
// declared. Held over the whole enumerable key space rather than a sample, because the risk is a key
// nobody thought to check.
func TestInstallingIntoARealBootChangesOnlyDeclaredKeys(t *testing.T) {
	configtest.Isolate(t)
	ctx := bootSource(t)
	resolved := resolvedForTest(t)

	before := map[string]any{}
	for _, key := range ctx.Viper.AllKeys() {
		before[strings.ToLower(key)] = ctx.Viper.Get(key)
	}

	if _, err := appopts.Install(ctx.Viper, resolved); err != nil {
		t.Fatalf("Install: %v", err)
	}

	var changed []string
	for key, was := range before {
		if got := ctx.Viper.Get(key); !sameSetting(got, was) {
			changed = append(changed, key)
		}
	}
	for _, key := range changed {
		if _, declared := resolved.Keys[key]; !declared {
			t.Errorf("%q changed and no section declares it. Migrating a section must not move a key "+
				"that was not part of it", key)
		}
	}
	t.Logf("%d of %d enumerable keys changed, all of them declared", len(changed), len(before))
}

// TestInstallingIntoARealBootDoesChangeADeclaredKey is what stops the containment claim being vacuous.
//
// Registering the first section at the defaults a node already runs means installing it changes nothing,
// which is the point of that registration and also means the test above would pass for an install that
// did nothing at all. This one installs a value that differs on purpose and requires exactly that key to
// move, and nothing else.
func TestInstallingIntoARealBootDoesChangeADeclaredKey(t *testing.T) {
	configtest.Isolate(t)
	ctx := bootSource(t)

	// One declared key, resolved to something the boot source does not already say.
	const key = "giga_executor.occ_enabled"
	was := ctx.Viper.Get(key)
	if was == nil {
		t.Fatalf("%s reads nothing from a real boot source, so this test cannot tell a change from a "+
			"first value", key)
	}
	differs := registry.Resolved{Keys: map[string]registry.Resolution{
		key: {Key: key, Value: "a value no baseline produces", From: "default"},
	}}

	before := map[string]any{}
	for _, k := range ctx.Viper.AllKeys() {
		before[strings.ToLower(k)] = ctx.Viper.Get(k)
	}

	if _, err := appopts.Install(ctx.Viper, differs); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if got := ctx.Viper.Get(key); got != "a value no baseline produces" {
		t.Errorf("%s reads %#v after installing a differing value, want the installed one. If an "+
			"install cannot change a key, the containment test above holds for doing nothing", key, got)
	}
	for k, before := range before {
		if k == key {
			continue
		}
		if got := ctx.Viper.Get(k); !sameSetting(got, before) {
			t.Errorf("installing one declared key changed %q from %#v to %#v", k, before, got)
		}
	}
}

// resolvedForTest resolves the sections this binary declares, for a validator.
func resolvedForTest(t *testing.T) registry.Resolved {
	t.Helper()
	resolved, err := registry.Resolve(registry.ModeValidator)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(resolved.Keys) == 0 {
		t.Fatal("this binary declares no sections, so installing changes nothing and the assertions " +
			"about containment hold trivially")
	}
	return resolved
}

// sameSetting reports whether a value read from a configuration source is unchanged.
//
// reflect.DeepEqual rather than a comparison written here. Configuration values include slices, and
// == panics on those rather than returning false, so a hand-written comparison has to enumerate every
// slice type a section might hold and is wrong the moment one is missed.
func sameSetting(a, b any) bool { return reflect.DeepEqual(a, b) }

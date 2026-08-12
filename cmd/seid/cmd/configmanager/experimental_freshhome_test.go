package configmanager

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace"

	"github.com/sei-protocol/sei-chain/config/experimental"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	serverconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// A fresh home grows no experimental section.
//
// The [experimental] table is created by an operator, never by the binary. A section seid wrote
// would read as a value an operator chose, and regenerating the file would then look like an
// intentional change.

// materializeFreshHome runs the real handler on an empty home and returns the home plus the source
// the handler populated.
//
// The handler is what writes config/app.toml on a home that has none, so this drives it rather than
// asserting against a template: a template can say one thing while the writer does another.
func materializeFreshHome(t *testing.T, env map[string]string) (root string, src experimental.Source) {
	t.Helper()
	// Isolate unsets every variable off its allowlist, so anything this test needs delivered has to
	// be set after it rather than before. Set before, it is silently gone and the test reads as the
	// environment channel not working.
	configtest.Isolate(t)
	for k, v := range env {
		t.Setenv(k, v)
	}
	home := configtest.NewHome(t)

	cmd := server.StartCmd(nil, home.Root, []trace.TracerProviderOption{})
	if err := cmd.Flags().Set(flags.FlagHome, home.Root); err != nil {
		t.Fatalf("set --home: %v", err)
	}
	serverCtx := &server.Context{}
	cmd.SetContext(context.WithValue(context.Background(), server.ServerContextKey, serverCtx))

	if err := (LegacyConfigManager{}).Apply(cmd,
		serverconfig.DefaultConfigTemplate, serverconfig.DefaultConfig()); err != nil {
		t.Fatalf("the handler refused a fresh home (%v), so nothing was materialized and this test "+
			"would pass for a binary that wrote the section", err)
	}
	if serverCtx.Viper == nil {
		t.Fatal("the handler left the source nil, so the key-space assertions below would hold for " +
			"any binary at all")
	}
	return home.Root, serverCtx.Viper
}

// TestAFreshHomeGrowsNoExperimentalSection is the first two clauses.
//
// Asserted on the written file and on the resolved key space, because they can disagree: a section
// absent from the file could still be present in the source through a bound flag or a default, and
// a key in the file could be absent from AllKeys if nothing enumerated it.
func TestAFreshHomeGrowsNoExperimentalSection(t *testing.T) {
	root, src := materializeFreshHome(t, nil)

	appTOML := filepath.Join(root, "config", "app.toml")
	raw, err := os.ReadFile(appTOML) //nolint:gosec // a path this test just created under t.TempDir
	if err != nil {
		t.Fatalf("the handler did not write %s (%v), so this test asserts nothing about what it "+
			"writes", appTOML, err)
	}
	if body := string(raw); strings.Contains(body, "["+experimental.Namespace+"]") ||
		strings.Contains(body, experimental.Namespace+".") {
		t.Errorf("the generated app.toml carries the %s table. It is operator-created by contract, and "+
			"a section the binary wrote would read as a value an operator chose",
			experimental.Namespace)
	}

	prefix := experimental.Namespace + "."
	for _, k := range src.AllKeys() {
		if lowered := strings.ToLower(k); lowered == experimental.Namespace || strings.HasPrefix(lowered, prefix) {
			t.Errorf("the resolved key space carries %q on a fresh home. An enumerated key is a sweep "+
				"candidate, so a binary that grew one would report it against itself", k)
		}
	}
}

// TestAFreshHomeSweepsSilent is the consequence the two clauses above exist for.
//
// If a fresh home grew a section, every node would report findings against configuration it had
// never been given. Held through the real reporter, so it covers the render path too.
func TestAFreshHomeSweepsSilent(t *testing.T) {
	_, src := materializeFreshHome(t, nil)

	f := experimental.SweepRegistry(src, "SEID", os.Environ())

	if !f.Empty() {
		t.Errorf("a fresh home swept non-empty: %+v.\n\nEvery node would then report findings against "+
			"configuration nobody wrote, on every application command", f)
	}
}

// TestAnEnvironmentDeliveredKeyStillResolves is the third clause, and it is the asymmetry worth
// understanding rather than tidying away.
//
// The environment is a working delivery channel and an unenumerable one. AllKeys cannot see it, so
// the sweep cannot report an undeclared key delivered that way. Environment delivery is therefore
// unsupported for this namespace rather than half-supported. What must not happen is Get silently
// ignoring a value an operator did supply.
func TestAnEnvironmentDeliveredKeyStillResolves(t *testing.T) {
	experimental.Reset()
	k := experimental.Int(experimental.Decl[int]{
		Name: "freshhome.workers", Default: 8, Owner: "configtest", Since: "v6.6.0",
	})

	// Derived rather than written out, so this cannot pass against a prefix no node uses.
	prefix, err := handlerEnvPrefix()
	if err != nil {
		t.Fatalf("handlerEnvPrefix: %v", err)
	}
	envVar := strings.ToUpper(strings.NewReplacer(".", "_", "-", "_").Replace(prefix + "." + k.Path()))

	_, src := materializeFreshHome(t, map[string]string{envVar: "16"})

	if got := k.Get(src); got != 16 {
		t.Errorf("an environment-delivered key read %d, want the operator's 16. The environment sits "+
			"above the file in the resolution order, so ignoring it here would discard a value an "+
			"operator did supply", got)
	}
	// And it is still invisible to enumeration, which is what makes the sweep's blind spot a
	// documented property rather than a bug someone will "fix" by widening AllKeys.
	for _, key := range src.AllKeys() {
		if strings.EqualFold(key, k.Path()) {
			t.Errorf("%q is enumerable after environment delivery. If that has become true, the sweep "+
				"can see the environment, and the note above about environment delivery being unsupported "+
				"is stale",
				key)
		}
	}
}

// TestTheEnvPrefixDerivationsAgree makes the duplication safe rather than merely noted.
//
// Three copies of path.Base(os.Executable()) exist: this package's handlerEnvPrefix, which
// production uses and which cannot import configtest; configtest.ServerEnvPrefix, which the harness
// exposes for the same reason; and resolveHomeDir's inline one. Production cannot collapse them
// while this change is not permitted to modify anything under sei-cosmos, so the next best thing is
// a test that fails the moment they disagree. If they ever do, the shadow pass looks for variables
// under a prefix no node uses and silently reports nothing.
func TestTheEnvPrefixDerivationsAgree(t *testing.T) {
	mine, err := handlerEnvPrefix()
	if err != nil {
		t.Fatalf("handlerEnvPrefix: %v", err)
	}
	harness, err := configtest.ServerEnvPrefix()
	if err != nil {
		t.Fatalf("configtest.ServerEnvPrefix: %v", err)
	}
	if mine != harness {
		t.Errorf("the production derivation gives %q and the harness gives %q. Every variable the "+
			"sweep looks for is built from the first, and every one a characterization test asserts is "+
			"built from the second", mine, harness)
	}
}

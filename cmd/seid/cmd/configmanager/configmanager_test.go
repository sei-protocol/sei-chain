package configmanager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace"

	seiconfig "github.com/sei-protocol/sei-config"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	serverconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// TestSelect covers the dispatch table: unset and "legacy" select the
// LegacyConfigManager, "v2" selects the SeiConfigManager, and any other
// value is a hard error (no silent fallback).
func TestSelect(t *testing.T) {
	cases := []struct {
		name    string
		val     string
		want    ConfigManager
		wantErr bool
	}{
		{name: "unset", val: "", want: LegacyConfigManager{}},
		{name: "legacy", val: "legacy", want: LegacyConfigManager{}},
		{name: "v2", val: "v2", want: SeiConfigManager{}},
		{name: "garbage", val: "v3", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr, err := Select(func(string) string { return tc.val })
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.IsType(t, tc.want, mgr)
		})
	}
}

// TestResolveHomeDir_Flag confirms resolveHomeDir reads the --home flag. That the
// value it returns is the same one the re-entered handler reads is a separate
// property, asserted in TestResolveHomeDirAgreesWithTheLegacyHandler.
func TestResolveHomeDir_Flag(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String(flags.FlagHome, "", "")
	require.NoError(t, cmd.Flags().Set(flags.FlagHome, "/tmp/seid-test-home"))

	got, err := resolveHomeDir(cmd)
	require.NoError(t, err)
	require.Equal(t, "/tmp/seid-test-home", got)
}

// TestResolveHomeDirEnvAndPrecedence covers the two cells the flag case above does
// not: the home arriving through the environment, and an explicit flag outranking it.
//
// The key is derived from the running binary's basename rather than spelled as
// SEID_HOME, because that is how both resolveHomeDir and the legacy handler build it.
// A change that hardcoded "seid" would still satisfy a literal-key test while
// silently ceasing to answer the environment under any other binary name, and the
// test binary is never named seid, so deriving the key is what makes this real.
//
// This asserts resolveHomeDir alone, against literal expectations. Agreement with the
// home the legacy handler reads is the lockstep property, and it is asserted against
// the real handler in TestResolveHomeDirAgreesWithTheLegacyHandler.
func TestResolveHomeDirEnvAndPrecedence(t *testing.T) {
	prefix, err := configtest.ServerEnvPrefix()
	require.NoError(t, err)
	envKey := configtest.ServerEnvKey(prefix, flags.FlagHome)

	t.Run("the environment supplies the home", func(t *testing.T) {
		configtest.Isolate(t)
		t.Setenv(envKey, "/tmp/seid-env-home")

		cmd := &cobra.Command{}
		cmd.Flags().String(flags.FlagHome, "", "")

		got, err := resolveHomeDir(cmd)
		require.NoError(t, err)
		require.Equal(t, "/tmp/seid-env-home", got,
			"an unchanged flag default ranks below AutomaticEnv, so %s resolves the home", envKey)
	})

	t.Run("an explicit flag outranks the environment", func(t *testing.T) {
		configtest.Isolate(t)
		t.Setenv(envKey, "/tmp/seid-env-home")

		cmd := &cobra.Command{}
		cmd.Flags().String(flags.FlagHome, "", "")
		require.NoError(t, cmd.Flags().Set(flags.FlagHome, "/tmp/seid-flag-home"))

		got, err := resolveHomeDir(cmd)
		require.NoError(t, err)
		require.Equal(t, "/tmp/seid-flag-home", got,
			"a changed flag outranks the environment in viper's precedence")
	})
}

// TestResolveHomeDirAgreesWithTheLegacyHandler is the lockstep assertion: the home v2
// validates has to be the home the handler it re-enters actually reads.
//
// No test outside this one can reach that property. v2's channels are produced
// entirely by the legacy handler, so a resolveHomeDir that drifted from the handler
// would only cause the advisory read to skip or warn, which changes no channel and
// fails no parity assertion anywhere. The result is that v2 would report diagnostics
// about one directory while the node booted on another, which is the silent drift the
// advisory design cannot surface on its own. So it is asserted directly, against the
// real handler, for both ways a home arrives.
func TestResolveHomeDirAgreesWithTheLegacyHandler(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, cmd *cobra.Command, root string)
	}{
		{"home from the flag", func(t *testing.T, cmd *cobra.Command, root string) {
			require.NoError(t, cmd.Flags().Set(flags.FlagHome, root))
		}},
		{"home from the environment", func(t *testing.T, cmd *cobra.Command, root string) {
			prefix, err := configtest.ServerEnvPrefix()
			require.NoError(t, err)
			t.Setenv(configtest.ServerEnvKey(prefix, flags.FlagHome), root)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			configtest.Isolate(t)
			root := filepath.Join(t.TempDir(), "node")
			require.NoError(t, os.MkdirAll(root, 0o750))

			// A real StartCmd, so the flag set and its defaults are the ones seid ships.
			cmd := server.StartCmd(nil, "/foobar", []trace.TracerProviderOption{})
			tc.setup(t, cmd, root)

			// Captured before the handler runs, which is both what Apply does and what makes
			// this an assertion about resolveHomeDir. The handler ends in bindFlags, which
			// writes the env-resolved value back onto the --home flag and marks it Changed
			// (sei-cosmos/server/util.go), so a resolveHomeDir read afterwards would be
			// reading that flag rather than resolving anything, and a version that had
			// dropped SetEnvPrefix and AutomaticEnv entirely would still pass.
			got, err := resolveHomeDir(cmd)
			require.NoError(t, err)

			serverCtx := &server.Context{}
			cmd.SetContext(context.WithValue(context.Background(), server.ServerContextKey, serverCtx))

			// The real handler, which is what v2 re-enters and therefore what it must agree with.
			require.NoError(t, LegacyConfigManager{}.Apply(cmd,
				serverconfig.DefaultConfigTemplate, serverconfig.DefaultConfig()))

			handlerHome := serverCtx.Viper.GetString(flags.FlagHome)
			require.Equal(t, root, handlerHome,
				"the fixture did not drive the handler's resolution, so this comparison would be vacuous")

			require.Equal(t, handlerHome, got,
				"resolveHomeDir has drifted from the legacy handler: v2 would validate %q while "+
					"the node boots on %q, and no parity assertion can see it", got, handlerHome)
		})
	}
}

// writeMinimalHome creates a node directory carrying the two files
// ReadConfigFromDir decodes, with the given contents, and returns its root.
//
// The files are written by hand rather than generated by the legacy creator, so the
// input is exactly what the test says it is. A generated config would carry
// machine-derived values (the worker counts, the hostname moniker) and template
// literals that move between releases, and an assertion about validation should not
// depend on either.
func writeMinimalHome(t *testing.T, configTOML, appTOML string) string {
	t.Helper()
	h := configtest.NewHome(t)
	h.WriteConfigTOML(t, []byte(configTOML))
	h.WriteAppTOML(t, []byte(appTOML))
	return h.Root
}

// homeCmd returns a command whose --home is set to root.
func homeCmd(t *testing.T, root string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.Flags().String(flags.FlagHome, "", "")
	require.NoError(t, cmd.Flags().Set(flags.FlagHome, root))
	return cmd
}

// TestValidateAdvisoryReportsWhatItFound is the positive case for the validation
// pass, and it is the one claim the parity assertions cannot make.
//
// Channel parity and never-refuses-boot both hold just as well when the pass does
// nothing at all: if the read always failed, or validation degraded to returning no
// diagnostics after a sei-config bump, every one of those assertions would still
// pass. This one fails, because it names a finding the pass has to produce.
func TestValidateAdvisoryReportsWhatItFound(t *testing.T) {
	configtest.Isolate(t)

	// app.toml carries no minimum-gas-prices, which sei-config reports against
	// chain.min_gas_prices. A named field is asserted rather than a count, so an
	// unrelated diagnostic appearing or disappearing does not make this pass or fail
	// for the wrong reason.
	root := writeMinimalHome(t, "mode = \"full\"\n", "")

	out := validateAdvisory(homeCmd(t, root))

	require.Nil(t, out.Panic, "the pass panicked: %v\n%s", out.Panic, out.Stack)
	require.NoError(t, out.Err, "the pass could not read the config it was pointed at")
	require.False(t, out.Skipped, "the pass skipped a home that carries both config files")
	require.Equal(t, root, out.Home, "the pass validated a different directory than it was given")

	require.NotEmpty(t, out.Diagnostics,
		"validation produced no findings on a config missing a required field, so the "+
			"pass has become a no-op and every parity assertion elsewhere holds vacuously")
	require.Contains(t, strings.Join(out.Diagnostics, "\n"), "chain.min_gas_prices",
		"validation ran but did not report the missing required field")
}

// TestValidateAdvisorySkipsAnUnresolvedHome pins the guard on an empty home.
//
// resolveHomeDir returns "" with no error when --home is absent, and a read rooted at
// "" resolves ./config relative to the process working directory. That would validate
// whatever node happens to live there and report diagnostics about a config this node
// is not booting on, which is worse than reporting nothing from a pass that exists to
// inform an operator.
func TestValidateAdvisorySkipsAnUnresolvedHome(t *testing.T) {
	configtest.Isolate(t)

	out := validateAdvisory(&cobra.Command{}) // no --home registered at all

	require.True(t, out.Skipped, "an unresolved home must be declined, not read")
	require.Empty(t, out.Home, "nothing may be recorded as validated")
	require.Empty(t, out.Diagnostics, "a declined pass must not report findings")
	require.NoError(t, out.Err)
	require.Nil(t, out.Panic)
}

// TestCapDiagnostics pins the truncation arithmetic at its boundary. A miscount here
// would misreport how much was left out of a log line, which is the kind of defect
// nobody notices by reading the output.
func TestCapDiagnostics(t *testing.T) {
	diags := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("d%d", i)
		}
		return out
	}
	cases := []struct {
		name        string
		in          int
		wantShown   int
		wantOmitted int
	}{
		{"none", 0, 0, 0},
		{"one", 1, 1, 0},
		{"exactly at the cap", maxLoggedDiagnostics, maxLoggedDiagnostics, 0},
		{"one over the cap", maxLoggedDiagnostics + 1, maxLoggedDiagnostics, 1},
		{"far over the cap", maxLoggedDiagnostics + 15, maxLoggedDiagnostics, 15},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := diags(tc.in)
			shown, omitted := capDiagnostics(in)

			require.Len(t, shown, tc.wantShown)
			require.Equal(t, tc.wantOmitted, omitted)
			// Nothing may be invented or reordered, and the two parts must account for
			// the whole list, which is what makes the reported count trustworthy.
			require.Equal(t, tc.in, len(shown)+omitted, "shown plus omitted must be the whole list")
			for i := range shown {
				require.Equal(t, in[i], shown[i], "the rendered part must be the first %d in order", tc.wantShown)
			}
		})
	}
}

// TestLogAdvisoryHandlesEveryOutcome exercises the branches no other test reaches: a
// recovered panic, a failure at each stage, and a truncated diagnostic list. It
// asserts the reporting path survives each shape rather than asserting log text, since
// the text is not a contract; a panic or nil dereference in the reporter would turn an
// advisory pass into a boot failure, which is the one thing it must never do.
func TestLogAdvisoryHandlesEveryOutcome(t *testing.T) {
	many := make([]string, maxLoggedDiagnostics+3)
	for i := range many {
		many[i] = fmt.Sprintf("[ERROR] field%d: broken", i)
	}
	cases := []struct {
		name string
		out  advisoryOutcome
	}{
		{"zero value", advisoryOutcome{}},
		{"skipped", advisoryOutcome{Skipped: true}},
		{"resolve failed", advisoryOutcome{Stage: stageResolve, Err: errors.New("no home")}},
		{"read failed", advisoryOutcome{Stage: stageRead, Err: errors.New("bad toml")}},
		{"panicked", advisoryOutcome{Panic: "boom", Stack: []byte("goroutine 1 [running]:\n")}},
		{"panicked with no stack", advisoryOutcome{Panic: errors.New("boom")}},
		{"one diagnostic", advisoryOutcome{Home: "/tmp/n", Diagnostics: []string{"[ERROR] a: b"}}},
		{"more diagnostics than the cap", advisoryOutcome{Home: "/tmp/n", Diagnostics: many}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NotPanics(t, func() { logAdvisory(tc.out) })
		})
	}
}

// TestReadConfigFromDirMissingIsErrNotExist pins the contract validateAdvisory's
// silent-skip depends on: a missing config file must yield an error that
// errors.Is(os.ErrNotExist) recognizes, so a fresh-home boot skips the advisory
// read quietly instead of logging a spurious warning. If sei-config ever swaps
// to a custom not-found error, this fails here rather than going noisy in prod.
func TestReadConfigFromDirMissingIsErrNotExist(t *testing.T) {
	_, err := seiconfig.ReadConfigFromDir(t.TempDir())
	require.ErrorIs(t, err, os.ErrNotExist)
}

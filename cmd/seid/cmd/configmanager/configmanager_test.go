package configmanager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace"

	seiconfig "github.com/sei-protocol/sei-config"
	"github.com/sei-protocol/seilog"

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
		{"exactly at the cap", maxLoggedItems, maxLoggedItems, 0},
		{"one over the cap", maxLoggedItems + 1, maxLoggedItems, 1},
		{"far over the cap", maxLoggedItems + 15, maxLoggedItems, 15},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := diags(tc.in)
			shown, omitted := capLoggedItems(in)

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
//
// It reports through the production logger, which is what the zero-value manager
// resolves to, so the every-outcome table covers the logger a node actually boots with.
// The other tests here inject their own, and without this one nothing would construct
// the package logger at all: a nil or broken one would ship unnoticed, and a nil
// *slog.Logger panics on use rather than discarding, so it would refuse a boot.
func TestLogAdvisoryHandlesEveryOutcome(t *testing.T) {
	lg := SeiConfigManager{}.log()
	require.NotNil(t, lg, "the zero-value manager must resolve to a usable logger")

	many := make([]string, maxLoggedItems+3)
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
			require.NotPanics(t, func() { logAdvisory(lg, tc.out) })
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

// TestReportAdvisoryNeverEscapesWhenTheLoggerPanics exercises the manager's one hard
// promise: a panic in the advisory path must never propagate out of Apply into
// PersistentPreRunE and refuse a boot the legacy path would have allowed.
//
// The logger is the subsystem this defends against. validateAdvisory recovers its own
// panics (TestValidateAdvisoryReportsWhatItFound asserts a clean pass), and logAdvisory
// is proven panic-free on every outcome (TestLogAdvisoryHandlesEveryOutcome), so the
// remaining exposure is a logger broken independent of its arguments. A logger whose
// handler panics on every record makes both the reporting call and the recover handler's
// own log call panic; reportAdvisory must still return normally.
//
// The broken logger is passed in rather than assigned over the package one. Reassigning
// it would leave a future parallel test in this package racing the swap, and nothing here
// needs the package logger to change: the reporter takes the logger it reports through.
func TestReportAdvisoryNeverEscapesWhenTheLoggerPanics(t *testing.T) {
	configtest.Isolate(t)
	// A config missing a required field, so the pass produces a diagnostic and
	// logAdvisory reaches a logger call rather than the quiet fresh-node skip.
	root := writeMinimalHome(t, "mode = \"full\"\n", "")
	out := validateAdvisory(homeCmd(t, root))

	require.NotPanics(t, func() { reportAdvisory(slog.New(panickingHandler{}), out) })
}

// panickingHandler is a slog.Handler that panics on every record, standing in for a
// logger broken independent of its arguments (a bad handler, a writer on a closed fd).
type panickingHandler struct{}

func (panickingHandler) Enabled(context.Context, slog.Level) bool  { return true }
func (panickingHandler) Handle(context.Context, slog.Record) error { panic("logger handler is broken") }
func (panickingHandler) WithAttrs([]slog.Attr) slog.Handler        { return panickingHandler{} }
func (panickingHandler) WithGroup(string) slog.Handler             { return panickingHandler{} }

// TestApplyRunsTheValidationPass asserts the one claim that distinguishes v2 from the
// legacy manager: that Apply actually runs the validation pass and reports it.
//
// Nothing else reaches this. The parity rows compare two runs of the same reader and
// still match if the pass never runs; the never-refuses-boot tests only assert that a
// boot succeeds; and the advisory tests call validateAdvisory and reportAdvisory
// directly. Deleting the two lines in Apply that call them makes v2 byte-identical to
// the legacy manager, and every one of those tests stays green. This one fails, because
// it drives the real Apply and names a finding the pass has to produce.
func TestApplyRunsTheValidationPass(t *testing.T) {
	configtest.Isolate(t)
	// A config missing a required field, so validation has a finding to report. app.toml
	// is written as well: without it the read fails with ErrNotExist, the pass skips
	// silently, and there would be nothing to distinguish that from the wiring being gone.
	root := writeMinimalHome(t, "mode = \"full\"\n", "")

	// A real StartCmd, and one whose default home is non-empty. With an empty default an
	// unresolved --home reports the no-home skip instead, so an assertion that merely
	// counted records would hold even on a fixture that never reached the config.
	cmd := server.StartCmd(nil, "/foobar", []trace.TracerProviderOption{})
	require.NoError(t, cmd.Flags().Set(flags.FlagHome, root))
	serverCtx := &server.Context{}
	cmd.SetContext(context.WithValue(context.Background(), server.ServerContextKey, serverCtx))

	capture := &capturingHandler{}
	require.NoError(t, SeiConfigManager{logger: slog.New(capture)}.Apply(cmd,
		serverconfig.DefaultConfigTemplate, serverconfig.DefaultConfig()),
		"the fixture is meant to boot, so a failure here means the fixture, not the pass")

	// Named by diagnostic rather than by record count, and it is the same finding
	// TestValidateAdvisoryReportsWhatItFound anchors on, so a sei-config change moves
	// both together instead of leaving this one asserting that something was logged.
	var found *slog.Record
	var seen []string
	for i := range capture.records {
		line := renderRecord(capture.records[i])
		seen = append(seen, line)
		if strings.Contains(line, "chain.min_gas_prices") {
			found = &capture.records[i]
		}
	}
	require.NotNil(t, found,
		"Apply reported no diagnostic naming chain.min_gas_prices, so the validation pass "+
			"is not wired into it; records seen: %v", seen)
	require.Equal(t, slog.LevelWarn, found.Level,
		"the diagnostic line is the operator-facing one and has to survive a node's level")
	require.Contains(t, renderRecord(*found), "home="+root,
		"the line has to name the directory validated, or a drifted resolve is invisible")
}

// TestApplyPropagatesALegacyHandlerPanic asserts the abort direction of the manager's
// promise: v2 must not turn a boot the legacy path refuses into one that succeeds.
//
// Every other test asserts the permissive direction, that a boot legacy allows still
// happens. This one exists because the advisory path holds a recover, and the reporting
// call in Apply is deliberately not deferred for that reason. A deferred report whose
// recover had been hoisted into reportAdvisory's own body would swallow the handler's
// panic and return nil, which is exactly this assertion failing.
//
// The fixture drives a real panic rather than simulating one. A home under a
// non-writable parent is absent as far as the handler's stat is concerned, so it takes
// the fresh-node branch, and the directory creation inside that branch then fails on
// permissions, which the legacy handler raises as a panic rather than an error.
func TestApplyPropagatesALegacyHandlerPanic(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, which ignores the directory permissions this relies on")
	}
	configtest.Isolate(t)

	readOnly := filepath.Join(t.TempDir(), "read-only")
	require.NoError(t, os.MkdirAll(readOnly, 0o500))
	// Restored so the temp-dir cleanup can remove it.
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o700) })

	cmd := server.StartCmd(nil, "/foobar", []trace.TracerProviderOption{})
	require.NoError(t, cmd.Flags().Set(flags.FlagHome, filepath.Join(readOnly, "node")))
	serverCtx := &server.Context{}
	cmd.SetContext(context.WithValue(context.Background(), server.ServerContextKey, serverCtx))

	require.Panics(t, func() {
		_ = SeiConfigManager{logger: slog.New(&capturingHandler{})}.Apply(cmd,
			serverconfig.DefaultConfigTemplate, serverconfig.DefaultConfig())
	}, "a panic from the legacy handler must reach the caller rather than being recovered "+
		"into a successful boot")
}

// capturingHandler records what was logged so a test can assert on it without
// reassigning the package logger.
type capturingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

// Enabled admits every level, so a record cannot be dropped by the level the legacy
// handler applies partway through Apply and leave an assertion looking at nothing.
func (h *capturingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Cloned because slog only lends a record for the duration of the call.
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *capturingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *capturingHandler) WithGroup(string) slog.Handler      { return h }

// renderRecord flattens a record's message and attributes into one line, so an
// assertion can name a value without knowing which attribute carries it.
func renderRecord(r slog.Record) string {
	var b strings.Builder
	b.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		b.WriteString(" ")
		b.WriteString(a.Key)
		b.WriteString("=")
		b.WriteString(a.Value.String())
		return true
	})
	return b.String()
}

// TestTheReportingFloorSurvivesALevelSetAcrossEveryLogger is what keeps this manager audible.
//
// The handler this manager re-enters sets one level across every logger in the process, from a key an
// operator writes. A fleet that runs its nodes quiet sets it above the level these reports use, and every
// outcome here is a report: what was applied, what was held back, what was refused and what had no effect.
// Silenced, the manager changes what a node runs and says nothing about it.
//
// The level is asserted rather than a message, because a message can be absent for reasons that have
// nothing to do with whether it would have been printed.
func TestTheReportingFloorSurvivesALevelSetAcrossEveryLogger(t *testing.T) {
	// What a node whose file says log_level = "error" produces.
	seilog.SetDefaultLevel(slog.LevelError, true)
	t.Cleanup(func() { seilog.SetDefaultLevel(slog.LevelInfo, true) })

	if OwnReportingEnabledForTest() {
		t.Fatal("this package's reporting is still on after a level was set across every logger, so " +
			"this test cannot show the floor being restored")
	}

	keepOwnReportingVisible()

	if !OwnReportingEnabledForTest() {
		t.Error("this package's reporting is off after the floor was applied. Every outcome here is a " +
			"report, so the manager would change what a node runs and say nothing about it")
	}
}

// TestTheFloorDoesNotLowerALevelAnOperatorRaised is the other direction of the same property.
//
// It is a floor, not a level. On a node run at debug, the lines this manager emits below the floor are
// exactly the ones somebody turned the level up to see: that there is no file, and what an ordinary
// invocation held back or installed. Assigning the floor would drop all of them.
func TestTheFloorDoesNotLowerALevelAnOperatorRaised(t *testing.T) {
	// What a node whose file says log_level = "debug" produces.
	seilog.SetDefaultLevel(slog.LevelDebug, true)
	t.Cleanup(func() { seilog.SetDefaultLevel(slog.LevelInfo, true) })

	if !logger.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("this package is not emitting at debug after a level was set across every logger, so " +
			"this test cannot show the floor leaving it alone")
	}

	keepOwnReportingVisible()

	if !logger.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("this package stopped emitting at debug after the floor was applied, so an operator who " +
			"raised the level to work out why their file does nothing lost the lines that would say")
	}
}

// TestTheFloorAddressesALoggerThatExists holds the derived name to matching what was registered.
//
// The floor is applied by name. A name that matches no registered logger applies nothing and returns zero,
// and the only symptom is this package going quiet exactly when an operator raised the level to see it.
func TestTheFloorAddressesALoggerThatExists(t *testing.T) {
	if got := seilog.SetLevel(loggerName, ownReportingFloor); got == 0 {
		t.Errorf("%q matches no registered logger, so the floor is applied to nothing", loggerName)
	}
}

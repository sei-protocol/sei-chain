package configmanager

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel/sdk/trace"

	"github.com/sei-protocol/sei-chain/config/experimental"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// Gate 5 of PR3: experimental semantics run under both configuration managers.
//
// This is the constraint that put experimental first in the stack. The design ships these
// semantics in both managers, not behind SEI_CONFIG_MANAGER=v2, because they are the minimal
// unblock for values that change between binaries and cannot wait for v2 adoption. A gate
// that exercised one manager would let the framework ship gated by accident.
//
// It lives here rather than in config/experimental because it drives Apply, and only the
// managers have one.

// gate5Key is declared for this gate alone. A key declared in the package under test would
// couple two files' registrations, and a duplicate declaration panics.
var gate5Key = experimental.Int("configmanager.gate5", 3, experimental.Owner("configtest"))

// captureLog returns a logger writing into buf, so a test can read what Apply reported without
// reassigning package state that a parallel test could race.
func captureLog() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

// applyOverExperimental drives a manager's Apply on a home whose app.toml carries the given
// experimental keys, and returns what it logged.
//
// The keys go into the file rather than into a pre-seeded viper, because the handler builds
// that viper itself. Pre-seeding one would test a state the boot never reaches. This also makes
// the gate exercise the real path an operator uses: TOML on disk, merged by the handler, read
// back through the section prefix.
//
// A real StartCmd, so the flag set and its defaults are the ones seid ships.
func applyOverExperimental(
	t *testing.T,
	apply func(lg *slog.Logger, cmd *cobra.Command) error,
	kv map[string]string,
) (string, error) {
	t.Helper()
	configtest.Isolate(t)
	home := configtest.NewHome(t)

	if len(kv) > 0 {
		cfgDir := filepath.Join(home.Root, "config")
		if err := os.MkdirAll(cfgDir, 0o750); err != nil {
			t.Fatalf("mkdir config: %v", err)
		}
		var b strings.Builder
		b.WriteString("[" + experimental.Section + "]\n")
		for k, v := range kv {
			// Quoted, because a dotted key inside a table is one literal name in TOML. Viper
			// flattens it back to the full dotted path, which is what Check enumerates.
			fmt.Fprintf(&b, "%q = %q\n", k, v)
		}
		if err := os.WriteFile(filepath.Join(cfgDir, "app.toml"), []byte(b.String()), 0o600); err != nil {
			t.Fatalf("write app.toml: %v", err)
		}
	}

	cmd := server.StartCmd(nil, home.Root, []trace.TracerProviderOption{})
	if err := cmd.Flags().Set(flags.FlagHome, home.Root); err != nil {
		t.Fatalf("set --home: %v", err)
	}
	serverCtx := &server.Context{}
	cmd.SetContext(context.WithValue(context.Background(), server.ServerContextKey, serverCtx))

	lg, buf := captureLog()
	err := apply(lg, cmd)

	// Without this the gate would be vacuous: a handler that never populated the viper would
	// leave nothing for the check pass to enumerate, and every assertion below would read as
	// "not reported" for the wrong reason.
	if serverCtx.Viper == nil {
		t.Fatal("the handler left serverCtx.Viper nil, so the experimental check had no key space " +
			"to read and this gate proves nothing")
	}
	return buf.String(), err
}

// TestGate5BothManagersReportExperimentalFindings is the gate.
//
// Each manager is driven over the same configuration and has to report the same finding. The
// assertion is on the reported output rather than on a call count, because a manager that
// called the check pass and discarded its result would satisfy a count.
func TestGate5BothManagersReportExperimentalFindings(t *testing.T) {
	const unknownKey = "configmanager.from_a_later_release"

	for _, tc := range []struct {
		manager string
		apply   func(lg *slog.Logger, cmd *cobra.Command) error
	}{
		{"legacy", func(lg *slog.Logger, cmd *cobra.Command) error {
			return LegacyConfigManager{logger: lg}.Apply(cmd, "", nil)
		}},
		{"v2", func(lg *slog.Logger, cmd *cobra.Command) error {
			return SeiConfigManager{logger: lg}.Apply(cmd, "", nil)
		}},
	} {
		t.Run(tc.manager, func(t *testing.T) {
			// The handler's own outcome is not this gate's subject. What matters is that the
			// experimental report happened either way, since a boot that fails still wants the
			// operator-facing lines.
			out, _ := applyOverExperimental(t, tc.apply, map[string]string{
				unknownKey:     "42",
				gate5Key.Key(): "not-a-number",
			})
			if !strings.Contains(out, unknownKey) {
				t.Errorf("the %s manager did not report the unrecognized key %q. These semantics ship "+
					"in both managers, because they are the unblock for values that change between "+
					"binaries and cannot wait for v2 to become the default.\nlogged: %s",
					tc.manager, unknownKey, out)
			}
			if !strings.Contains(out, gate5Key.Key()) {
				t.Errorf("the %s manager did not report the declared key whose value does not convert. "+
					"A declared type that is never checked at boot makes Handle.Get's fall back to its "+
					"default a silent substitution.\nlogged: %s", tc.manager, out)
			}
		})
	}
}

// TestGate5NeitherManagerHaltsOnAnExperimentalFinding is the other half, and without it gate 5
// would hold for a manager that refused the boot.
//
// Warn, never halt. A configuration written for the next release has to stay bootable on this
// one, and an unconvertible value on an experimental key is reported rather than fatal.
func TestGate5NeitherManagerHaltsOnAnExperimentalFinding(t *testing.T) {
	for _, tc := range []struct {
		manager string
		apply   func(lg *slog.Logger, cmd *cobra.Command) error
	}{
		{"legacy", func(lg *slog.Logger, cmd *cobra.Command) error {
			return LegacyConfigManager{logger: lg}.Apply(cmd, "", nil)
		}},
		{"v2", func(lg *slog.Logger, cmd *cobra.Command) error {
			return SeiConfigManager{logger: lg}.Apply(cmd, "", nil)
		}},
	} {
		t.Run(tc.manager, func(t *testing.T) {
			_, cleanErr := applyOverExperimental(t, tc.apply, nil)
			_, dirtyErr := applyOverExperimental(t, tc.apply, map[string]string{
				"configmanager.from_a_later_release": "42",
				gate5Key.Key():                       "not-a-number",
			})

			// The comparison is relative. Whether Apply errors here depends on the handler and the
			// test's home, and this gate's subject is only that experimental findings do not change
			// the verdict.
			if (cleanErr == nil) != (dirtyErr == nil) {
				t.Errorf("the %s manager's verdict changed when experimental findings were present: "+
					"clean=%v dirty=%v. An unrecognized or unconvertible experimental key must never "+
					"be the reason a boot fails, or a config written for the next release stops "+
					"booting on this one", tc.manager, cleanErr, dirtyErr)
			}
		})
	}
}

// TestGate5ReportingSurvivesAContextWithNoViper holds the degenerate path.
//
// The report reads serverCtx.Viper, and a command whose context never reached the handler
// carries nil. A manager whose only output is operator-facing lines must not be the reason a
// boot panics.
func TestGate5ReportingSurvivesAContextWithNoViper(t *testing.T) {
	lg, buf := captureLog()

	// A bare command, so GetServerContextFromCmd finds no context of ours.
	reportExperimental(lg, &cobra.Command{Use: "start"})
	// And an explicit nil, which a caller could reach through a zero value.
	reportExperimental(lg, nil)

	if strings.Contains(buf.String(), "panicked") {
		t.Errorf("reporting panicked on a context with no viper and recovered, which means the "+
			"nil case is being handled by the recover rather than by a check.\nlogged: %s", buf.String())
	}
}

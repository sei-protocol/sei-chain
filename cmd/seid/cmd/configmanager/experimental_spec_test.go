package configmanager

import (
	"bytes"
	"context"
	"fmt"
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
// The design ships these semantics in both managers rather than behind the new manager's flag,
// because they are the unblock for values that change between binaries. Driven through Select,
// which is the one function every production path passes through, so a manager added later is
// covered without anyone remembering.

// gate5Key is declared for this gate alone, since a duplicate declaration panics.
var gate5Key = experimental.Int("configmanager.gate5", 3, experimental.WithOwner("configtest"))

// streams captures a command's two output streams separately, because which stream a finding
// lands on is itself under test.
type streams struct{ out, err bytes.Buffer }

// applyThroughSelect drives Select's manager over a home whose app.toml carries the given
// experimental keys, and returns both streams.
//
// The keys go into the file rather than a pre-seeded source, because the handler builds that
// source itself; pre-seeding one would test a state the boot never reaches.
func applyThroughSelect(t *testing.T, managerEnv string, kv map[string]string) (*streams, error) {
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
			// Quoted, because a dotted key inside a table is one literal name in TOML. A source
			// flattens it back to the full dotted path, which is what the check pass reads.
			fmt.Fprintf(&b, "%q = %q\n", k, v)
		}
		if err := os.WriteFile(filepath.Join(cfgDir, "app.toml"), []byte(b.String()), 0o600); err != nil {
			t.Fatalf("write app.toml: %v", err)
		}
	}

	mgr, err := Select(func(string) string { return managerEnv })
	if err != nil {
		t.Fatalf("Select(%q): %v", managerEnv, err)
	}

	cmd := server.StartCmd(nil, home.Root, []trace.TracerProviderOption{})
	if err := cmd.Flags().Set(flags.FlagHome, home.Root); err != nil {
		t.Fatalf("set --home: %v", err)
	}
	var s streams
	cmd.SetOut(&s.out)
	cmd.SetErr(&s.err)
	serverCtx := &server.Context{}
	cmd.SetContext(context.WithValue(context.Background(), server.ServerContextKey, serverCtx))

	applyErr := mgr.Apply(cmd, "", nil)

	// Without this the gate would be vacuous: a handler that never populated the source leaves
	// nothing for the check pass to read, and every assertion below reads as "not reported" for
	// the wrong reason.
	if serverCtx.Viper == nil {
		t.Fatal("the handler left serverCtx.Viper nil, so the experimental check had no source and " +
			"this gate proves nothing")
	}
	return &s, applyErr
}

// managerEnvs are the two values Select maps onto a manager. Both must report.
var managerEnvs = []struct{ name, env string }{{"legacy", ""}, {"v2", "v2"}}

// TestGate5BothManagersReportExperimentalFindings is the gate.
//
// Asserted on the reported output rather than a call count, because a manager that called the
// check pass and discarded its result would satisfy a count.
func TestGate5BothManagersReportExperimentalFindings(t *testing.T) {
	const undeclared = "configmanager.from_a_later_release"

	for _, m := range managerEnvs {
		t.Run(m.name, func(t *testing.T) {
			// The handler's own outcome is not this gate's subject; the report has to happen either
			// way, since a boot that fails still wants the operator-facing lines.
			s, _ := applyThroughSelect(t, m.env, map[string]string{
				undeclared:     "42",
				gate5Key.Key(): "not-a-number",
			})

			got := s.err.String()
			if !strings.Contains(got, undeclared) {
				t.Errorf("the %s manager did not report the undeclared key %q; Select must wrap every "+
					"manager it returns.\nstderr: %s", m.name, undeclared, got)
			}
			if !strings.Contains(got, gate5Key.Key()) {
				t.Errorf("the %s manager did not report the declared key whose value does not convert, "+
					"which makes Handle.Get's fall back to its default a silent substitution.\nstderr: %s",
					m.name, got)
			}
			if !strings.Contains(got, "declared_type=int") || !strings.Contains(got, "owner=configtest") {
				t.Errorf("the %s manager's report omits the declared type or the owner, so an operator "+
					"cannot tell what shape was expected or who to ask.\nstderr: %s", m.name, got)
			}
		})
	}
}

// TestGate5FindingsNeverReachStdout is the regression guard for machine-readable output.
//
// Apply runs in the root command's PersistentPreRunE, so it runs for every seid subcommand. An
// advisory line on stdout therefore corrupts the output of any command a caller pipes, and the
// framework's premise is that undeclared keys are the ordinary steady state rather than an edge
// case. Held in both directions, so it cannot pass by reporting nothing at all.
func TestGate5FindingsNeverReachStdout(t *testing.T) {
	for _, m := range managerEnvs {
		t.Run(m.name, func(t *testing.T) {
			s, _ := applyThroughSelect(t, m.env, map[string]string{
				"configmanager.from_a_later_release": "42",
			})

			if out := s.out.String(); out != "" {
				t.Errorf("the %s manager wrote %q to stdout. Every seid subcommand runs this, so a line "+
					"here breaks any caller piping the command's output", m.name, out)
			}
			if s.err.Len() == 0 {
				t.Fatalf("the %s manager reported nothing at all, so the stdout assertion above would "+
					"hold for a reporter that had been deleted", m.name)
			}
		})
	}
}

// TestGate5NeitherManagerHaltsOnAnExperimentalFinding is the other half of warn-never-halt.
//
// A configuration written for the next release has to stay bootable on this one, and an
// unconvertible value on an experimental key is reported rather than fatal.
func TestGate5NeitherManagerHaltsOnAnExperimentalFinding(t *testing.T) {
	for _, m := range managerEnvs {
		t.Run(m.name, func(t *testing.T) {
			_, cleanErr := applyThroughSelect(t, m.env, nil)
			_, dirtyErr := applyThroughSelect(t, m.env, map[string]string{
				"configmanager.from_a_later_release": "42",
				gate5Key.Key():                       "not-a-number",
			})

			// Relative, because whether Apply errors here depends on the handler and the test's home.
			// This gate's subject is only that findings do not change the verdict.
			if (cleanErr == nil) != (dirtyErr == nil) {
				t.Errorf("the %s manager's verdict changed when experimental findings were present: "+
					"clean=%v dirty=%v. A finding must never be the reason a boot fails",
					m.name, cleanErr, dirtyErr)
			}
		})
	}
}

// TestGate5ReportingSurvivesEverySourcelessShape covers the paths where there is nothing to read.
//
// Each shape reaches a different guard, and the branch names say which. A test that drove only
// the nil-context shape would leave the nil-source branch uncovered while its name implied
// otherwise, which is how this test read before review.
func TestGate5ReportingSurvivesEverySourcelessShape(t *testing.T) {
	for _, tc := range []struct {
		name string
		cmd  func() *cobra.Command
	}{
		{"nil command", func() *cobra.Command { return nil }},
		{"no cobra context", func() *cobra.Command { return &cobra.Command{Use: "start"} }},
		{"context with a zero server context", func() *cobra.Command {
			// server.Context's zero value carries a nil Viper, which is the only shape that reaches
			// the nil-source guard. NewDefaultContext would supply a non-nil one.
			cmd := &cobra.Command{Use: "start"}
			cmd.SetContext(context.WithValue(context.Background(), server.ServerContextKey, &server.Context{}))
			return cmd
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer

			reportExperimental(&buf, tc.cmd())

			if strings.Contains(buf.String(), "panicked") {
				t.Errorf("reporting panicked and recovered, so this shape is handled by the recover "+
					"rather than by a check.\nwrote: %s", buf.String())
			}
		})
	}
}

// TestGate5AReportedSourcelessPassIsObservable pins that declining is not silence.
//
// An operator cannot otherwise tell a pass that never ran from a pass that ran and found
// nothing. The sibling reporter in this package makes the same distinction for the same reason.
func TestGate5AReportedSourcelessPassIsObservable(t *testing.T) {
	var buf bytes.Buffer
	cmd := &cobra.Command{Use: "start"}
	cmd.SetContext(context.WithValue(context.Background(), server.ServerContextKey, &server.Context{}))

	reportExperimental(&buf, cmd)

	if !strings.Contains(buf.String(), "skipped") {
		t.Errorf("a pass with no source reported %q, which does not distinguish declining from "+
			"running clean", buf.String())
	}
}

// TestGate5TheKeyListIsCapped applies this package's own bound to the new reporter.
//
// A rollback can make a whole feature's key set undeclared at once, and the count is what an
// operator acts on. The sibling reporter caps for the same reason.
func TestGate5TheKeyListIsCapped(t *testing.T) {
	kv := map[string]string{}
	for i := 0; i < maxReportedKeys*3; i++ {
		kv[fmt.Sprintf("configmanager.bulk_%02d", i)] = "1"
	}

	s, _ := applyThroughSelect(t, "", kv)

	got := s.err.String()
	if !strings.Contains(got, fmt.Sprintf("count=%d", len(kv))) {
		t.Errorf("the report does not carry the full count, which is what an operator alerts on.\n%s", got)
	}
	if !strings.Contains(got, fmt.Sprintf("omitted=%d", len(kv)-maxReportedKeys)) {
		t.Errorf("the report does not say how many keys it left out, so a reader takes the rendered "+
			"list for the whole set.\n%s", got)
	}
	if n := strings.Count(got, "configmanager.bulk_"); n > maxReportedKeys {
		t.Errorf("the report rendered %d keys with a cap of %d; an unbounded line is dropped by some "+
			"log shippers and split by others", n, maxReportedKeys)
	}
}

package configmanager

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/config/tendermintbase"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	serverconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel/sdk/trace"
)

// runCheck runs the command against a home holding the given sei.toml, and returns what it printed and
// whether it failed.
func runCheck(t *testing.T, body string) (string, error) {
	t.Helper()
	// Isolated because this resolves through the real environment and reads the gate out of it. A stray
	// SEID_* or SEI_CONFIG_MANAGER on a developer's machine or a runner would change a resolved value and
	// flip these assertions.
	configtest.Isolate(t)
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if body != "" {
		if err := os.WriteFile(filepath.Join(home, "config", seiTomlName), []byte(body), 0o600); err != nil {
			t.Fatalf("write sei.toml: %v", err)
		}
	}
	// A node's own configuration file, recording the same kind of node the header below writes. Without it
	// every case here also carries a disagreement about what kind of node this is, which is a different
	// test's subject.
	if err := os.WriteFile(filepath.Join(home, "config", "config.toml"),
		[]byte("mode = \"validator\"\n"), 0o600); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}

	cmd := CheckCmd()
	cmd.Flags().String(flags.FlagHome, home, "")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	err := cmd.Execute()
	return out.String(), err
}

// TestTheCheckFailsOnWhatABootWouldRefuse is the point of the command.
//
// A boot may not refuse a file, so every value it cannot use is a report on a node that has already
// restarted. The same questions have exact answers beforehand, and this is where an answer costs a failed
// check instead.
func TestTheCheckFailsOnWhatABootWouldRefuse(t *testing.T) {
	const header = "schema_version = 1\nnode_mode = \"validator\"\n"

	t.Run("a file this binary can use passes", func(t *testing.T) {
		out, err := runCheck(t, header+"\n[mempool]\nttl-duration = \"60s\"\nsize = 4321\n")
		if err != nil {
			t.Errorf("a usable file was refused: %v\n%s", err, out)
		}
	})

	t.Run("no file at all is not a problem", func(t *testing.T) {
		out, err := runCheck(t, "")
		if err != nil {
			t.Errorf("a node with no sei.toml was refused: %v", err)
		}
		if !strings.Contains(out, "no sei.toml") {
			t.Errorf("the report does not say the file is absent, so a missing file reads as a clean "+
				"one:\n%s", out)
		}
	})

	t.Run("a length of time written as a plain number fails", func(t *testing.T) {
		out, err := runCheck(t, header+"\n[mempool]\nttl-duration = 60\n")
		if err == nil {
			t.Errorf("a plain number in a length of time passed:\n%s", out)
		}
		if !strings.Contains(out, "nanoseconds") {
			t.Errorf("the report does not say what is wrong with it:\n%s", out)
		}
	})

	t.Run("a value the decode refuses fails", func(t *testing.T) {
		out, err := runCheck(t, header+"\n[instrumentation]\nmax-open-connections = \"not a number\"\n")
		if err == nil {
			t.Errorf("a value no decode accepts passed:\n%s", out)
		}
	})

	t.Run("a key no section declares is reported", func(t *testing.T) {
		out, err := runCheck(t, header+"\n[mempool]\nnot-a-key = 1\n")
		if err == nil {
			t.Errorf("a key nothing declares passed:\n%s", out)
		}
		if !strings.Contains(out, "no effect") {
			t.Errorf("the report does not say the key has no effect:\n%s", out)
		}
	})

	t.Run("a mode this binary does not know fails", func(t *testing.T) {
		out, err := runCheck(t, "schema_version = 1\nnode_mode = \"sentry\"\n")
		if err == nil {
			t.Errorf("a mode nothing declares passed:\n%s", out)
		}
	})
}

// TestADisagreementAboutTheKindOfNodeIsFound covers a fact two files state under different names.
//
// sei.toml records the kind of node at its top and every value resolved through this manager is the answer
// for that kind. The node's own configuration file states it again in a key of its own, and that one is what
// the node runs as. Nothing here declares the second on purpose, so the two can be written to disagree, and
// a node that resolves a validator's values while running as a full node reads correctly in every report
// about it.
func TestADisagreementAboutTheKindOfNodeIsFound(t *testing.T) {
	for _, tc := range []struct {
		recorded, running string
		disagree          bool
		why               string
	}{
		{"validator", "validator", false, "the same kind is not a disagreement"},
		{"validator", "full", true, "a validator that runs as a query-serving node serves queries"},
		{"full", "validator", true, "a node resolved for queries that runs as a validator holds a key"},
		{"seed", "full", true, "a seed exists to serve peers and would be serving queries"},
		{"archive", "full", false, "the kind that keeps every version has no name of its own in that " +
			"file, so the command that writes it writes this one"},
		{"archive", "validator", true, "an archive that runs as a validator is a disagreement"},
	} {
		if got := modesDisagree(tc.recorded, tc.running); got != tc.disagree {
			t.Errorf("sei.toml %q against a node running %q reports disagree=%v, want %v: %s",
				tc.recorded, tc.running, got, tc.disagree, tc.why)
		}
	}
}

// TestApplyReportsADisagreementAboutTheKindOfNode drives the real Apply, so the wiring is what is asserted.
//
// The test beside this one holds the decision, which a comparison never reached would still pass. This one
// gives the two files different kinds of node and looks for the report, so removing the call fails here.
func TestApplyReportsADisagreementAboutTheKindOfNode(t *testing.T) {
	configtest.Isolate(t)
	root := writeMinimalHome(t, "mode = \"full\"\n", "")
	if err := os.WriteFile(filepath.Join(root, "config", seiTomlName),
		[]byte("schema_version = 1\nnode_mode = \"validator\"\n"), 0o600); err != nil {
		t.Fatalf("write sei.toml: %v", err)
	}

	cmd := server.StartCmd(nil, "/foobar", []trace.TracerProviderOption{})
	if err := cmd.Flags().Set(flags.FlagHome, root); err != nil {
		t.Fatalf("set --home: %v", err)
	}
	serverCtx := &server.Context{}
	cmd.SetContext(context.WithValue(context.Background(), server.ServerContextKey, serverCtx))

	capture := &capturingHandler{}
	mgr := SeiConfigManager{logger: slog.New(capture)}
	if err := mgr.Apply(cmd, serverconfig.DefaultConfigTemplate, serverconfig.DefaultConfig()); err != nil {
		t.Fatalf("the fixture is meant to boot, so this is the fixture: %v", err)
	}

	var found bool
	for _, r := range capture.records {
		if strings.Contains(r.Message, "one kind of node") {
			found = true
		}
	}
	if !found {
		t.Error("sei.toml said validator, the node's own file said full, and nothing reported it. A " +
			"node resolving a validator's values while running as a query-serving node reads correctly " +
			"in every other report about it")
	}
}

// TestCheckReportsAFileItCannotRead holds the difference the command exists to tell an operator.
//
// Running this before a restart asks whether the file is right. A file that will not parse, or records a
// schema this binary does not know, or names no node kind, is the case where the answer matters most, and
// answering that the node has no file is both wrong and the answer least likely to make anyone look.
func TestCheckReportsAFileItCannotRead(t *testing.T) {
	configtest.Isolate(t)
	for _, tc := range []struct {
		name string
		body string
	}{
		{"unparseable", "[evm\n"},
		{"unknown schema", "schema_version = 99\nnode_mode = \"validator\"\n"},
		{"no node mode", "schema_version = 1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if err := os.MkdirAll(filepath.Join(home, "config"), 0o750); err != nil {
				t.Fatalf("make a home: %v", err)
			}
			if err := os.WriteFile(filepath.Join(home, "config", seiTomlName),
				[]byte(tc.body), 0o600); err != nil {
				t.Fatalf("write the file: %v", err)
			}

			cmd := &cobra.Command{}
			cmd.Flags().String(flags.FlagHome, home, "")
			problems, _, found, err := checkSeiToml(cmd)
			if err != nil {
				t.Fatalf("checkSeiToml: %v", err)
			}
			if !found {
				t.Errorf("a %s sei.toml is reported as no file at all, so the command exits zero on it",
					tc.name)
			}
			if len(problems) == 0 {
				t.Errorf("a %s sei.toml produced no problem to report", tc.name)
			}
		})
	}
}

// TestTheCheckSaysWhetherABootWouldReadTheFileAtAll covers the conclusion an operator would otherwise draw.
//
// Until the gate is switched, a boot reads none of this file. A check that answers only about the file's
// contents reads as "in use and correct" on every one of those nodes, which invites somebody to trust a
// file nothing reads. That is the wrong conclusion in the dangerous direction.
func TestTheCheckSaysWhetherABootWouldReadTheFileAtAll(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		says  bool
	}{
		{"unset", "", true},
		{"legacy", "legacy", true},
		{"v2", "v2", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			problems, notes := whatTheGateSays(func(string) string { return tc.value })
			said := strings.Contains(strings.Join(notes, "\n"), "reads none of this file")
			if said != tc.says {
				t.Errorf("with %s=%q the command says a boot reads none of the file: %v, want %v.\n%v",
					EnvVar, tc.value, said, tc.says, notes)
			}
			// A gate that is off is not a problem: the file is still worth checking, and the exit status
			// has to stay usable on every node that has not switched yet.
			if len(problems) != 0 {
				t.Errorf("with %s=%q the gate is reported as a problem: %v", EnvVar, tc.value, problems)
			}
		})
	}
}

// TestTheCheckSaysWhenTheGateItselfIsWrong covers a value a boot refuses outright.
//
// The gate is matched exactly and never falls back, so a misspelling stops the node before it reaches any
// file. A check that reported only on the file would pass, and the node would not start.
func TestTheCheckSaysWhenTheGateItselfIsWrong(t *testing.T) {
	problems, _ := whatTheGateSays(func(string) string { return "V2" })
	if len(problems) == 0 {
		t.Fatal("a gate value this binary refuses is not reported as a problem, so the command exits " +
			"zero for a node that cannot start")
	}
	if !strings.Contains(strings.Join(problems, "\n"), "does not accept") {
		t.Errorf("the problem does not say the value was refused: %v", problems)
	}
}

// runCheckWithNodeFile runs the command against a home holding both files.
func runCheckWithNodeFile(t *testing.T, seiToml, configToml string) (string, error) {
	t.Helper()
	// Isolated because this resolves through the real environment and reads the gate out of it. A stray
	// SEID_* or SEI_CONFIG_MANAGER on a developer's machine or a runner would change a resolved value and
	// flip these assertions.
	configtest.Isolate(t)
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "config", seiTomlName), []byte(seiToml), 0o600); err != nil {
		t.Fatalf("write sei.toml: %v", err)
	}
	if configToml != "" {
		path := filepath.Join(home, "config", "config.toml")
		if err := os.WriteFile(path, []byte(configToml), 0o600); err != nil {
			t.Fatalf("write config.toml: %v", err)
		}
	}

	cmd := CheckCmd()
	cmd.Flags().String(flags.FlagHome, home, "")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	err := cmd.Execute()
	return out.String(), err
}

// TestTheCheckFindsADisagreementAboutWhatKindOfNodeThisIs covers the question with the largest consequence.
//
// Two files record what kind of node this is, under different names, and nothing keeps them in step. A node
// whose sei.toml says validator while its own file says full resolves a validator's answers and runs as a
// full node, serving queries. Every report about it reads correctly, which is what makes it worth catching
// before a restart rather than after.
//
// The boot reports this at its loudest level. A pre-flight that does not ask is silent on the one thing an
// operator most needs to know before they restart.
func TestTheCheckFindsADisagreementAboutWhatKindOfNodeThisIs(t *testing.T) {
	const seiToml = "schema_version = 1\nnode_mode = \"validator\"\n\n[mempool]\nsize = 4321\n"

	t.Run("a disagreement is reported", func(t *testing.T) {
		out, err := runCheckWithNodeFile(t, seiToml, "mode = \"full\"\n")
		if err == nil {
			t.Errorf("sei.toml says validator and the node's own file says full, and the check passed:\n%s",
				out)
		}
		if !strings.Contains(out, "node's own") {
			t.Errorf("the report does not name the disagreement:\n%s", out)
		}
	})

	t.Run("agreement is not a problem", func(t *testing.T) {
		out, err := runCheckWithNodeFile(t, seiToml, "mode = \"validator\"\n")
		if err != nil {
			t.Errorf("both files say validator and the check failed: %v\n%s", err, out)
		}
	})

	t.Run("no configuration file of its own is compared against what a boot would compute", func(t *testing.T) {
		// A boot does not see an absent mode. It starts from this binary's defaults and writes the file
		// itself, so a node with sei.toml naming a different kind reports the disagreement at its loudest
		// level on the next start. Answering "nothing to disagree with" here would pass that node.
		out, err := runCheckWithNodeFile(t, seiToml, "")
		if err == nil {
			t.Errorf("sei.toml says validator, a boot would compute a different kind for a node with no "+
				"configuration file, and the check passed:\n%s", out)
		}
		if !strings.Contains(out, "node's own") {
			t.Errorf("the report does not name the disagreement:\n%s", out)
		}
	})

	t.Run("a present but empty kind is what a boot gets, not the default", func(t *testing.T) {
		// sei.toml names the kind this binary defaults to, which is the case that separates the two
		// answers: substituting the default makes the two sides agree and the check pass, while a boot
		// unmarshals the empty string over its default, runs with no kind at all, and reports it.
		naming := "schema_version = 1\nnode_mode = \"" + tmcfg.DefaultConfig().Mode +
			"\"\n\n[mempool]\nsize = 4321\n"
		out, err := runCheckWithNodeFile(t, naming, "mode = \"\"\n")
		if err == nil {
			t.Errorf("the node's own file records no kind at all and the check passed, so it answered "+
				"with a default the boot does not use:\n%s", out)
		}
	})

	t.Run("a configuration file it cannot read is a problem, not an absence", func(t *testing.T) {
		// A boot does not start on one of these at all, so answering with a default would pass a node
		// that cannot boot. It is the same rule this command already applies to sei.toml.
		out, err := runCheckWithNodeFile(t, seiToml, "mode = \"validator\"\n[[[ not toml\n")
		if err == nil {
			t.Errorf("an unparseable configuration file passed the check, and a boot would not start:\n%s",
				out)
		}
		if !strings.Contains(out, "cannot be read") {
			t.Errorf("the report does not say the node's own file cannot be read:\n%s", out)
		}
	})

	t.Run("no configuration file of its own and a matching kind passes", func(t *testing.T) {
		matching := "schema_version = 1\nnode_mode = \"" + tmcfg.DefaultConfig().Mode +
			"\"\n\n[mempool]\nsize = 4321\n"
		out, err := runCheckWithNodeFile(t, matching, "")
		if err != nil {
			t.Errorf("sei.toml names the kind a boot would compute and the check failed: %v\n%s", err, out)
		}
	})
}

// TestTheCheckReportsWhatTheResolutionAlreadyKnows covers the two problems that need no rehearsal.
//
// A refused registration is the one that matters most, because of what it does to the report beside it: the
// section's keys are absent from the declared set, so an operator's valid key for one of them lands among
// the keys nothing declares. Reported without the cause, this command tells them their file is wrong about
// a key it was right about. The boot reports the cause first for exactly this reason.
func TestTheCheckReportsWhatTheResolutionAlreadyKnows(t *testing.T) {
	t.Run("a refused registration is named, before anything else", func(t *testing.T) {
		got := whatTheResolutionAlreadyKnows(registry.Resolved{
			Refused: []registry.Defect{{Section: "mempool", Err: errors.New("two keys share a spelling")}},
			Ignored: map[string]string{"telemetry.global-labels": "a variable holds one string"},
		})
		if len(got) != 2 {
			t.Fatalf("got %d problems, want the refusal and the ignored variable: %v", len(got), got)
		}
		if !strings.Contains(got[0], "mempool") || !strings.Contains(got[0], "carries a defect") {
			t.Errorf("the first problem does not name the refused registration: %q", got[0])
		}
		if !strings.Contains(got[1], "telemetry.global-labels") {
			t.Errorf("the second problem does not name the ignored variable: %q", got[1])
		}
	})

	t.Run("a clean resolution reports nothing", func(t *testing.T) {
		if got := whatTheResolutionAlreadyKnows(registry.Resolved{}); len(got) != 0 {
			t.Errorf("a resolution with no defect and no ignored variable reports %v", got)
		}
	})
}

// TestTheCauseIsReportedBeforeTheSymptom holds the order the two reports are read in.
//
// A refused registration leaves its section's keys out of the declared set, so an operator's valid key for
// one of them reads as a key nothing declares. Printed after that line, the registration defect explains
// something the reader has already concluded was their own typo. The boot reports the cause first for the
// same reason.
func TestTheCauseIsReportedBeforeTheSymptom(t *testing.T) {
	var out bytes.Buffer
	problems := append(
		whatTheResolutionAlreadyKnows(registry.Resolved{
			Refused: []registry.Defect{{Section: "mempool", Err: errors.New("two keys share a spelling")}},
		}),
		"mempool.size: sei.toml writes this and no section declares it, so it has no effect")
	for _, line := range problems {
		report(&out, line)
	}

	cause := strings.Index(out.String(), "carries a defect")
	symptom := strings.Index(out.String(), "no section declares it")
	if cause == -1 || symptom == -1 {
		t.Fatalf("both lines have to be present for the order to mean anything:\n%s", out.String())
	}
	if cause > symptom {
		t.Errorf("the registration defect is printed after the key it explains, so an operator reading "+
			"in order treats a valid key as their own typo:\n%s", out.String())
	}
}

// TestTheCountsAddUpAndNameTheRightSource holds the arithmetic an operator reads before a restart.
//
// The line decides whether somebody expects a restart to move one setting or to replace everything around
// it, and an off-by-one in it is invisible: the sentence reads correctly whatever the numbers are. The
// source matters as much as the count. A key an environment variable or a flag answered is absent from the
// file and does not take a declared value, so counting the file's absences would name a default where one
// does not apply.
func TestTheCountsAddUpAndNameTheRightSource(t *testing.T) {
	configtest.Isolate(t)
	written := map[string]any{"mempool.size": 4321}
	resolved, err := registry.Resolve(registry.ModeFull, registry.Sources{
		File:      written,
		LookupEnv: func(name string) (string, bool) { return "", false },
		Flags:     map[string]any{"rpc.max-open-connections": "99"},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	total := len(resolved.Values)
	if total == 0 || len(resolved.Overrides) < 2 {
		t.Fatalf("the resolution answered %d keys with %d overrides, so this measures nothing",
			total, len(resolved.Overrides))
	}

	line := whatTheFileLeavesToTheDeclaration(resolved, written, "full")

	// Every declared key falls in exactly one of the three, so the three have to sum to the total.
	var stated, elsewhere, declared int
	if _, err := fmt.Sscanf(line, "this file states %d of %d declared keys", &stated, &total); err != nil {
		t.Fatalf("the line does not state the file's count: %q", line)
	}
	if !strings.Contains(line, "answered by an environment variable or a flag") {
		t.Fatalf("a flag answered a declared key and the line does not say so, so its value reads as a "+
			"binary default: %q", line)
	}
	for _, part := range strings.Split(line, "; ") {
		switch {
		case strings.HasPrefix(part, "the other "):
			_, _ = fmt.Sscanf(part, "the other %d", &declared)
		case strings.Contains(part, "more are answered"):
			_, _ = fmt.Sscanf(part, "%d more are answered", &elsewhere)
		}
	}
	if stated+elsewhere+declared != total {
		t.Errorf("%d stated + %d answered elsewhere + %d declared = %d, and the resolution answered %d. "+
			"A key is counted twice or not at all:\n%s",
			stated, elsewhere, declared, stated+elsewhere+declared, total, line)
	}
	if stated != 1 {
		t.Errorf("the file states one key and the line says %d", stated)
	}
	if elsewhere != 1 {
		t.Errorf("one flag answered a declared key and the line says %d", elsewhere)
	}
}

// theTwoVerdicts is what the check and the boot's delivery each made of one input.
type theTwoVerdicts struct {
	// The sections each refused, sorted.
	byTheCheck, byTheDelivery []string
	// The configuration the delivery published into, which is what a boot would go on to run.
	delivered *tmcfg.Config
}

// rehearseAndDeliver runs the check's rehearsal and the boot's delivery over one resolution.
//
// Driven from one resolution and one node configuration, because the two verdicts are only comparable for
// the same input. Each side is handed a copy of that configuration, since the delivery publishes into the
// one it is given.
func rehearseAndDeliver(t *testing.T, own *tmcfg.Config, file map[string]any) theTwoVerdicts {
	t.Helper()
	resolved, err := registry.Resolve(registry.ModeValidator, registry.Sources{
		File:      file,
		LookupEnv: func(string) (string, bool) { return "", false },
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	for key := range file {
		if _, declared := resolved.Values[key]; !declared {
			t.Fatalf("no section declares %s, so the file writes nothing and both verdicts are about a "+
				"resolution that carries none of this fixture", key)
		}
	}

	forTheCheck, err := copyNodeConfig(own)
	if err != nil {
		t.Fatalf("copy the node's configuration: %v", err)
	}
	forTheDelivery, err := copyNodeConfig(own)
	if err != nil {
		t.Fatalf("copy the node's configuration: %v", err)
	}

	capture := &capturingHandler{}
	log := slog.New(capture)
	ctx := &server.Context{Config: forTheDelivery}
	bySection, _ := registry.ResolvedAndOwnedByDecodedSections(resolved)
	deliverDecodedSections(ctx, bySection, log, log.Info)

	return theTwoVerdicts{
		byTheCheck:    sectionsTheCheckNamed(whatADecodeWouldRefuse(resolved, forTheCheck)),
		byTheDelivery: sectionsTheDeliveryNamed(capture.records),
		delivered:     ctx.Config,
	}
}

// sectionsTheCheckNamed reads the section out of each problem the check reported, which names it first in
// brackets.
//
// A problem naming no section is kept whole rather than dropped. Dropped, a report that stopped naming
// sections would compare equal to a run that refused nothing.
func sectionsTheCheckNamed(problems []string) []string {
	out := make([]string, 0, len(problems))
	for _, problem := range problems {
		closing := strings.Index(problem, "]")
		if !strings.HasPrefix(problem, "[") || closing < 0 {
			out = append(out, problem)
			continue
		}
		out = append(out, problem[1:closing])
	}
	sort.Strings(out)
	return out
}

// sectionsTheDeliveryNamed reads the section off every refusal the delivery logged. A refusal is what it
// reports at its error level, and each one carries the section it cost.
func sectionsTheDeliveryNamed(records []slog.Record) []string {
	out := make([]string, 0, len(records))
	for _, record := range records {
		if record.Level < slog.LevelError {
			continue
		}
		named := ""
		record.Attrs(func(a slog.Attr) bool {
			if a.Key == "section" {
				named = a.Value.String()
			}
			return true
		})
		out = append(out, named)
	}
	sort.Strings(out)
	return out
}

// TestTheCheckDoesNotFailASectionTheDeliveryApplies is why this command sits on top of the delivery.
//
// The answer is a prediction, and a prediction is worth its exit status only when it comes from the code
// that would refuse the value at boot. The delivery holds a section to that section's own rules. Asking the
// whole configuration instead reports a failure standing anywhere in config.toml as this section's values
// being refused, so a fleet reading the exit status holds a change back over a node that would take it.
func TestTheCheckDoesNotFailASectionTheDeliveryApplies(t *testing.T) {
	configtest.Isolate(t)
	const written = tendermintbase.RPCSectionName + ".max-open-connections"

	// State sync is on with no servers to reach, which is a state its own rules refuse and an operator's
	// config.toml can be in for months without it mattering.
	own := tmcfg.DefaultConfig()
	own.StateSync.Enable = true
	if whatTheSectionsOwnRulesSay(own, []string{tendermintbase.StateSyncSectionName + ".enable"}) == nil {
		t.Fatal("this fixture passes state sync's own rules, so there is no standing failure for the " +
			"check to attribute to another section")
	}
	if own.ValidateBasic() == nil {
		t.Fatal("this fixture passes the whole configuration's rules, so asking those answers the same " +
			"as asking one section's and nothing here separates the two questions")
	}
	// The delivery takes sections in sorted order, so the written section has to sort first. A standing
	// failure in a section delivered earlier is corrected by that section's own declared values before the
	// written one is reached, and the case would not arise.
	if tendermintbase.RPCSectionName >= tendermintbase.StateSyncSectionName {
		t.Fatalf("[%s] does not sort before [%s], so the standing failure is corrected before the "+
			"written section is rehearsed", tendermintbase.RPCSectionName,
			tendermintbase.StateSyncSectionName)
	}

	got := rehearseAndDeliver(t, own, map[string]any{written: 111})

	if runs := got.delivered.RPC.MaxOpenConnections; runs != 111 {
		t.Fatalf("the delivery left max-open-connections at %d with 111 in the file, so it refused the "+
			"section and there is no applied section for the check to disagree about", runs)
	}
	if strings.Join(got.byTheCheck, ",") != strings.Join(got.byTheDelivery, ",") {
		t.Errorf("the check refused %v and the delivery refused %v for the same input. A failure "+
			"standing in [%s] is reported as another section's values being refused, and the command "+
			"exits non-zero on a file a boot applies",
			got.byTheCheck, got.byTheDelivery, tendermintbase.StateSyncSectionName)
	}
}

// TestTheCheckRefusesWhatTheDeliveryRefuses holds the command to still failing on what a boot drops.
//
// Asking one section's rules rather than the whole configuration's is only right while it refuses what the
// boot refuses. The keys at the root of the file are the case to watch: the registry names that section for
// its reports and the node's own type carries no tag of that name, so a section picked out by name states
// no rules and every value in it passes.
func TestTheCheckRefusesWhatTheDeliveryRefuses(t *testing.T) {
	for _, tc := range []struct {
		section string
		file    map[string]any
	}{
		{tendermintbase.RPCSectionName, map[string]any{
			tendermintbase.RPCSectionName + ".max-open-connections": -1}},
		{tendermintbase.MempoolSectionName, map[string]any{
			tendermintbase.MempoolSectionName + ".size": -1}},
		{tendermintbase.RootSectionName, map[string]any{"log-format": "nonsense"}},
	} {
		t.Run(tc.section, func(t *testing.T) {
			configtest.Isolate(t)
			own := tmcfg.DefaultConfig()
			if err := own.ValidateBasic(); err != nil {
				t.Fatalf("this fixture's node configuration fails on its own, so a refusal here says "+
					"nothing about the written value: %v", err)
			}

			got := rehearseAndDeliver(t, own, tc.file)

			if strings.Join(got.byTheDelivery, ",") != tc.section {
				t.Fatalf("the delivery refused %v, want exactly [%s]. The fixture does not write a value "+
					"a boot refuses, so this case cannot show the check refusing one",
					got.byTheDelivery, tc.section)
			}
			if strings.Join(got.byTheCheck, ",") != tc.section {
				t.Errorf("the check refused %v where the delivery refused %v. A value a boot drops "+
					"passes the command an operator runs to find it", got.byTheCheck, got.byTheDelivery)
			}
		})
	}
}

// TestNoHomeIsRefusedRatherThanAnsweredFromTheWorkingDirectory covers the one case where answering is
// worse than declining.
//
// Every path here joins the home with config/sei.toml. An empty home leaves that relative, so the read
// lands wherever the command was run from and reports on a file belonging to some other node. An operator
// runs this to decide whether to restart, so a confident answer about the wrong node is the worst outcome
// available.
//
// The variable this names is read from the running binary, which under a test binary is not the one a node
// uses, so the message is checked for naming a variable rather than for naming seid's.
func TestNoHomeIsRefusedRatherThanAnsweredFromTheWorkingDirectory(t *testing.T) {
	configtest.Isolate(t)

	elsewhere := t.TempDir()
	if err := os.MkdirAll(filepath.Join(elsewhere, "config"), 0o750); err != nil {
		t.Fatalf("make a directory holding another node's file: %v", err)
	}
	// A complete, valid file for a different node. Nothing about it is wrong; it is simply not ours.
	if err := os.WriteFile(filepath.Join(elsewhere, "config", seiTomlName),
		[]byte("schema_version = 1\nnode_mode = \"validator\"\n"), 0o600); err != nil {
		t.Fatalf("write another node's sei.toml: %v", err)
	}
	t.Chdir(elsewhere)

	// No --home flag and nothing in the environment, which is what an exec into a container gives when
	// the variable that is set is not the one the resolver reads.
	cmd := &cobra.Command{Use: "check"}
	cmd.SetContext(context.Background())

	problems, notes, found, err := checkSeiToml(cmd)
	if err == nil {
		t.Fatalf("with no home set the check answered from the working directory: found=%v problems=%v "+
			"notes=%v. It reported on a file belonging to another node", found, problems, notes)
	}
	if !strings.Contains(err.Error(), "--home") || !strings.Contains(err.Error(), "_HOME") {
		t.Errorf("declining is right, but the reason does not name what to set: %v", err)
	}
}

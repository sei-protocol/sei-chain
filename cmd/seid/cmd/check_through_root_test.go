package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	svrcmd "github.com/sei-protocol/sei-chain/sei-cosmos/server/cmd"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// runCheckThroughRoot runs `config check --home <dir>` the way an operator and a runbook run it.
//
// Through the real root command, because that is what makes the difference. A command executed on its own
// has no parent, so nothing the root does before it happens: no hook runs, no flag is marked changed by
// anything but the caller, and no file is generated. Every one of those is a thing this command has to be
// right about, and a test that builds the command directly cannot see any of them.
func runCheckThroughRoot(t *testing.T, home string, extraArgs ...string) (string, error) {
	t.Helper()

	root, _ := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SilenceUsage = true
	root.SilenceErrors = true
	root.SetArgs(append([]string{"config", "check", "--home", home}, extraArgs...))

	// The binary's own entry point rather than a copy of it. It registers the two logging flags, seeds the
	// client and server contexts, and runs PrepareBaseCmd, which is what registers --home. Re-implementing
	// that here would drift the moment any of it changes, and the claim this helper rests on is that the
	// command runs the way an operator runs it.
	err := svrcmd.Execute(root, home)
	return out.String(), err
}

// writeNodeHome puts a sei.toml in a home that also records what kind of node this is.
//
// A real node has a configuration file of its own. Without one, every case here would also carry a
// disagreement about what kind of node it is, which is a different test's subject.
func writeNodeHome(t *testing.T, body string) string {
	t.Helper()
	home := writeSeiToml(t, body)
	if err := os.WriteFile(filepath.Join(home, "config", "config.toml"),
		[]byte("mode = \"validator\"\n"), 0o600); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}
	return home
}

// writeSeiToml puts a sei.toml in a home that holds nothing else.
func writeSeiToml(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "config", "sei.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write sei.toml: %v", err)
	}
	return home
}

// TestTheCheckPassesACorrectFileWhenRunAsAnOperatorRunsIt is the answer the command exists to give.
//
// The one flag every real invocation carries is --home, and it names no setting: the node is told where its
// files are, not what to put in them. It arrives in the resolution beside the file's own keys, and counted
// with them it is a key nothing declares, so the command failed on a correct file and named a key the file
// does not contain. A pre-flight that fails every time carries nothing, and it is the whole compensating
// control for a boot that may not refuse a file.
func TestTheCheckPassesACorrectFileWhenRunAsAnOperatorRunsIt(t *testing.T) {
	configtest.Isolate(t)
	home := writeNodeHome(t, "schema_version = 1\nnode_mode = \"validator\"\n\n[mempool]\nsize = 4321\n")

	out, err := runCheckThroughRoot(t, home)
	if err != nil {
		t.Errorf("a correct file was refused when the command was run with --home: %v\n%s", err, out)
	}
	for _, flag := range []string{"home", "trace", "chain-id"} {
		if strings.Contains(out, flag+": sei.toml writes this") {
			t.Errorf("the report names --%s as something sei.toml wrote. The file does not contain it, "+
				"and an operator told this on every run stops reading the one signal they have", flag)
		}
	}
}

// TestTheCheckDoesNotGenerateTheFilesItIsAskedAbout holds the command to answering rather than acting.
//
// The root command's hook runs the configuration handler, which writes config.toml and app.toml when they
// are absent. A command that reports on one file would then create two others as a side effect, on a node
// an operator was only asking a question about.
func TestTheCheckDoesNotGenerateTheFilesItIsAskedAbout(t *testing.T) {
	configtest.Isolate(t)
	home := writeSeiToml(t, "schema_version = 1\nnode_mode = \"validator\"\n\n[mempool]\nsize = 4321\n")

	// The verdict is not what this measures, and asserting it first would stop the measurement whenever
	// something else about the check is wrong.
	if out, err := runCheckThroughRoot(t, home); err != nil {
		t.Logf("the check reported a problem, which is not what this test is about: %v\n%s", err, out)
	}
	for _, name := range []string{"config.toml", "app.toml"} {
		if _, err := os.Stat(filepath.Join(home, "config", name)); err == nil {
			t.Errorf("%s was created by a command that answers a question about a different file", name)
		}
	}
}

// TestTheCheckStillFailsAFileWithSomethingWrongInIt keeps the fixes above from making it answer nothing.
//
// Passing a correct file and generating no files are both satisfied by a command that does nothing at all,
// so the failure has to be shown to still happen through the same path.
func TestTheCheckStillFailsAFileWithSomethingWrongInIt(t *testing.T) {
	configtest.Isolate(t)
	for _, tc := range []struct {
		name string
		body string
		says string
	}{
		{
			name: "a key no section declares",
			body: "schema_version = 1\nnode_mode = \"validator\"\n\n[mempool]\nsizze = 4321\n",
			says: "no section declares it",
		},
		{
			name: "a length of time as a plain number",
			body: "schema_version = 1\nnode_mode = \"validator\"\n\n[mempool]\nttl-duration = 60\n",
			says: "nanoseconds",
		},
		{
			name: "a number larger than the setting holds",
			body: "schema_version = 1\nnode_mode = \"validator\"\n\n[p2p]\nmax-connections = 1e20\n",
			says: "larger than this setting can hold",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runCheckThroughRoot(t, writeNodeHome(t, tc.body))
			if err == nil {
				t.Fatalf("the file was accepted, so a boot would apply what it could and the operator "+
					"would find out afterwards\n%s", out)
			}
			if !strings.Contains(out, tc.says) {
				t.Errorf("the report does not say %q, so it does not tell an operator what to change:\n%s",
					tc.says, out)
			}
		})
	}
}

// TestTheCheckRefusesWhatTheDeliveryWouldRefuse holds the two paths to the same answer.
//
// A value can decode cleanly and still be one the node's own rules reject, and the delivery refuses a
// section on exactly that. A check that rehearses only the decode passes a file the boot drops, which is
// the divergence this command exists to prevent rather than produce.
func TestTheCheckRefusesWhatTheDeliveryWouldRefuse(t *testing.T) {
	configtest.Isolate(t)
	home := writeNodeHome(t,
		"schema_version = 1\nnode_mode = \"validator\"\n\n[mempool]\nmax-tx-bytes = -1\n")

	out, err := runCheckThroughRoot(t, home)
	if err == nil {
		t.Fatalf("a negative transaction-size ceiling passed the check, and a boot refuses it:\n%s", out)
	}
	if !strings.Contains(out, "max-tx-bytes") {
		t.Errorf("the report does not name the value the node's rules reject:\n%s", out)
	}
}

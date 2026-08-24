package configmanager

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
)

// runCheck runs the command against a home holding the given sei.toml, and returns what it printed and
// whether it failed.
func runCheck(t *testing.T, body string) (string, error) {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if body != "" {
		if err := os.WriteFile(filepath.Join(home, "config", seiTomlName), []byte(body), 0o600); err != nil {
			t.Fatalf("write sei.toml: %v", err)
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

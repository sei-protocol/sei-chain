package configmanager

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
)

// runGenerate runs the command on its own, which is what makes the two refusals below reachable.
//
// A command executed with no parent has no sibling start command to read flag defaults off, and a home
// only if the caller registers one. Both are states the production wiring never produces, and both are
// states the command has to refuse rather than answer from.
func runGenerate(t *testing.T, home string, args ...string) (string, error) {
	t.Helper()
	cmd := GenerateCmd()
	cmd.Flags().String(flags.FlagHome, home, "")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// TestGenerateRefusesWhatItCannotAnswerFrom covers the states where writing a file would be worse than
// writing none.
//
// Each one produces a plausible file rather than an error. That is what makes them worth a test: an
// operator reads a sei.toml this command printed and has no way to tell it describes their node from it
// describing a directory the command happened to be run in.
func TestGenerateRefusesWhatItCannotAnswerFrom(t *testing.T) {
	t.Run("no home is set", func(t *testing.T) {
		_, err := runGenerate(t, "", "--mode", "validator")
		if err == nil {
			t.Fatal("the command answered with no home set, so it read whichever config directory the " +
				"working directory holds and described some other node's files")
		}
		if !strings.Contains(err.Error(), "no home directory is set") {
			t.Errorf("the refusal says %q, and it has to name the home", err)
		}
	})

	t.Run("no start command to read flag defaults off", func(t *testing.T) {
		home := t.TempDir()
		if err := os.MkdirAll(filepath.Join(home, "config"), 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		_, err := runGenerate(t, home, "--mode", "validator")
		if err == nil {
			t.Fatal("the command answered without the start command's flags, so a key answered only by " +
				"a flag's default read as answered by nothing and the file would leave it out")
		}
		if !strings.Contains(err.Error(), "start") {
			t.Errorf("the refusal says %q, and it has to name the command it could not find", err)
		}
	})

	t.Run("no kind of node is given", func(t *testing.T) {
		_, err := runGenerate(t, t.TempDir())
		if err == nil {
			t.Fatal("the command answered with no kind of node given, so its values were chosen against " +
				"whichever defaults it picked")
		}
		if !strings.Contains(err.Error(), "--"+flagGenerateMode) {
			t.Errorf("the refusal says %q, and it has to name the flag that fixes it", err)
		}
	})

	t.Run("the kind of node is not one this binary declares", func(t *testing.T) {
		_, err := runGenerate(t, t.TempDir(), "--mode", "sentry")
		if err == nil {
			t.Fatal("the command accepted a kind of node it declares no defaults for, so every value in " +
				"the file was compared against nothing")
		}
		if !strings.Contains(err.Error(), "sentry") {
			t.Errorf("the refusal says %q, and it has to name what was given", err)
		}
	})
}

// TestGenerateNamesTheHomeVariableThatWorks holds the message to the resolver.
//
// The refusal tells an operator which variable sets the home, and a name that does not match what the
// resolver reads sends them to change something with no effect.
func TestGenerateNamesTheHomeVariableThatWorks(t *testing.T) {
	_, err := runGenerate(t, "", "--mode", "validator")
	if err == nil {
		t.Fatal("no refusal to read")
	}
	if !strings.Contains(err.Error(), theVariableThatSetsTheHome()) {
		t.Errorf("the refusal says %q and the resolver reads %s, so an operator following it sets a "+
			"variable nothing looks at", err, theVariableThatSetsTheHome())
	}
}

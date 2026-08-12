package configcli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/cmd/seid/cmd/configcli"
	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
)

// invoke runs one verb against a home directory and returns what it printed.
//
// The verbs are mounted under a parent that carries --home as a persistent flag, which is how the
// real tree provides it: the executor adds it to the root command at run time. Driven through cobra
// rather than by calling the verb functions, because the flags, the argument counts and the path
// each verb resolves are all part of what an operator uses and none of them are exercised by calling
// the functions directly.
func invoke(t *testing.T, home string, args ...string) (string, error) {
	t.Helper()
	parent := &cobra.Command{Use: "config", SilenceUsage: true, SilenceErrors: true}
	parent.PersistentFlags().String(flags.FlagHome, home, "The application home directory")
	parent.AddCommand(configcli.Verbs(home)...)

	var out bytes.Buffer
	parent.SetOut(&out)
	parent.SetErr(&out)
	parent.SetArgs(args)
	err := parent.Execute()
	return out.String(), err
}

// newHome returns an empty home with the config directory a node keeps.
func newHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return home
}

// TestTheVerbsWorkEndToEndThroughTheCommandTree walks an operator's whole path.
//
// Generate, then read it back, then change one value, then fall back again. Each step is asserted
// on the file rather than only on the output, because a verb that printed the right thing and wrote
// nothing would pass on the output alone.
func TestTheVerbsWorkEndToEndThroughTheCommandTree(t *testing.T) {
	registerTyped(t)
	home := newHome(t)
	path := configcli.Path(home)

	if out, err := invoke(t, home, "generate", "--mode", "validator"); err != nil {
		t.Fatalf("generate: %v\n%s", err, out)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("generate printed success and wrote no file: %v", err)
	}
	if got := valuesAt(t, path)["probe.workers"]; got != int64(4) {
		t.Errorf("the generated file holds workers=%#v, want the baseline 4", got)
	}

	// doctor accepts what generate wrote.
	if out, err := invoke(t, home, "doctor"); err != nil {
		t.Errorf("doctor refused the file generate wrote: %v\n%s", err, out)
	}

	// set changes one value and reports what it replaced.
	out, err := invoke(t, home, "set", "probe.workers", "16")
	if err != nil {
		t.Fatalf("set: %v\n%s", err, out)
	}
	if !strings.Contains(out, "4") || !strings.Contains(out, "16") {
		t.Errorf("set printed %q, which does not report the change from 4 to 16", out)
	}
	if got := valuesAt(t, path)["probe.workers"]; got != int64(16) {
		t.Errorf("after set the file holds %#v, want 16", got)
	}

	// diff now shows exactly one key away from the default.
	out, err = invoke(t, home, "diff", "--mode", "validator")
	if err != nil {
		t.Fatalf("diff: %v\n%s", err, out)
	}
	if !strings.Contains(out, "1 key(s) differ") || !strings.Contains(out, "probe.workers") {
		t.Errorf("diff printed %q, which does not name the one changed key", out)
	}

	// unset puts it back on the default.
	out, err = invoke(t, home, "unset", "probe.workers")
	if err != nil {
		t.Fatalf("unset: %v\n%s", err, out)
	}
	if _, present := valuesAt(t, path)["probe.workers"]; present {
		t.Error("the key is still written after unset")
	}
	out, err = invoke(t, home, "diff", "--mode", "validator")
	if err != nil {
		t.Fatalf("diff: %v\n%s", err, out)
	}
	if !strings.Contains(out, "follow this binary's defaults") {
		t.Errorf("diff printed %q, which does not report the key as following the default", out)
	}
}

// TestGenerateWillNotSilentlyReplaceAnExistingFile is the one destructive path in the tree.
//
// Generating over a file replaces every value in it, including the ones an operator set. Doing that
// without being asked would lose configuration that exists nowhere else.
func TestGenerateWillNotSilentlyReplaceAnExistingFile(t *testing.T) {
	registerTyped(t)
	home := newHome(t)
	path := configcli.Path(home)

	if out, err := invoke(t, home, "generate"); err != nil {
		t.Fatalf("generate: %v\n%s", err, out)
	}
	if _, err := invoke(t, home, "set", "probe.workers", "16"); err != nil {
		t.Fatalf("set: %v", err)
	}

	out, err := invoke(t, home, "generate")
	if err == nil {
		t.Fatalf("generate replaced an existing file without being asked:\n%s", out)
	}
	if got := valuesAt(t, path)["probe.workers"]; got != int64(16) {
		t.Errorf("the refused generate still changed the file: workers=%#v, want the 16 that was set",
			got)
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("the refusal does not say how to proceed: %v", err)
	}

	// And with --force it does replace it, or the refusal above would be a dead end.
	if out, err := invoke(t, home, "generate", "--force"); err != nil {
		t.Fatalf("generate --force: %v\n%s", err, out)
	}
	if got := valuesAt(t, path)["probe.workers"]; got != int64(4) {
		t.Errorf("after --force the file holds %#v, want the baseline 4", got)
	}
}

// TestDoctorExitsNonZeroOnAnUnrecognizedKey is what lets a deploy gate on it.
//
// A doctor that printed its findings and exited zero would be invisible to any automation, and the
// operator would have to read the output to know something is wrong.
func TestDoctorExitsNonZeroOnAnUnrecognizedKey(t *testing.T) {
	registerTyped(t)
	home := newHome(t)
	if err := os.WriteFile(configcli.Path(home),
		[]byte("schema_version = 1\n\n[probe]\nnot_a_key = 1\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	out, err := invoke(t, home, "doctor")
	if err == nil {
		t.Errorf("doctor exited zero on a file it does not recognize, so nothing automated can gate "+
			"on it:\n%s", out)
	}
	if !strings.Contains(out, "probe.not_a_key") {
		t.Errorf("doctor did not name the unrecognized key:\n%s", out)
	}
}

// TestUpgradeOnACurrentFileSaysSoAndWritesNothing is the ordinary case for the verb.
func TestUpgradeOnACurrentFileSaysSoAndWritesNothing(t *testing.T) {
	registerTyped(t)
	home := newHome(t)
	if out, err := invoke(t, home, "generate"); err != nil {
		t.Fatalf("generate: %v\n%s", err, out)
	}
	before, err := os.ReadFile(configcli.Path(home)) //nolint:gosec // a path under t.TempDir
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	out, err := invoke(t, home, "upgrade")
	if err != nil {
		t.Fatalf("upgrade: %v\n%s", err, out)
	}

	if !strings.Contains(out, "already on schema version") {
		t.Errorf("upgrade printed %q on a current file", out)
	}
	after, err := os.ReadFile(configcli.Path(home)) //nolint:gosec // a path under t.TempDir
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("upgrade rewrote a file that needed no steps")
	}
}

// TestEveryVerbRefusesAModeNoNodeRuns holds the flag's value against the registry.
//
// The mode reaches a baseline lookup, and a section's baseline function may answer for a mode its
// switch does not name. Accepting one would report a comparison against defaults no node uses.
func TestEveryVerbRefusesAModeNoNodeRuns(t *testing.T) {
	registerTyped(t)
	home := newHome(t)

	for _, verb := range []string{"generate", "diff"} {
		t.Run(verb, func(t *testing.T) {
			if out, err := invoke(t, home, verb, "--mode", "archival"); err == nil {
				t.Errorf("%s accepted a mode no node runs:\n%s", verb, out)
			}
		})
	}
	// The usage text lists the real modes, so it cannot name one the registry does not have.
	out, _ := invoke(t, home, "generate", "--help")
	for _, mode := range registry.Modes() {
		if !strings.Contains(out, string(mode)) {
			t.Errorf("the help text does not mention %q, so an operator cannot discover it:\n%s",
				mode, out)
		}
	}
}

// TestAVerbOnAMissingFileSaysWhichFile is the first thing a new operator hits.
//
// An error naming no path leaves them guessing which directory the tool looked in, and --home
// defaults to a location they may not have created.
func TestAVerbOnAMissingFileSaysWhichFile(t *testing.T) {
	registerTyped(t)
	home := newHome(t)

	for _, verb := range [][]string{{"show"}, {"doctor"}, {"diff"}, {"upgrade"},
		{"set", "probe.workers", "1"}, {"unset", "probe.workers"}} {
		t.Run(verb[0], func(t *testing.T) {
			_, err := invoke(t, home, verb...)
			if err == nil {
				t.Fatalf("%s succeeded with no file present", verb[0])
			}
			if !strings.Contains(err.Error(), configcli.FileName) {
				t.Errorf("%s failed with %q, which does not name the file it looked for", verb[0], err)
			}
		})
	}
}

// TestNoVerbDeclaresItsOwnHomeFlag is what keeps the inherited one reachable.
//
// The root command carries --home as a persistent flag. A verb declaring a local flag of the same
// name would shadow it, so `seid --home /data config generate` would bind the operator's path to the
// root's flag while the verb read its own default, and the file would be written somewhere else.
func TestNoVerbDeclaresItsOwnHomeFlag(t *testing.T) {
	for _, verb := range configcli.Verbs("/fallback") {
		if verb.Flags().Lookup(flags.FlagHome) != nil {
			t.Errorf("%q declares its own --home, which shadows the root's persistent flag. An "+
				"operator's --home would be ignored and the file written under the default",
				verb.Name())
		}
	}
}

// TestTheInheritedHomeFlagIsWhatDecidesThePath drives that property rather than inspecting flags.
func TestTheInheritedHomeFlagIsWhatDecidesThePath(t *testing.T) {
	registerTyped(t)
	home := newHome(t)
	elsewhere := newHome(t)

	// --home given to the parent, after the verb's own name, is still the one that decides.
	parent := &cobra.Command{Use: "config", SilenceUsage: true, SilenceErrors: true}
	parent.PersistentFlags().String(flags.FlagHome, elsewhere, "The application home directory")
	parent.AddCommand(configcli.Verbs(elsewhere)...)
	var out bytes.Buffer
	parent.SetOut(&out)
	parent.SetErr(&out)
	parent.SetArgs([]string{"generate", "--" + flags.FlagHome, home})
	if err := parent.Execute(); err != nil {
		t.Fatalf("generate: %v\n%s", err, out.String())
	}

	if _, err := os.Stat(configcli.Path(home)); err != nil {
		t.Errorf("generate did not write under the --home it was given: %v", err)
	}
	if _, err := os.Stat(configcli.Path(elsewhere)); err == nil {
		t.Error("generate wrote under the fallback home while --home named another, so an operator's " +
			"path is being ignored")
	}
}

// TestEveryVerbDeclaresItsArgumentCount keeps a mistyped invocation from being ignored.
//
// A verb with no argument count accepts extra arguments and silently drops them, so `set a b c`
// would write b and ignore c without saying anything.
func TestEveryVerbDeclaresItsArgumentCount(t *testing.T) {
	registerTyped(t)
	home := newHome(t)

	for _, args := range [][]string{
		{"set", "probe.workers"},
		{"set", "probe.workers", "1", "extra"},
		{"unset"},
		{"unset", "probe.workers", "extra"},
		{"doctor", "extra"},
		{"generate", "extra"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if _, err := invoke(t, home, args...); err == nil {
				t.Errorf("%v was accepted, so an operator's mistake is silently ignored", args)
			}
		})
	}
}

// contains reports whether ss holds want.
func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// walkTree visits cmd and every descendant.
func walkTree(cmd *cobra.Command, visit func(*cobra.Command)) {
	visit(cmd)
	for _, c := range cmd.Commands() {
		walkTree(c, visit)
	}
}

// TestEveryVerbHasHelpText keeps an undiscoverable verb out of the set.
//
// These commands are how an external operator configures a node, so a verb with no description is
// one they cannot use without reading the source.
func TestEveryVerbHasHelpText(t *testing.T) {
	for _, verb := range configcli.Verbs(t.TempDir()) {
		walkTree(verb, func(c *cobra.Command) {
			if c.Short == "" {
				t.Errorf("%q has no description", c.Name())
			}
		})
	}
}

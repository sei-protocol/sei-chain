package cli_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/cli"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// This is the third of seid's viper universes: the process-global singleton
// PrepareBaseCmd wires, with an empty env prefix. Every seid invocation runs
// BindFlagsLoadViper through it before anything else.
//
// The empty prefix is what makes it dangerous. InitEnv calls SetEnvPrefix("") plus
// AutomaticEnv, so viper looks up a bare upper-cased key name — and `home` looks up
// HOME, which every shell sets. AutomaticEnv outranks a flag's *default* (though not an
// explicitly-passed flag), so on a command line without --home the global viper resolves
// the operator's login home directory rather than the default the binary declared.
//
// The commands that read --home from this viper rather than from the server one —
// tendermint debug dump, debug kill, reset — therefore operate on $HOME unless --home is
// passed. That is sharp edge #1 in the manifest, and the reason every test in the config
// suite pins the environment.
//
// These tests mutate the global viper, so each resets it and none may run in parallel.

// withGlobalViper gives a test a clean global viper and a pinned environment.
//
// Both halves are necessary and they are bundled here rather than left to each caller,
// because a caller that forgets one is not obviously wrong. The viper reset is needed
// because the singleton has no save or restore of its own. The environment pin is needed
// in both directions: inbound, an empty env prefix makes a bare TRACE or HOME on the
// runner a config source for these very tests, and outbound, cli.InitEnv re-exports the
// whole environment through os.Setenv and restores nothing, so the duplicates would leak
// into every later test in this binary.
func withGlobalViper(t *testing.T) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)
	configtest.Isolate(t)
}

// newBaseCmd wires a command the way seid's entry point does — an empty env prefix and a
// declared default home — and parses args so cobra merges the persistent flags into
// cmd.Flags().
//
// The merge is not optional here. BindFlagsLoadViper binds cmd.Flags(), and cobra
// populates that set with persistent flags only during flag parsing; binding before the
// parse would bind an empty set, leaving home unbound and every lookup falling through to
// the environment. Getting that wrong makes the trap look worse than it is.
func newBaseCmd(t *testing.T, defaultHome string, args ...string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{
		Use:  "seid",
		RunE: func(*cobra.Command, []string) error { return nil },
	}
	cmd = cli.PrepareBaseCmd(cmd, "", defaultHome)
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("ParseFlags(%v): %v", args, err)
	}
	return cmd
}

// TestGlobalViperHomeIsOverriddenByTheHomeEnvVar pins the trap directly: with no --home
// on the command line, HOME wins over the declared default.
func TestGlobalViperHomeIsOverriddenByTheHomeEnvVar(t *testing.T) {
	withGlobalViper(t)

	declaredDefault := filepath.Join(t.TempDir(), "declared-default")
	loginHome := filepath.Join(t.TempDir(), "login-home")
	t.Setenv("HOME", loginHome)

	cmd := newBaseCmd(t, declaredDefault)
	cli.InitEnv("") // cobra.OnInitialize runs this during Execute; call it directly
	if err := cli.BindFlagsLoadViper(cmd, nil); err != nil {
		t.Fatalf("BindFlagsLoadViper: %v", err)
	}

	got := viper.GetString(cli.HomeFlag)
	if got == declaredDefault {
		t.Fatalf("the global viper resolved the declared default (%q). Narrowing the empty env "+
			"prefix so HOME no longer matches the home key changes which directory the tendermint "+
			"subcommands operate on, which is sharp edge #1, so it gets recorded here rather than "+
			"skipped past", got)
	}
	if got != loginHome {
		t.Fatalf("global viper home = %q, want the HOME value %q; a bare HOME matches the home "+
			"key under an empty env prefix", got, loginHome)
	}
}

// TestGlobalViperExplicitHomeFlagBeatsTheEnvVar pins the boundary of the trap. An
// explicitly-passed --home does outrank HOME, because a changed pflag sits above
// AutomaticEnv in viper's order. So the trap is specifically about relying on the
// default, which is what makes it easy to miss: it disappears the moment anyone passes
// the flag.
func TestGlobalViperExplicitHomeFlagBeatsTheEnvVar(t *testing.T) {
	withGlobalViper(t)

	declaredDefault := filepath.Join(t.TempDir(), "declared-default")
	loginHome := filepath.Join(t.TempDir(), "login-home")
	explicit := filepath.Join(t.TempDir(), "explicit")
	t.Setenv("HOME", loginHome)

	cmd := newBaseCmd(t, declaredDefault, "--home", explicit)
	cli.InitEnv("")
	if err := cli.BindFlagsLoadViper(cmd, nil); err != nil {
		t.Fatalf("BindFlagsLoadViper: %v", err)
	}

	if got := viper.GetString(cli.HomeFlag); got != explicit {
		t.Fatalf("global viper home = %q, want the explicit --home %q; an explicitly-set flag "+
			"must outrank the environment", got, explicit)
	}
}

// FuzzGlobalViperBareEnvVarsBecomeConfigSources generalizes the trap past home: under an
// empty prefix, any bare variable whose upper-cased name matches a bound key becomes a
// configuration source.
//
// trace is the one that bites in CI, where TRACE is a common variable name and the flag
// it collides with turns on full stack-trace printing.
func FuzzGlobalViperBareEnvVarsBecomeConfigSources(f *testing.F) {
	f.Add("trace", "true")
	f.Add("trace", "false")
	f.Add("trace", "1")
	f.Add("home", "/somewhere")

	f.Fuzz(func(t *testing.T, key, value string) {
		// Only the two keys PrepareBaseCmd binds are in play.
		if key != "trace" && key != "home" {
			return
		}
		if value == "" || len(value) > 64 {
			return
		}
		for _, r := range value {
			if r == 0 || r == '=' {
				return
			}
		}
		withGlobalViper(t)

		envName := "TRACE"
		if key == "home" {
			envName = "HOME"
		}
		t.Setenv(envName, value)

		cmd := newBaseCmd(t, filepath.Join(t.TempDir(), "declared-default"))
		cli.InitEnv("")
		if err := cli.BindFlagsLoadViper(cmd, nil); err != nil {
			t.Fatalf("BindFlagsLoadViper: %v", err)
		}

		if got := viper.GetString(key); got != value {
			t.Fatalf("%s=%q did not reach the global viper key %q (got %q); with an empty env "+
				"prefix a bare variable is a config source", envName, value, key, got)
		}
	})
}

// TestInitEnvDuplicatesTheEnvironmentUnderAnEmptyPrefix records the copy loop's behavior
// at the prefix seid actually uses.
//
// InitEnv exists to accept TMROOT as well as TM_ROOT. With an empty prefix every variable
// matches the prefix test and none matches the already-underscored test, so the loop
// re-exports the entire environment with a leading underscore — the process environment
// doubles in size at startup.
//
// The duplicates are not inert. AutomaticEnv under an empty prefix makes any variable a
// resolvable key, so _PATH is a viper key exactly as PATH is. The global viper's key space
// is therefore the whole environment, twice over, which is what makes "which keys can this
// viper answer for" an unanswerable question.
func TestInitEnvDuplicatesTheEnvironmentUnderAnEmptyPrefix(t *testing.T) {
	withGlobalViper(t)
	t.Setenv("SEI_CONFIGTEST_MARKER", "present")

	cli.InitEnv("")

	if os.Getenv("_SEI_CONFIGTEST_MARKER") != "present" {
		t.Fatal("InitEnv no longer duplicates variables under an empty prefix. Guarding the copy " +
			"loop is an improvement, and it shrinks the global viper's key space, so it gets " +
			"recorded here rather than skipped past")
	}
	// Both spellings resolve, because an empty prefix makes every variable a key.
	if got := viper.GetString("sei_configtest_marker"); got != "present" {
		t.Fatalf("the original variable must resolve as a viper key, got %q", got)
	}
	if got := viper.GetString("_sei_configtest_marker"); got != "present" {
		t.Fatalf("the duplicated variable must resolve too (%q); under an empty prefix the "+
			"duplication enlarges the viper key space rather than being inert", got)
	}
}

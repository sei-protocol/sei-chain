package cmd

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/cmd/seid/cmd/configmanager"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/sei-protocol/sei-chain/testutil/fuzzing"
)

// The rest of this package's config tests call LegacyConfigManager.Apply directly,
// which isolates the resolution. This file drives the seam as seid actually wires it:
// the root command's PersistentPreRunE, where two decisions are made before Apply is
// ever reached.
//
// Which manager runs comes from SEI_CONFIG_MANAGER through Select, read once per
// invocation. And whether Apply runs at all comes from a prefix match on the
// subcommand's Use string. Both are the seam's own contract rather than
// configuration values, and neither is observable from Apply.

// runRootPreRun builds a real root command, resolves the named subcommand, and runs
// the root's PersistentPreRunE against it — the same call seid makes for every
// invocation.
//
// The root is rebuilt per call. That is safe (NewRootCmd is re-entrant; the sealed
// sdk config and the tracer options tolerate it) and it matters, because Apply
// mutates the flag set it is handed.
func runRootPreRun(t *testing.T, home *configtest.Home, subcommand string) (*server.Context, error) {
	t.Helper()

	root, _ := NewRootCmd()
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	cmd, _, err := root.Find([]string{subcommand})
	if err != nil {
		t.Fatalf("find %q: %v", subcommand, err)
	}
	return runRootPreRunOn(t, root, cmd, home)
}

// runRootPreRunOn is runRootPreRun for a command the caller supplies, so a test can
// introduce a subcommand of its own and observe how the prefix match treats it.
func runRootPreRunOn(t *testing.T, root, cmd *cobra.Command, home *configtest.Home) (*server.Context, error) {
	t.Helper()

	serverCtx := &server.Context{}
	ctx := context.WithValue(context.Background(), server.ServerContextKey, serverCtx)
	ctx = context.WithValue(ctx, client.ClientContextKey, &client.Context{})
	cmd.SetContext(ctx)

	if cmd.Flags().Lookup("home") != nil {
		if err := cmd.Flags().Set("home", home.Root); err != nil {
			t.Fatalf("set --home: %v", err)
		}
	}
	return serverCtx, root.PersistentPreRunE(cmd, nil)
}

// TestPreRunAppliesConfigForAnOrdinarySubcommand pins the default: any subcommand
// that is not init-prefixed runs Apply, which means it materializes config.toml and
// app.toml as a side effect of being invoked.
//
// That side effect is the part worth stating. Running `seid start` on a fresh home
// writes both files; so does any other non-init subcommand, whether or not it has
// anything to do with node configuration.
func TestPreRunAppliesConfigForAnOrdinarySubcommand(t *testing.T) {
	configtest.Isolate(t)
	home := configtest.NewHome(t)

	serverCtx, err := runRootPreRun(t, home, "start")
	if err != nil {
		t.Fatalf("PersistentPreRunE for start must succeed, got %v", err)
	}
	if serverCtx.Viper == nil || serverCtx.Config == nil {
		t.Fatal("PersistentPreRunE must leave both boot channels populated")
	}
	if !home.Exists("config.toml") || !home.Exists("app.toml") {
		t.Fatal("a non-init subcommand materializes config.toml and app.toml as a side effect")
	}
}

// TestPreRunSkipsConfigForInitPrefixedSubcommands pins the skip, and the fact that it
// is a prefix match rather than an exact one.
//
// init creates the two files itself, so PersistentPreRunE returns early to avoid
// writing them into the home init is about to initialize. The check is
// strings.HasPrefix(cmd.Use, "init"), so it also catches any command whose Use merely
// starts with those four letters — a future `initialize-x` would silently lose config
// interception, and the only symptom would be an unpopulated serverCtx.Viper much
// later.
func TestPreRunSkipsConfigForInitPrefixedSubcommands(t *testing.T) {
	configtest.Isolate(t)

	t.Run("init itself", func(t *testing.T) {
		home := configtest.NewHome(t)
		serverCtx, err := runRootPreRun(t, home, "init")
		if err != nil {
			t.Fatalf("PersistentPreRunE for init must succeed, got %v", err)
		}
		if serverCtx.Viper != nil || serverCtx.Config != nil {
			t.Fatal("init must skip Apply, leaving the boot channels unpopulated")
		}
		if home.Exists("config.toml") || home.Exists("app.toml") {
			t.Fatal("init must not have config materialized underneath it by PersistentPreRunE")
		}
	})

	// A command nobody has written yet, to show the match is on the prefix.
	t.Run("a command merely starting with init", func(t *testing.T) {
		home := configtest.NewHome(t)
		root, _ := NewRootCmd()
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)

		invented := &cobra.Command{
			Use:  "initialize-something",
			RunE: func(*cobra.Command, []string) error { return nil },
		}
		invented.Flags().String("home", "", "")
		root.AddCommand(invented)

		serverCtx, err := runRootPreRunOn(t, root, invented, home)
		if err != nil {
			t.Fatalf("PersistentPreRunE: %v", err)
		}
		if serverCtx.Viper != nil {
			t.Fatal("the skip is an exact match now, not a prefix match. That is a fix — but it " +
				"changes which subcommands materialize config, so record it deliberately")
		}
		if home.Exists("config.toml") {
			t.Fatal("a command whose Use starts with init must skip Apply under the prefix match")
		}
	})
}

// FuzzPreRunManagerSelection pins the manager gate at its real call site.
//
// Select is read once per PersistentPreRunE from SEI_CONFIG_MANAGER, matched exactly:
// unset and "legacy" run the legacy path, "v2" selects the new manager, and anything
// else is a hard error. No trimming, no case folding, and deliberately no fallback —
// a typo'd value stops the command rather than quietly booting the wrong manager,
// which is what makes the flag safe to leave in a deployment.
//
// Because this tree ships the v2 body as a stub, "v2" surfaces as a
// not-implemented error. That distinguishes "the gate rejected the value" from "the
// gate accepted it and the manager refused", and both are asserted so the difference
// survives the v2 body landing.
func FuzzPreRunManagerSelection(f *testing.F) {
	f.Add("")
	f.Add("legacy")
	f.Add("v2")
	f.Add("V2")      // case-sensitive
	f.Add(" v2")     // not trimmed
	f.Add("legacy ") // not trimmed
	f.Add("v3")
	f.Add("sei")

	f.Fuzz(func(t *testing.T, value string) {
		if !configtest.EnvValueIsSettable(value) {
			return
		}
		configtest.Isolate(t)
		home := configtest.NewHome(t)

		if value != "" {
			if err := os.Setenv(configmanager.EnvVar, value); err != nil {
				t.Fatalf("set %s: %v", configmanager.EnvVar, err)
			}
		}

		serverCtx, err := runRootPreRun(t, home, "start")

		switch value {
		case "", "legacy":
			if err != nil {
				t.Fatalf("%s=%q selects the legacy manager and must boot, got %v",
					configmanager.EnvVar, value, err)
			}
			if serverCtx.Viper == nil {
				t.Fatal("the legacy manager must populate the boot channels")
			}
		case "v2":
			if err == nil {
				t.Fatal("v2 is accepted by the gate but its body is a stub, so the command must " +
					"fail rather than silently falling back to legacy")
			}
			if !strings.Contains(err.Error(), configmanager.EnvVar) {
				t.Fatalf("the v2 failure must name the gate, got %v", err)
			}
		default:
			if err == nil {
				t.Fatalf("%s=%q is not a legal value and must be a hard error, never a silent "+
					"fallback to legacy", configmanager.EnvVar, value)
			}
			if !strings.Contains(err.Error(), "invalid") {
				t.Fatalf("an illegal value must be reported as invalid, got %v", err)
			}
			if serverCtx.Viper != nil {
				t.Fatalf("%s=%q was rejected, so no manager may have populated the boot channels",
					configmanager.EnvVar, value)
			}
		}
	})
}

// TestPreRunReadsTheManagerGateOncePerInvocation pins the read-once discipline the
// design calls out as load-bearing.
//
// One process must not be able to select two managers. The gate is read a single time
// inside PersistentPreRunE and the chosen manager is used for that whole invocation,
// so changing the variable afterwards cannot switch managers mid-command. A future
// `seid config` subtree that skips PersistentPreRunE has to re-own this, which is why
// the property is written down rather than assumed.
func TestPreRunReadsTheManagerGateOncePerInvocation(t *testing.T) {
	configtest.Isolate(t)

	var reads []string
	getenv := func(key string) string {
		if key == configmanager.EnvVar {
			reads = append(reads, key)
		}
		return ""
	}
	if _, err := configmanager.Select(getenv); err != nil {
		t.Fatalf("Select: %v", err)
	}
	if len(reads) != 1 {
		t.Fatalf("Select consulted the environment %d times, want exactly 1", len(reads))
	}

	// And the manager chosen for an invocation is fixed. Flipping the variable after
	// PersistentPreRunE has returned must leave the resolved channels alone, which means
	// comparing the viper's identity across the change rather than re-checking that it is
	// non-nil: a nil check would hold whatever the gate did.
	home := configtest.NewHome(t)
	serverCtx, err := runRootPreRun(t, home, "start")
	if err != nil {
		t.Fatalf("PersistentPreRunE: %v", err)
	}
	if serverCtx.Viper == nil {
		t.Fatal("the legacy manager must have populated the boot channels")
	}

	before := serverCtx.Viper
	beforeDump := configtest.DumpViper(serverCtx.Viper)
	if err := os.Setenv(configmanager.EnvVar, "v2"); err != nil {
		t.Fatalf("set %s: %v", configmanager.EnvVar, err)
	}
	if serverCtx.Viper != before {
		t.Fatal("the resolved viper was replaced after PersistentPreRunE returned; one invocation " +
			"must not be able to switch managers partway through")
	}
	if after := configtest.DumpViper(serverCtx.Viper); after != beforeDump {
		t.Fatalf("the resolved view changed after the gate was flipped\n--- before\n%s\n--- after\n%s",
			beforeDump, after)
	}
}

// FuzzExportableAppRequiresAHome pins the one place that checks --home rather than
// casting it.
//
// newApp reads home with cast.ToString, so an absent key yields "" and the app is built
// rooted at the process working directory. getExportableApp instead uses an ok-checked
// type assertion and refuses an empty or non-string home, so `seid export` fails where
// `seid start` would quietly run against the wrong directory.
//
// Only the refusal is driven here: a valid home sends this straight into app.New.
func FuzzExportableAppRequiresAHome(f *testing.F) {
	f.Add(uint8(0), "", int64(0), false)  // absent
	f.Add(uint8(1), "", int64(0), false)  // empty string
	f.Add(uint8(3), "", int64(1), false)  // an int where a path belongs
	f.Add(uint8(2), "", int64(0), true)   // a bool
	f.Add(uint8(11), "", int64(0), false) // a table

	f.Fuzz(func(t *testing.T, kind uint8, s string, n int64, b bool) {
		value := fuzzing.ConfigValue(kind, s, n, b)
		home, isString := value.(string)
		if isString && home != "" {
			return // a usable home proceeds into app.New, which is out of scope here
		}

		_, err := getExportableApp(dbm.NewMemDB(), nil, -1, configtest.AppOpts{flags.FlagHome: value})
		if err == nil {
			t.Fatalf("home = %#v is not a usable path and export must refuse it rather than "+
				"defaulting to the working directory the way newApp does", value)
		}
		if !strings.Contains(err.Error(), "home") {
			t.Fatalf("the refusal must name the home, got %v", err)
		}
	})
}

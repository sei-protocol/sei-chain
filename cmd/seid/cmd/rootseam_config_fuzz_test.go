package cmd

import (
	"context"
	"io"
	"testing"

	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// The rest of this package's config tests call server.InterceptConfigsPreRunHandler
// (Apply) directly, which isolates the resolution. This file drives the seam as seid
// actually wires it: the root command's PersistentPreRunE, which decides whether Apply
// runs at all from a prefix match on the subcommand's Use string. That is the seam's
// own contract rather than a configuration value, and it is not observable from Apply.

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

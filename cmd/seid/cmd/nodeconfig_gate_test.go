package cmd

import (
	"testing"

	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/cmd/seid/cmd/configcli"
	"github.com/sei-protocol/sei-chain/cmd/seid/cmd/configmanager"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	tmcli "github.com/sei-protocol/sei-chain/sei-tendermint/libs/cli"
)

// The sei.toml verbs live under the config command an operator already uses, and appear only where
// the v2 configuration manager is selected.
//
// Sharing that command is only safe while two things hold. Every verb must be reachable, and every
// form the client configuration already answers to must keep working. Cobra resolves a subcommand
// before it treats an argument as positional, so the two coexist exactly as long as no verb is
// named the same as a client configuration key.

// configCommand returns the config command out of the assembled tree.
func configCommand(t *testing.T) *cobra.Command {
	t.Helper()
	root, _ := NewRootCmd()

	for _, c := range root.Commands() {
		if c.Name() == "config" {
			return c
		}
	}
	t.Fatal("the assembled tree has no config command")
	return nil
}

// TestTheVerbsJoinTheConfigCommandOnlyUnderTheV2Manager holds the gate in both directions.
func TestTheVerbsJoinTheConfigCommandOnlyUnderTheV2Manager(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"v2", true},
		{"legacy", false},
		{"", false},
		// A value Select refuses must not add the verbs, and must not stop the tree being built:
		// the boot's pre-run hook reports the bad value, and failing during assembly would break
		// seid --help for a typo.
		{"nonsense", false},
	} {
		t.Run("SEI_CONFIG_MANAGER="+tc.value, func(t *testing.T) {
			t.Setenv(configmanager.EnvVar, tc.value)

			var names []string
			for _, c := range configCommand(t).Commands() {
				names = append(names, c.Name())
			}

			if got := len(names) > 0; got != tc.want {
				if tc.want {
					t.Errorf("the config command carries no sei.toml verbs under %s=%q, so a v2 node "+
						"cannot generate or inspect its configuration", configmanager.EnvVar, tc.value)
					return
				}
				t.Errorf("the config command carries %v under %s=%q. These edit a file this manager "+
					"does not read, so an operator would configure a node and see no effect",
					names, configmanager.EnvVar, tc.value)
			}
			if !tc.want {
				return
			}
			for _, want := range configcli.VerbNames("") {
				if !holds(names, want) {
					t.Errorf("the config command has no %q verb; it has %v", want, names)
				}
			}
		})
	}
}

// TestNoVerbTakesOverAClientConfigurationKey is what makes sharing the command safe.
//
// The client configuration reads its argument as a key, so `seid config chain-id` means get, and
// `seid config chain-id sei-1` means set. Cobra prefers a subcommand to a positional argument, so a
// verb named after one of those keys would take the command over and the operator's get would
// silently become something else.
func TestNoVerbTakesOverAClientConfigurationKey(t *testing.T) {
	// The keys the client configuration answers to, from the flags it is built on.
	clientKeys := []string{
		flags.FlagChainID,
		flags.FlagKeyringBackend,
		flags.FlagOutput,
		flags.FlagNode,
		flags.FlagBroadcastMode,
	}

	for _, verb := range configcli.VerbNames("") {
		if holds(clientKeys, verb) {
			t.Errorf("the verb %q is also a client configuration key. Cobra resolves the subcommand "+
				"first, so `seid config %s` would stop reporting that setting and run the verb "+
				"instead", verb, verb)
		}
	}
	if len(clientKeys) == 0 || len(configcli.VerbNames("")) == 0 {
		t.Fatal("one of the two lists is empty, so this comparison would hold for any pair of names")
	}
}

// TestTheClientConfigurationStillResolvesAlongsideTheVerbs drives the real tree.
//
// The check above compares names; this one confirms cobra actually routes each form where it should,
// which is the part a name comparison cannot prove.
func TestTheClientConfigurationStillResolvesAlongsideTheVerbs(t *testing.T) {
	t.Setenv(configmanager.EnvVar, "v2")
	root, _ := NewRootCmd()
	// --home is added by the executor at run time, not while commands are assembled, so the test
	// adds it here for the same reason the real binary has it.
	root.PersistentFlags().StringP(tmcli.HomeFlag, "", t.TempDir(), "directory for config and data")

	for _, tc := range []struct {
		name string
		args []string
		want string // the command path the arguments must resolve to
	}{
		{"a client get", []string{"config", "chain-id"}, "seid config"},
		{"a client set", []string{"config", "chain-id", "sei-1"}, "seid config"},
		{"the client dump", []string{"config"}, "seid config"},
		{"a sei.toml verb", []string{"config", "generate"}, "seid config generate"},
		{"a sei.toml verb with args", []string{"config", "set", "a.b", "1"}, "seid config set"},
		{"another verb", []string{"config", "doctor"}, "seid config doctor"},
		{"upgrade", []string{"config", "upgrade"}, "seid config upgrade"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			found, _, err := root.Find(tc.args)
			if err != nil {
				t.Fatalf("Find(%v): %v", tc.args, err)
			}
			if got := found.CommandPath(); got != tc.want {
				t.Errorf("%v resolves to %q, want %q. Sharing one command only works while every "+
					"form routes where an operator expects", tc.args, got, tc.want)
			}
		})
	}
}

// TestAClientKeyIsStillAcceptedAsAnArgument holds the argument counts the shared command needs.
//
// The client configuration takes none, one or two arguments. Mounting subcommands must not narrow
// that, or `seid config chain-id sei-1` would start failing on a node that has the verbs.
func TestAClientKeyIsStillAcceptedAsAnArgument(t *testing.T) {
	t.Setenv(configmanager.EnvVar, "v2")
	cmd := configCommand(t)

	for _, args := range [][]string{{}, {"chain-id"}, {"chain-id", "sei-1"}} {
		if err := cmd.ValidateArgs(args); err != nil {
			t.Errorf("the config command refuses %v: %v. That is a form operators already use",
				args, err)
		}
	}
	// And three arguments are still refused, so the count is a real constraint rather than absent.
	if err := cmd.ValidateArgs([]string{"a", "b", "c"}); err == nil {
		t.Error("the config command accepts three arguments, so its own argument checking is gone")
	}
}

// TestABadManagerValueStillBuildsTheCommandTree keeps a typo from breaking help.
func TestABadManagerValueStillBuildsTheCommandTree(t *testing.T) {
	t.Setenv(configmanager.EnvVar, "nonsense")

	root, _ := NewRootCmd()

	if root == nil || len(root.Commands()) == 0 {
		t.Fatal("the tree has no commands, so seid --help would show nothing for a mistyped variable")
	}
	if _, err := configmanager.Select(func(string) string { return "nonsense" }); err == nil {
		t.Error("an invalid manager value was accepted, so the gate would silently pick a default")
	}
}

// holds reports whether ss contains want.
func holds(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

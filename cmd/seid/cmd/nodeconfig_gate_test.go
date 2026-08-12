package cmd

import (
	"testing"

	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/cmd/seid/cmd/configmanager"
)

// The sei.toml verbs are reachable only where the v2 configuration manager is selected.
//
// They act on a file no other manager reads, so on a node running the legacy manager the whole
// subtree would be a set of commands that edit a file nothing consults. An operator who found them
// in the help output would configure a node and see no effect.

// nodeConfigPresent reports whether the assembled tree carries the sei.toml subtree.
func nodeConfigPresent(t *testing.T) bool {
	t.Helper()
	root, _ := NewRootCmd()

	var found bool
	walk(root, func(c *cobra.Command) {
		if c.Name() == "node-config" {
			found = true
		}
	})
	return found
}

// TestTheNodeConfigTreeAppearsOnlyUnderTheV2Manager holds the gate in both directions.
//
// Asserted on the assembled tree rather than on the wiring, because only the real tree knows what
// a command's name resolves to once every other command has been added.
func TestTheNodeConfigTreeAppearsOnlyUnderTheV2Manager(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"v2", true},
		{"legacy", false},
		{"", false},
		// A value Select refuses must not add the subtree either, and must not stop the tree being
		// built: the boot's pre-run hook reports the bad value, and failing during assembly would
		// break seid --help for a typo.
		{"nonsense", false},
	} {
		t.Run("SEI_CONFIG_MANAGER="+tc.value, func(t *testing.T) {
			t.Setenv(configmanager.EnvVar, tc.value)

			if got := nodeConfigPresent(t); got != tc.want {
				if tc.want {
					t.Errorf("the sei.toml verbs are absent under %s=%q, so a v2 node has no way to "+
						"generate or inspect its configuration", configmanager.EnvVar, tc.value)
					return
				}
				t.Errorf("the sei.toml verbs are present under %s=%q. They edit a file this manager "+
					"does not read, so an operator would configure a node and see no effect",
					configmanager.EnvVar, tc.value)
			}
		})
	}
}

// TestABadManagerValueStillBuildsTheCommandTree keeps a typo from breaking help.
//
// Selection errors belong to the boot's pre-run hook, which reports the value it saw. Raising one
// while assembling commands would make seid --help fail for a mistyped variable, and the message
// would arrive from the wrong place.
func TestABadManagerValueStillBuildsTheCommandTree(t *testing.T) {
	t.Setenv(configmanager.EnvVar, "nonsense")

	root, _ := NewRootCmd()

	if root == nil {
		t.Fatal("the command tree was not built at all")
	}
	if len(root.Commands()) == 0 {
		t.Error("the tree has no commands, so seid --help would show nothing for a mistyped variable")
	}
	// And the value is still refused where it is read.
	if _, err := configmanager.Select(func(string) string { return "nonsense" }); err == nil {
		t.Error("an invalid manager value was accepted, so the gate above would silently pick a default")
	}
}

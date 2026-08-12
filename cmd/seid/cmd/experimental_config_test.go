package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/cmd/seid/cmd/configmanager"
	"github.com/sei-protocol/sei-chain/config/experimental"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// The registry-only checks for [experimental], run from the one binary that can see every
// declaration.
//
// They live here rather than in config/experimental's own test because that package's import graph
// contains zero feature packages: two teams declaring one name would pass a leaf-package test
// clean. This test binary links the whole node, so Declarations() reflects what a seid binary
// actually recognizes.
//
// A caveat worth knowing when reading a failure. This binary's import graph is a superset of
// seid's, since cmd/seid/main.go imports cmd plus this file's test-only imports. It is a tight
// approximation, not an identity.

// selfCheck is declared here, in a test file, for one reason: the registry ships empty, so the
// record below would compare clean even if registration never appended anything. This key is what
// makes the record self-validating.
//
// It is declared at package scope, which is the shape a real caller uses, and it is deliberately
// well-formed so the declaration checks have something valid to pass over.
var selfCheck = experimental.Int(experimental.Decl[int]{
	Name:    "configtest.self_check",
	Default: 7,
	Owner:   "configtest",
	Since:   "v6.6.0",
})

// TestExperimentalDeclarationsAreWellFormed is the registry-only gate.
//
// Every rule registration could judge without running caller code is already a defect by the time
// this runs; this turns each into a build failure. It also runs each declaration's own Check
// against its own default, which is the rule registration must not assert, because doing so means
// running caller code before main.
func TestExperimentalDeclarationsAreWellFormed(t *testing.T) {
	configtest.CheckExperimentalDeclarations(t,
		experimental.Declarations(),
		experimental.Defects(),
		experimental.Checkers(),
		experimental.Tombstones(),
	)
}

// TestExperimentalRegistryReachesDeclarations is what keeps the record honest.
//
// An empty registry renders an empty record, and an empty record compares clean against a registry
// whose registration silently stopped appending. Asserting that a key declared in this file arrives
// in Declarations() is the only thing that distinguishes the two.
func TestExperimentalRegistryReachesDeclarations(t *testing.T) {
	want := selfCheck.Name()
	if want == "" {
		t.Fatal("the self-check key has no name, so its declaration was refused and this test would " +
			"pass for a registry that recorded nothing")
	}

	for _, d := range experimental.Declarations() {
		if d.Name == want {
			if d.Owner == "" || d.Since == "" || d.Type != "int" || d.Default != "7" {
				t.Errorf("the self-check key registered as %+v, which does not carry what was declared. "+
					"The record renders these fields, so a wrong one is a wrong record", d)
			}
			return
		}
	}
	t.Fatalf("the key declared in this file did not reach Declarations(): %v.\n\nEvery other "+
		"assertion about the registry is vacuous while this fails, because an empty registry and a "+
		"broken registration are indistinguishable.", experimental.Declarations())
}

// TestExperimentalRegistryMatchesTheRecord records the registry, keyed by name.
//
// A key added, removed, renamed, re-typed, re-owned or re-defaulted changes what this binary
// recognizes, which is what an operator's file is written against. The record is deliberately
// outside the schema fingerprint: an experimental key may change shape in a patch release, so this
// exists to make the change visible rather than to freeze it.
func TestExperimentalRegistryMatchesTheRecord(t *testing.T) {
	configtest.CheckExperimentalGolden(t, "experimental",
		experimental.Declarations(), experimental.Tombstones())
}

// TestOnlyApplicationCommandsSweep holds A-2 against the real command tree.
//
// Asserted on the assembled tree rather than a list of strings, because the gate compares
// CommandPath() and only the real tree knows what those are. The two export commands are the point:
// server/export.go and client/keys/export.go are both named export, so a gate matching on name or
// prefix would emit records into a stream carrying an armored private key.
func TestOnlyApplicationCommandsSweep(t *testing.T) {
	root, _ := NewRootCmd()

	var swept, skipped []string
	walk(root, func(c *cobra.Command) {
		if configmanager.SweepsExperimental(c) {
			swept = append(swept, c.CommandPath())
			return
		}
		skipped = append(skipped, c.CommandPath())
	})

	if len(swept) == 0 {
		t.Fatal("no command sweeps, so the report is unreachable and every other assertion about it " +
			"describes code that never runs")
	}
	for _, path := range swept {
		if !strings.HasPrefix(path, "seid ") || strings.Count(path, " ") != 1 {
			t.Errorf("%q sweeps but is not a top-level seid command; only commands that construct an "+
				"application read appOpts", path)
		}
	}
	// The one that must never sweep, named explicitly rather than left to the shape rule above.
	for _, path := range skipped {
		if path == "seid keys export" {
			return
		}
	}
	t.Error("seid keys export was not found in the assembled tree, so this test no longer holds the " +
		"case it exists for. If the command moved, point this at its new path rather than deleting it.")
}

// walk visits cmd and every descendant.
func walk(cmd *cobra.Command, visit func(*cobra.Command)) {
	visit(cmd)
	for _, c := range cmd.Commands() {
		walk(c, visit)
	}
}

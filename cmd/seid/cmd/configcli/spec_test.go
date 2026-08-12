//go:build configspec

package configcli_test

import "testing"

// What the seid config command tree must do, written before it is built.
//
// Six verbs over one versioned file: generate, set, unset, doctor, upgrade, diff. This is also
// where sei.toml is first materialized, and where the rule that a shipped migration never
// changes lands.
//
// One thing an implementer should not build. Every existing operator-facing key keeps its
// current name, so the keys whose unused struct tag would have derived a cleaner spelling need
// no migration at all. A rename migration for ss-keep-recent, ss-enable, ss-prune-interval,
// evm-ss-split or sc-async-commit-buffer would transform a file that was already correct. The
// migration chain exists for shape changes that actually happen.
//
// Out of scope, and each has a later home:
//
//	adoption on our own infrastructure               after this lands
//	flipping the default so unset means v2           after adoption
//	deleting the legacy path                         last
//
// How much of the file generate writes is settled: every declared key, at the baseline for the
// mode it was given. That has a consequence worth stating rather than discovering, because a
// written value is a commitment this binary never rewrites. A generated node keeps the values it
// was given even where a later release ships a different default, and regenerating is what moves
// it forward. The generated file says so in its own header, since an operator otherwise cannot
// tell a value they chose from one generate filled in.
//
// Adoption is reached as generate --from-legacy rather than as a verb of its own, because both
// produce the file a node starts from and the only difference is where the values come from. The
// behaviour is in Adopt, so moving it to its own verb later changes how it is reached and nothing
// else. That spelling is still open to a decision; adopt is free as a verb name, since no client
// configuration key uses it.
//
// Six verbs are built, and their tests live beside the code rather than here. They are mounted as
// subcommands of the existing config command, which already reads and writes client.toml, so every
// configuration verb is in one place. Cobra resolves a subcommand before it treats an argument as
// positional, so config generate reaches the verb while config chain-id still means the client
// setting it always meant. That holds only while no verb is named after a client configuration key,
// which a test compares. The verbs appear only where the v2 manager is selected, because they act on
// a file no other manager reads. What remains below is what is not built yet.

// TestTheKeySetIsObservedNotHandListed is what makes the completeness claim above mean anything.
//
// 154 of the 481 keys in this tree are findable only at runtime, so a hand-written list of what
// generate must cover is a guess. The key set generate is held against is recorded instead, by
// driving a boot and observing every key the node asks for.
func TestTheKeySetIsObservedNotHandListed(t *testing.T) {
	t.Fatal("unimplemented: needs a recording view over a driven boot")
}

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
// One question is still open, and an implementer must not settle it by picking whichever reading
// is convenient: how a node adopts, a --from-legacy flag on generate against a separate adopt
// verb. The adoption test below is written against the behaviour rather than the spelling, so it
// holds either way.
//
// Six verbs are built and mounted, and their tests live beside the code rather than here. The tree
// is named node-config, not config: the client configuration command already owns that name and
// writes client.toml, and two unrelated files under one verb would leave an operator unable to tell
// which one they had just edited. What remains below is what is not built yet.

// TestAdoptionCarriesLegacyValuesOverAsWrittenValues is how a node that already exists moves.
//
// Resolving the node's existing configuration files rather than the binary's baselines, so its
// current settings carry over as written values instead of being silently re-baselined.
// Environment variables and flags sit above the file and are never folded into it, so adoption
// reports them instead, and this asserts that report exists.
func TestAdoptionCarriesLegacyValuesOverAsWrittenValues(t *testing.T) {
	t.Fatal("unimplemented: the adoption path is not built")
}

// TestTheKeySetIsObservedNotHandListed is what makes the completeness claim above mean anything.
//
// 154 of the 481 keys in this tree are findable only at runtime, so a hand-written list of what
// generate must cover is a guess. The key set generate is held against is recorded instead, by
// driving a boot and observing every key the node asks for.
func TestTheKeySetIsObservedNotHandListed(t *testing.T) {
	t.Fatal("unimplemented: needs a recording view over a driven boot")
}

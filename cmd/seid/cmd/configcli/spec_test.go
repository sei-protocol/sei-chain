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
// Two questions are open inside this scope, and an implementer must not settle either by
// picking whichever reading is convenient. Both need a decision from the people who own the
// generate path.
//
// First, how a node adopts: a --from-legacy flag on generate, or a separate adopt verb. The
// adoption test below is written against the behaviour rather than the spelling, so it holds
// either way.
//
// Second, how much of the file to materialize. A complete configuration runs to a hundred-plus
// keys, and writing all of them may cost more legibility than the complete view buys. Whether
// to group, fold or elide sections that provably do nothing has to be measured against a real
// rendered file rather than argued. The first test below demands completeness because that is
// what is agreed today; if the measurement changes it, that test changes with it in the same
// change.

// TestGenerateWritesAResolvedFileForTheDeclaredMode is the verb an external node operator
// reaches for, and the only one they should need.
//
// Every stable key at the running binary's baseline for the declared mode, so the file is the
// complete picture of what the node runs at generation time.
func TestGenerateWritesAResolvedFileForTheDeclaredMode(t *testing.T) {
	t.Fatal("unimplemented: generate is not built")
}

// TestDoctorHaltsOnAnUnrecognizedStableKeyAndWarnsOnExperimental is the asymmetry that makes
// the experimental namespace worth having.
//
// A written stable value the binary does not recognize is a broken promise and halts with the
// key named. An experimental key warns and boots. An unwritten key is healthy by definition,
// since it resolves to the baseline. All three directions are asserted, because checking only
// the halt would pass for a doctor that halted on everything.
func TestDoctorHaltsOnAnUnrecognizedStableKeyAndWarnsOnExperimental(t *testing.T) {
	t.Fatal("unimplemented: doctor is not built")
}

// TestUpgradeRunsTheFrozenChainAtomicallyPerStep holds what upgrade promises.
//
// One atomic write per schema step, reported as a diff, with --dry-run previewing the same diff
// without writing. Defaults are not part of upgrade, since baselines apply on their own. Held
// on a file two schema versions behind, so a single-step implementation fails.
func TestUpgradeRunsTheFrozenChainAtomicallyPerStep(t *testing.T) {
	t.Fatal("unimplemented: upgrade and the migration registry are not built")
}

// TestAShippedMigrationIsImmutable is what makes a migration safe to run everywhere.
//
// A migration that has shipped never changes, so every node transforms its configuration the
// same way and no node ends up with a variant of the file nobody else has. Enforced
// mechanically, because a reviewer cannot be relied on to notice an edit to history.
func TestAShippedMigrationIsImmutable(t *testing.T) {
	t.Fatal("unimplemented: the frozen-migration check is not built")
}

// TestSetAndUnsetRoundTripThroughWrittenValues holds what the two verbs mean.
//
// set writes a value, unset removes the key so it resolves back to the baseline. Hand-editing
// the file is equally legitimate, so these are conveniences and the file is what counts.
// Asserted by unsetting a key whose baseline differs from the value that was set, since
// otherwise the before and after are indistinguishable.
func TestSetAndUnsetRoundTripThroughWrittenValues(t *testing.T) {
	t.Fatal("unimplemented: set and unset are not built")
}

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

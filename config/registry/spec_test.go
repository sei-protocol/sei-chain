//go:build configspec

package registry_test

import "testing"

// PR4 of the ConfigManager stack: the stable registry and mode-based defaults.
//
// Scope, from the design's Declaring Configuration Values and Data Model sections. One
// registration per section carries the struct and a baseline function of node mode. Key
// identity is derived, never hand-typed. Resolution runs in one fixed order, and the schema
// fingerprint hashes the registrations.
//
// This is requirement 2 of the design, mode-based defaults with legible failure reasons, and
// it is the prerequisite for PR5's generate.
//
// Depends on PR3 for the exclusion boundary: gate 4 below asserts the fingerprint covers
// stable registrations, and PR3's gate 4 asserts experimental keys stay out of it. Neither
// half means anything alone.
//
// Out of scope, and each has a later home:
//
//	sei.toml on disk, and every CLI verb            PR5
//	migrations and the frozen chain                 PR5
//	the experimental registry                       PR3, below this branch
//	retiring the legacy path                        after adoption
//
// One thing this PR must NOT do, and it is the correction that reshaped this stack. The
// design forbids feeding app.New from an in-memory struct, because a struct silently drops
// keys it does not model and a round-trip test passes while being wrong. The transport stays
// a fresh viper carrying every resolved key at override precedence, and no appOpts.Get call
// site changes. The registry is the authoring, validation and declaration surface. It is not
// the boot input.

// TestGate1KeyIdentityIsDerivedNeverHandTyped holds the property the whole registry exists
// for.
//
// The dotted key comes from the section name and the field's mapstructure tag. The legacy
// path already reads that same tag, since viper decodes these structs through mapstructure,
// so both managers take key identity from one declaration and neither can drift from the
// other. A hand-typed string anywhere in the derivation defeats that.
func TestGate1KeyIdentityIsDerivedNeverHandTyped(t *testing.T) {
	t.Fatal("unimplemented: RegisterSection and the dotted-key derivation are not built")
}

// TestGate2ABaselineVariesByModeAndAnAbsentKeyTracksIt is requirement 2.
//
// A section's baseline is a function of mode, so the same absent key resolves differently on
// an archive node and a validator. Held on a key whose baseline actually differs between two
// modes, since a key with one baseline everywhere would pass against a registry that ignored
// mode entirely.
func TestGate2ABaselineVariesByModeAndAnAbsentKeyTracksIt(t *testing.T) {
	t.Fatal("unimplemented: Mode and the per-mode baseline function are not built")
}

// TestGate3ResolutionRunsInTheDeclaredOrder pins the design's Data Model order: the binary's
// baseline, then the file, then environment variables, then CLI flags.
//
// Falsifiable by shuffling which layers are present and asserting the winner does not move.
// The legacy path fails this by construction, since its order emerges from which viper
// instance a caller asked, which is why two orders are observable across the key set.
func TestGate3ResolutionRunsInTheDeclaredOrder(t *testing.T) {
	t.Fatal("unimplemented: the layering is not built")
}

// TestGate4TheFingerprintMovesOnAStableChangeAndOnlyThen is what makes forgetting a schema
// bump impossible.
//
// Both directions, because either alone is useless. Adding, renaming or retyping a stable
// key moves the fingerprint, so CI can demand the bump and the migration in the same PR.
// Registering an experimental key does not, which is PR3's gate 4 seen from this side.
func TestGate4TheFingerprintMovesOnAStableChangeAndOnlyThen(t *testing.T) {
	t.Fatal("unimplemented: the fingerprint is not built")
}

// TestGate5TheEnvironmentSpellingIsCanonicalAndPinned closes a measured legacy defect.
//
// The legacy path answers to three environment universes, and its prefix is derived from the
// running binary's filename through path.Base(os.Executable()), so renaming the binary moves
// the whole namespace. One canonical spelling with a pinned prefix, derived from the
// registration rather than from argv, is the fix.
func TestGate5TheEnvironmentSpellingIsCanonicalAndPinned(t *testing.T) {
	t.Fatal("unimplemented: the env spelling derivation is not built")
}

// ---------------------------------------------------------------------------------------
// Derivation agreement. V1 resolves by explicit dotted string, V2 by walking a struct, so
// comparing values is not enough: a V2 tag typo produces a key V1 never reads, and a
// value-only comparison catches that only if a test happens to drive that key.
//
// DECIDED: V2 keeps every existing operator-facing name. Where an inert V1 tag would have
// addressed a cleaner spelling, the cleaner spelling is abandoned rather than migrated. The
// design's exemplar table left four of these open, and this is the answer to all four.
//
// Two consequences worth stating. Those keys need no migration, which is requirement 1
// satisfied by construction rather than by a transform. And a rename stops being a quiet
// tag edit: the gates below fail on any derived key that does not equal the spelling V1
// resolves, so a deliberate rename has to arrive with an explicit ratified entry saying so.
// ---------------------------------------------------------------------------------------

// v1Spellings are the operator-facing keys whose inert V1 tag would derive something else.
// Read from the tree by the design's Appendix E, and the reason each is here is that a
// tag-driven binder is the first thing in this tree that would ever read the tag.
var v1Spellings = []struct {
	operatorWrites string // what resolves today, and what V2 must keep
	inertTagWould  string // what the V1 tag would have addressed, now abandoned
	why            string
}{
	{"state-store.ss-keep-recent", "state-store.keep-recent", "prefix-strip"},
	{"state-store.ss-enable", "state-store.enable", "prefix-strip"},
	{"state-store.ss-prune-interval", "state-store.prune-interval-seconds", "word substitution, not just a prefix"},
	{"state-store.evm-ss-split", "state-store.evm-split", "infix deletion; they cannot both be intended"},
	{"state-commit.sc-async-commit-buffer", "state-commit.async-commit-buffer", "the inert tag names a dead field nothing reads"},
}

// TestGate6EveryDerivedKeyEqualsTheSpellingV1Resolves is the derivation-agreement gate.
//
// Held per key rather than per value. A key whose derived spelling differs from what V1
// resolves is a break whether or not any test drives it, which is the failure a value-only
// differential cannot see.
func TestGate6EveryDerivedKeyEqualsTheSpellingV1Resolves(t *testing.T) {
	for _, k := range v1Spellings {
		t.Run(k.operatorWrites, func(t *testing.T) {
			t.Fatalf("unimplemented: the registry cannot yet derive a key to compare. When it can, "+
				"the derived spelling must be %q and not %q (%s). A difference here renames a key "+
				"operators already have in their files",
				k.operatorWrites, k.inertTagWould, k.why)
		})
	}
}

// TestGate7ARenameRequiresAnExplicitRatifiedEntry is the escape hatch, and the reason gate 6
// is a gate rather than a prohibition.
//
// Keeping every name is the decision today, not a law. If a rename is ever wanted, the
// mechanism is that the divergence is ratified explicitly and CI fails without it, so every
// difference between the two families is a decision rather than drift. This gate holds that
// the ratification list exists and that it is empty, so adding an entry is a deliberate act
// visible in a diff.
func TestGate7ARenameRequiresAnExplicitRatifiedEntry(t *testing.T) {
	t.Fatal("unimplemented: no ratification list exists yet. It must start empty, since the " +
		"decision is to keep every existing name, and an entry in it is what permits gate 6 to " +
		"see a difference without failing")
}

// TestGate8TheDeadFieldIsNotReachableUnderTheLiveKey is the one trap the design calls settled,
// and it is tracked as PLT-945.
//
// state-commit.sc-async-commit-buffer resolves today to StateCommit.MemIAVLConfig
// .AsyncCommitBuffer, the live field. StateCommitConfig.AsyncCommitBuffer carries the inert
// tag async-commit-buffer and nothing reads it. A tag-driven binder that inherited that tag
// would bind an operator's value to the dead field while the spelling they actually write
// reached no field at all.
//
// The trap is invisible today precisely because nothing in the tree reads by tag, and it goes
// live the moment the registry does. That is the clearest single argument for new structs over
// corrected ones.
func TestGate8TheDeadFieldIsNotReachableUnderTheLiveKey(t *testing.T) {
	t.Fatal("unimplemented: needs the registry, then assert sc-async-commit-buffer binds the " +
		"live MemIAVLConfig field and that no derived key reaches StateCommitConfig" +
		".AsyncCommitBuffer, which nothing reads")
}

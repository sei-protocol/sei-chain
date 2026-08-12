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

//go:build configspec

package experimental_test

import "testing"

// PR3 of the ConfigManager stack: experimental configuration semantics.
//
// Scope, from the design's Experimental Values section. [experimental] is the feature-flag
// framework: a paved road for a team to ship a flagged feature's configuration, reshape or
// remove it in any release with no schema bump and no migration, and promote it to
// first-class once it is stable. Keys are declared in code, in a registry outside the schema
// fingerprint, so the binary type-checks what it recognizes and promotion is a mechanical
// move between registries.
//
// The one thing to get right, and the reason this PR is first rather than the registry: this
// ships in BOTH managers, not behind SEI_CONFIG_MANAGER=v2. The design calls it the minimal
// unblock for values that change between binaries, the cosmos_only class, and it does not
// wait for v2 adoption. A gate below holds that.
//
// Out of scope, and each has a later home:
//
//	the stable registry and mode-based defaults      PR4
//	sei.toml, generate, doctor, upgrade, diff        PR5
//	the schema fingerprint itself                    PR4, this only asserts exclusion from it
//	migrations                                       PR5
//
// BLOCKED, and it blocks one-way door 1 in the design's appendix B. Experimental-key
// promotion has no agreed review gate, and it is undecided whether promotion requires a
// deprecation window for the experimental spelling. Everything below is implementable
// without that answer, because promotion is a later PR's mechanics. What must not happen is
// an implementer inventing a promotion path here and making it the de facto contract.
//
// Package path config/experimental is provisional. It has to be importable from giga and the
// other feature packages, so it cannot live under cmd. The design's open question on key
// metadata placement may move it.

// TestGate1ADeclaredKeyReadsTypedWithItsDefault is the whole cost of shipping an
// experimental value, per the design: declaring it.
//
// An unset key resolves to the declared default rather than the type's zero, since an
// operator who wrote nothing gets the value the team shipped.
func TestGate1ADeclaredKeyReadsTypedWithItsDefault(t *testing.T) {
	t.Fatal("unimplemented: experimental.Int and its typed Get are not built")
}

// TestGate2AnUnrecognizedKeyWarnsAndIsLeftInPlace is the load-bearing gate for rollback.
//
// The design's trade is warn, never halt: the key is reported and left in place so a
// rollback does not lose it. A node that halted would make a config written for release N+1
// unbootable on N, which is the opposite of what a feature flag is for. Held in both
// directions: the warning fires, and the key survives a read-write cycle.
func TestGate2AnUnrecognizedKeyWarnsAndIsLeftInPlace(t *testing.T) {
	t.Fatal("unimplemented: the experimental registry does not exist, so nothing can be unrecognized yet")
}

// TestGate3ARecognizedKeyTypeChecks holds the half of the trade that is not freedom.
//
// Exemption from versioning ceremony is not exemption from definition. A key the binary
// recognizes has a declared type, and a value that does not convert is reported against
// that declaration rather than silently becoming a zero, which is the legacy path's failure
// mode across roughly two thirds of its casts.
func TestGate3ARecognizedKeyTypeChecks(t *testing.T) {
	t.Fatal("unimplemented: no declared type to check a value against")
}

// TestGate4ExperimentalKeysAreOutsideTheSchemaFingerprint is why an experimental key can
// change shape in a patch release.
//
// Registering, renaming or removing an experimental key must not move the fingerprint,
// because a moved fingerprint is what forces a schema bump and a migration. This gate
// asserts the exclusion; PR4 owns the fingerprint itself.
func TestGate4ExperimentalKeysAreOutsideTheSchemaFingerprint(t *testing.T) {
	t.Fatal("unimplemented: the fingerprint lands in PR4; this gate asserts exclusion from it")
}

// TestGate5ExperimentalSemanticsRunUnderBothManagers is the design constraint that puts this
// PR first.
//
// Experimental keys are the minimal unblock for values that change between binaries, so they
// cannot wait for v2 adoption. Driven with SEI_CONFIG_MANAGER unset and set to v2, and the
// same declared key has to resolve the same way through both. A gate that only exercised one
// manager would let this ship gated by accident.
func TestGate5ExperimentalSemanticsRunUnderBothManagers(t *testing.T) {
	t.Fatal("unimplemented: needs the registry, and a driven boot under each manager")
}

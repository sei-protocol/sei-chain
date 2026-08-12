package experimental_test

import (
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/sei-protocol/sei-chain/config/experimental"
)

// PR3 of the ConfigManager stack: experimental configuration semantics.
//
// [experimental] is the feature-flag framework. A team ships a flagged feature's
// configuration, reshapes or removes it in any release with no schema bump and no migration,
// and promotes it to first-class once it is stable. Keys are declared in code, in a registry
// outside the schema fingerprint, so the binary type-checks what it recognizes and promotion
// is a mechanical move between registries.
//
// Gate 5, that these semantics run under both configuration managers, needs the command tree
// and lives in cmd/seid/cmd/configmanager/experimental_spec_test.go.
//
// BLOCKED, and it blocks one-way door 1 in the design's appendix B. Experimental-key
// promotion has no agreed review gate, and whether promotion requires a deprecation window
// for the experimental spelling is undecided. Nothing here depends on that answer, because
// promotion is a later PR's mechanics. What must not happen is an implementer inventing a
// promotion path and making it the de facto contract.

// Keys declared for these gates. Registration happens at package scope, which is the shape a
// real caller uses, and it also means a duplicate declaration would panic during test setup
// rather than at some later call.
var (
	workers = experimental.Int("configspec.workers", 8, experimental.Owner("configtest"))
	label   = experimental.String("configspec.label", "unset", experimental.Owner("configtest"))
	toggle  = experimental.Bool("configspec.toggle", true, experimental.Owner("configtest"))
	unowned = experimental.Int("configspec.unowned", 1)
)

// written returns a viper carrying keys under the experimental section, as an operator's file
// would after the handler merged it.
func written(kv map[string]any) *viper.Viper {
	v := viper.New()
	for k, val := range kv {
		v.Set(experimental.Section+"."+k, val)
	}
	return v
}

// TestGate1ADeclaredKeyReadsTypedWithItsDefault is the whole cost of shipping an experimental
// value, per the design: declaring it.
//
// An unset key resolves to the declared default rather than the type's zero, so an operator
// who wrote nothing gets the value the team shipped. Asserted on all three types, and on
// defaults that are not their type's zero, since a default of 0 or "" or false would pass
// against an implementation that ignored the declaration entirely.
func TestGate1ADeclaredKeyReadsTypedWithItsDefault(t *testing.T) {
	empty := viper.New()
	if got := workers.Get(empty); got != 8 {
		t.Errorf("an absent int key read %d, want its declared default 8. An operator who wrote "+
			"nothing has to get the value the team shipped", got)
	}
	if got := label.Get(empty); got != "unset" {
		t.Errorf("an absent string key read %q, want its declared default", got)
	}
	if got := toggle.Get(empty); !got {
		t.Error("an absent bool key read false, want its declared default true")
	}

	// And a written value wins, or the default above would be the only answer this can give.
	v := written(map[string]any{"configspec.workers": "16", "configspec.label": "prod", "configspec.toggle": "false"})
	if got := workers.Get(v); got != 16 {
		t.Errorf("a written int key read %d, want 16. TOML carries this as a string, which is the "+
			"shape the design's own example uses, so the declared type has to absorb it", got)
	}
	if got := label.Get(v); got != "prod" {
		t.Errorf("a written string key read %q, want prod", got)
	}
	if got := toggle.Get(v); got {
		t.Error("a written bool key read true, want the written false")
	}
}

// TestGate2AnUnrecognizedKeyIsReportedAndLeftInPlace is the load-bearing gate for rollback.
//
// The design's trade is warn, never halt: the key is reported and left in place so a rollback
// does not lose it. A node that halted would make a config written for release N+1 unbootable
// on N, which is the opposite of what a feature flag is for.
//
// Held in both directions. The finding fires, and the key still reads back off the source
// afterwards, which is what "left in place" has to mean for a rollback to recover the value.
func TestGate2AnUnrecognizedKeyIsReportedAndLeftInPlace(t *testing.T) {
	const path = experimental.Section + ".configspec.from_a_later_release"
	v := written(map[string]any{"configspec.from_a_later_release": "42", "configspec.workers": "16"})

	findings := experimental.Check(v)
	unknown := experimental.Unrecognized(findings)
	if len(unknown) != 1 || unknown[0] != path {
		t.Fatalf("Check reported %v as unrecognized, want exactly [%s]. A key no binary in this "+
			"release declares is the case this framework exists for", unknown, path)
	}
	// Not an error. Nothing about an unrecognized key may make a caller halt.
	for _, f := range findings {
		if f.Unrecognized && f.Err != nil {
			t.Errorf("%s carries an error as well as being unrecognized, so a caller that halts on "+
				"errors would refuse a boot over a key written for a later release", f.Path)
		}
	}
	// Left in place: the value is still there to be read, which is what a rollback needs.
	if got := v.Get(path); got != "42" {
		t.Errorf("the unrecognized key reads back as %#v after the check, want 42. Reporting must "+
			"not consume or clear it, or a rollback to the release that declares it loses the value", got)
	}
	// And a recognized sibling in the same source is unaffected.
	if got := workers.Get(v); got != 16 {
		t.Errorf("a recognized key beside an unrecognized one read %d, want 16", got)
	}
}

// TestGate3ARecognizedKeyTypeChecks holds the half of the trade that is not freedom.
//
// Exemption from versioning ceremony is not exemption from definition. A value that does not
// convert is reported against its declaration rather than silently becoming a zero, which is
// the legacy path's failure mode across roughly two thirds of its casts.
func TestGate3ARecognizedKeyTypeChecks(t *testing.T) {
	v := written(map[string]any{"configspec.workers": "not-a-number"})

	invalid := experimental.Invalid(experimental.Check(v))
	if len(invalid) != 1 {
		t.Fatalf("Check reported %d invalid values for a declared int set to a non-number, want 1. "+
			"A declared type that never rejects anything is not a type", len(invalid))
	}
	f := invalid[0]
	if !strings.Contains(f.Path, "configspec.workers") {
		t.Errorf("the finding names %q rather than the key that failed", f.Path)
	}
	if f.Owner != "configtest" {
		t.Errorf("the finding reports owner %q, want configtest. A bad value needs someone to "+
			"ask, and the declaration is the only place that records who", f.Owner)
	}
	if f.Unrecognized {
		t.Error("a declared key was reported as unrecognized, which would send a reader looking " +
			"for a missing declaration rather than at their own value")
	}

	// The read falls back to the default, and that is only safe because the pass above reported
	// it. Asserted so the fallback is a recorded behaviour rather than an accident.
	if got := workers.Get(v); got != 8 {
		t.Errorf("a key with an unconvertible value read %d, want the declared default 8", got)
	}

	// A convertible value produces no finding, or the gate above would hold for a checker that
	// rejected everything.
	if got := experimental.Invalid(experimental.Check(written(map[string]any{"configspec.workers": "16"}))); len(got) != 0 {
		t.Errorf("a valid value produced %d findings, want none", len(got))
	}
}

// TestGate4ExperimentalKeysCannotBeReachedByAStableRegistryWalk is why an experimental key can
// change shape in a patch release.
//
// The fingerprint itself lands in PR4, and this asserts the structural property that keeps
// these keys out of it: every declared path is namespaced under the experimental section, and
// no stable section struct is named for that section. A registry that walks section structs
// therefore cannot produce one of these paths, so registering, renaming or removing an
// experimental key cannot move a hash taken over that walk.
//
// PR4's gate 4 asserts the other half, that a stable change does move the fingerprint. Neither
// half means anything alone.
func TestGate4ExperimentalKeysCannotBeReachedByAStableRegistryWalk(t *testing.T) {
	keys := experimental.Keys()
	if len(keys) == 0 {
		t.Fatal("no keys are declared, so this gate would hold for a registry that dropped every " +
			"declaration it was given")
	}

	prefix := experimental.Section + "."
	for _, key := range keys {
		d, ok := experimental.Lookup(key)
		if !ok {
			t.Errorf("%s is enumerated but does not resolve, so the registry disagrees with itself", key)
			continue
		}
		if !strings.HasPrefix(d.Path(), prefix) {
			t.Errorf("%s resolves at %q, outside the %q section. A stable registry walking section "+
				"structs could then produce this path, which would put the key in the fingerprint and "+
				"cost it a schema bump to rename", key, d.Path(), experimental.Section)
		}
		// The key's own identity must omit the section, or promotion to the stable registry
		// would have to rename it.
		if strings.HasPrefix(key, prefix) {
			t.Errorf("%s carries the section in its own identity. Promotion moves the declaration "+
				"between registries and must not rename the key", key)
		}
	}

	// An undeclared key resolves to nothing, so Lookup is a real gate rather than a constructor.
	if _, ok := experimental.Lookup("configspec.never-declared"); ok {
		t.Error("Lookup answered for a key nothing declared, so an unrecognized key would never " +
			"be reported and gate 2 would be unreachable")
	}
}

// TestGate4bAnUnownedKeyStillRegistersAndReportsSo keeps the owner requirement honest.
//
// Owner is not enforced by the type, because an unowned key is still better registered than
// absent. It reports as unknown so doctor can surface it, rather than reading as owned by
// whoever looks last.
func TestGate4bAnUnownedKeyStillRegistersAndReportsSo(t *testing.T) {
	d, ok := experimental.Lookup(unowned.Key())
	if !ok {
		t.Fatal("a declaration without an owner did not register")
	}
	if d.Owner() != "unknown" {
		t.Errorf("an unowned key reports owner %q, want unknown. An empty owner would render as "+
			"a blank field in a report and read as an oversight in the reporter", d.Owner())
	}
}

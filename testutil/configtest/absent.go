package configtest

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
)

// CheckZeroWhenAbsentMatchesTheReader holds a section's declaration against the reader it describes.
//
// A migration writes, for a key the existing configuration does not carry, whatever the node already
// resolves for it. That is the reader's default when the read is checked for presence, and the zero when it
// is not, and the difference is what the section declares. This proves the declaration is exactly right:
// no key claimed that the reader checks, and none missed that it does not.
//
// It asks the reader rather than reading its source. For each key it writes the baseline and then the zero,
// and the one that leaves the reader's output unchanged is the value an absent key already resolves to.
// Nothing is authored: both candidates come from what the registry already holds, so a section with sixty
// keys costs no more to cover than one with two.
//
// The comparison is over the reader's whole output, not a named field. There is nothing to identify, so
// there is no field path to resolve and no equality rule to get wrong.
func CheckZeroWhenAbsentMatchesTheReader(t testing.TB, section string, read func(AppOpts) (any, error)) {
	t.Helper()

	registered, ok := registry.Lookup(section)
	if !ok {
		t.Fatalf("%s is not registered, so there is no declaration to check", section)
	}
	absent, err := read(AppOpts{})
	if err != nil {
		t.Fatalf("%s: the reader refused an empty configuration, which is what it is handed for a node "+
			"whose file carries none of its keys: %v", section, err)
	}

	// A key's answer has to hold for every node mode, because a baseline may vary by one and the
	// candidates come from it. A section that names the value directly is checked against every mode too.
	var claimedButChecked, missedButNot, neither []string
	for _, key := range registered.Keys {
		if named, ok := registry.ValueWhenAbsent(key); ok {
			if !noOpInEveryMode(t, read, section, key, absent, func(any) any { return named }) {
				neither = append(neither, key)
			}
			continue
		}

		// The reader keeps its default for a key it checks, so writing the baseline changes nothing.
		if noOpInEveryMode(t, read, section, key, absent, func(baseline any) any { return baseline }) {
			if registry.ZeroWhenAbsent(key) {
				claimedButChecked = append(claimedButChecked, key)
			}
			continue
		}
		// It does not, so an absent key resolves to the zero. Confirm that rather than assume it.
		if noOpInEveryMode(t, read, section, key, absent, zeroLike) {
			if !registry.ZeroWhenAbsent(key) {
				missedButNot = append(missedButNot, key)
			}
			continue
		}
		neither = append(neither, key)
	}

	report(t, section, claimedButChecked,
		"declared as resolving to zero when absent, and their reader keeps its default for them. A "+
			"migration would write the zero where the node runs the default")
	report(t, section, missedButNot,
		"not declared, and their reader does not keep its default for them. A migration would write the "+
			"default where the node runs the zero, which is how enabling the state store on a node that "+
			"has none stops it starting")
	report(t, section, neither,
		"not describable by one declaration: their reader neither keeps its default nor resolves to the "+
			"zero, or it does one for some node modes and the other for the rest. A migration cannot "+
			"carry them by rule, so each needs deciding rather than guessing")
}

// noOpInEveryMode reports whether the candidate leaves the reader unchanged for every node mode.
//
// Every mode, because a mode-varying baseline makes the candidate vary with it, and a value that preserves
// behaviour for one kind of node and not another is not an answer a migration can use.
func noOpInEveryMode(t testing.TB, read func(AppOpts) (any, error), section, key string, absent any,
	candidate func(baseline any) any) bool {
	t.Helper()
	for _, mode := range registry.Modes() {
		resolved, err := registry.Resolve(mode)
		if err != nil {
			t.Fatalf("%s: cannot resolve the baselines for %q: %v", section, mode, err)
		}
		baseline, present := resolved.Keys[key]
		if !present || baseline.Value == nil {
			t.Errorf("%s: %q resolves to nothing for %q, so no candidate can be built", section, key, mode)
			return false
		}
		if !sameReaderOutput(t, read, key, candidate(baseline.Value), absent) {
			return false
		}
	}
	return true
}

// zeroLike returns the zero value of whatever type the baseline carries.
func zeroLike(baseline any) any {
	return reflect.Zero(reflect.TypeOf(baseline)).Interface()
}

// sameReaderOutput reports whether writing one value leaves the reader's whole output unchanged.
func sameReaderOutput(t testing.TB, read func(AppOpts) (any, error), key string, value, absent any) bool {
	t.Helper()
	got, err := read(AppOpts{key: value})
	if err != nil {
		// A refusal is not a match, and it is not this check's business to judge it: the value came from
		// the section's own declaration, so a reader refusing it is reported through the caller below.
		return false
	}
	return reflect.DeepEqual(got, absent)
}

// report names one class of disagreement, or says nothing when there is none.
func report(t testing.TB, section string, keys []string, why string) {
	t.Helper()
	if len(keys) == 0 {
		return
	}
	sort.Strings(keys)
	t.Errorf("%s: %d key(s) are %s:\n  %s", section, len(keys), why, strings.Join(keys, "\n  "))
}

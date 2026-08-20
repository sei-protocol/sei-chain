// Package appopts installs resolved configuration into the source a booting node reads.
//
// It layers rather than replaces. A key a section declares is written at override precedence, so the
// registry's answer wins over the file, the environment and any flag, and no resolution runs when the
// node reads it. Every other key is left exactly as it was, answered by the machinery that answers it
// today.
//
// Layering is what makes the migration possible at all. A key's value can only be resolved ahead of the
// read if its name is known, and a name is known only once a section declares it: an environment cannot
// be enumerated for a prefix, so a value delivered that way under a key nothing declares is readable and
// unlistable at the same time. Building a fresh source from an enumeration would drop exactly those
// values and replace an operator's setting with a code default. Leaving them alone cannot, because the
// code that answers them is unchanged.
//
// The two halves partition the problem rather than overlapping. Declared keys are resolvable because
// their names are known; undeclared keys are delegated because theirs are not. The delegated half shrinks
// as sections are declared, and when the last one lands the source carries a resolved value for every
// key.
package appopts

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/viper"

	"github.com/sei-protocol/sei-chain/config/registry"
)

// Report says where each key in the resulting configuration comes from.
type Report struct {
	// Installed are the declared keys written at override precedence, sorted. The registry answers for
	// these.
	Installed []string
	// Passthrough are keys the source enumerates that no section declares, sorted. These still read as
	// they always have, and they are the migration that remains.
	Passthrough []string
	// Added are declared keys the source did not enumerate, sorted. A node reads these for the first
	// time from the registry.
	Added []string
}

// Install writes every resolved value into target at override precedence and reports what it found.
func Install(target *viper.Viper, resolved registry.Resolved) (Report, error) {
	if target == nil {
		return Report{}, fmt.Errorf("no configuration source to install into")
	}
	// Enumerated once and handed to both the refusal and the report, so the two cannot disagree about
	// what the source carries.
	enumerated := enumerate(target)
	if err := refuseNotLowerCase(resolved); err != nil {
		return Report{}, err
	}
	if err := refuseUnwritable(target, resolved, enumerated); err != nil {
		return Report{}, err
	}

	// Described before anything is written, because installing makes every declared key enumerable and
	// Added is the set that was not. Taken afterwards it is always empty.
	report := describe(resolved, enumerated)
	for key, value := range resolved.Values {
		target.Set(key, value)
	}
	return report, nil
}

// enumerate returns the keys the source lists.
//
// A source lower-cases a key on the way in, so what it enumerates is already lower case and a declared
// key is too. The two compare directly.
func enumerate(target *viper.Viper) map[string]bool {
	keys := target.AllKeys()
	out := make(map[string]bool, len(keys))
	for _, key := range keys {
		out[key] = true
	}
	return out
}

// refuseNotLowerCase rejects a declared key the source would store under a different name.
//
// A source lower-cases a key on the way in, so a key that is not already lower case is written and read
// under a name that is not the one declared. Two consequences, and the first is the reason this runs
// before anything else here: a comparison against another key or against what the source enumerates
// misses a match that the source itself would make, so the refusal below cannot see the collision and
// both values land on one path. The second is that the report counts one key twice, once as installed
// under the declared spelling and once as untouched under the stored one.
//
// Refused rather than folded, because folding accepts a key the registry refuses at both of its own
// doors and then quietly writes a different one.
func refuseNotLowerCase(resolved registry.Resolved) error {
	var wrong []string
	for key := range resolved.Values {
		if key != strings.ToLower(key) {
			wrong = append(wrong, key)
		}
	}
	if len(wrong) == 0 {
		return nil
	}
	sort.Strings(wrong)
	return fmt.Errorf("these declared keys are not lower case, and a configuration source stores a key "+
		"lower-cased, so each would be written and read under a name nothing declares: %s",
		strings.Join(wrong, ", "))
}

// dottedPrefixes returns every path a key nests under, outermost first.
//
// Derived from the key rather than found among its neighbours. A sorted scan looking for the keys under
// one of them has to assume they follow it directly, and they do not: a hyphen sorts before a dot, so a
// hyphenated sibling sits between a key and its children. This key space separates words with hyphens,
// and grpc-web.address already sits between grpc and grpc.enable.
func dottedPrefixes(key string) []string {
	var out []string
	for i := range key {
		if key[i] == '.' {
			out = append(out, key[:i])
		}
	}
	return out
}

// refuseUnwritable rejects a key that cannot be written without making another key unreadable.
//
// The source holds one value per path, so two keys cannot both occupy one. Writing a.b turns a into a
// table and destroys whatever a held; writing a afterwards destroys the table. Neither order keeps both,
// and which one survives is decided by map iteration order.
//
// The cost falls on whichever key is not declared: the shorter path answers with a table instead of what
// an operator wrote, or the longer one answers with nothing, and a reader of either gets a zero rather
// than an error. That is the value this package exists to leave alone, so this refuses rather than
// choosing. Skipping the declared key instead would leave a section half migrated and a key whose answer
// depends on which of two readers a caller asked.
func refuseUnwritable(target *viper.Viper, resolved registry.Resolved, enumerated map[string]bool) error {
	var lost []string
	// Each pair says which of the two nobody declared, because that decides the remedy: an operator can
	// rename their own key, and only a release can change what a section declares.
	note := func(outer, inner string, bothDeclared bool) {
		if bothDeclared {
			lost = append(lost, fmt.Sprintf("%q and %q, both declared", outer, inner))
			return
		}
		lost = append(lost, fmt.Sprintf("%q declared and %q not", outer, inner))
	}

	// A declared key nesting under a path something already occupies, whether that is another declared
	// key or a value the source answers with.
	for key := range resolved.Values {
		for _, under := range dottedPrefixes(key) {
			if _, declared := resolved.Values[under]; declared {
				note(under, key, true)
			} else if holdsAValue(target, under) {
				note(key, under, false)
			}
		}
	}
	// A key the source enumerates nesting under a declared key. The declared value shadows it, and a read
	// of it stops before the source consults the layer it came from.
	for key := range enumerated {
		if _, declared := resolved.Values[key]; declared {
			continue
		}
		for _, under := range dottedPrefixes(key) {
			if _, declared := resolved.Values[under]; declared {
				note(under, key, false)
			}
		}
	}
	if len(lost) == 0 {
		return nil
	}
	sort.Strings(lost)
	return fmt.Errorf("these key pairs cannot both be installed, because one names a path the other "+
		"nests under and the source holds one value per path: %s. An undeclared key is an operator's to "+
		"rename; a declared one changes only in a release", strings.Join(lost, ", "))
}

// holdsAValue reports whether target answers for key with something other than a table.
//
// Asked of the source rather than of what it enumerates, because a value delivered through the
// environment is answerable and unlistable. IsSet rather than a nil check on Get, because a bound flag
// nobody set still answers with its default and refusing over one would be a false alarm.
func holdsAValue(target *viper.Viper, key string) bool {
	if !target.IsSet(key) {
		return false
	}
	switch target.Get(key).(type) {
	case map[string]any, map[any]any:
		return false
	default:
		return true
	}
}

// describe sorts the keys of target and resolved into the three populations a Report names.
func describe(resolved registry.Resolved, enumerated map[string]bool) Report {
	var report Report
	for key := range resolved.Values {
		report.Installed = append(report.Installed, key)
		if !enumerated[key] {
			report.Added = append(report.Added, key)
		}
	}
	for key := range enumerated {
		if _, declared := resolved.Values[key]; !declared {
			report.Passthrough = append(report.Passthrough, key)
		}
	}

	sort.Strings(report.Installed)
	sort.Strings(report.Passthrough)
	sort.Strings(report.Added)
	return report
}

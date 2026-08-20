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
	if err := refuseColliding(resolved); err != nil {
		return Report{}, err
	}
	if err := refuseShadowing(target, resolved, enumerated); err != nil {
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

// refuseColliding rejects two declared keys where one names a path the other nests under.
//
// A source holds a value at a path, so writing a.b turns a into a table and destroys whatever a held,
// and writing a afterwards destroys the table. Neither order keeps both, and which one survives is
// decided by map iteration order.
func refuseColliding(resolved registry.Resolved) error {
	var collisions []string
	for key := range resolved.Values {
		for _, under := range dottedPrefixes(key) {
			if _, declared := resolved.Values[under]; declared {
				collisions = append(collisions, under+" and "+key)
			}
		}
	}
	if len(collisions) == 0 {
		return nil
	}
	sort.Strings(collisions)
	return fmt.Errorf("these declared key pairs cannot both be installed, because one names a path the "+
		"other nests under and whichever is written second destroys the first: %s",
		strings.Join(collisions, ", "))
}

// refuseShadowing rejects a declared key that cannot be written without making another key unreadable.
//
// The source holds one value per path, so a declared key and a key nobody declared cannot both occupy
// one. Installing anyway costs the undeclared value silently: the shorter path answers with a table
// instead of what an operator wrote, or the longer one answers with nothing, and a reader of either gets
// a zero rather than an error. That is the value this package exists to leave alone.
//
// Refused rather than skipped. Skipping the declared key leaves a section half migrated and a key whose
// answer depends on which of two readers a caller asked, where refusing names the two keys and stops.
func refuseShadowing(target *viper.Viper, resolved registry.Resolved, enumerated map[string]bool) error {
	var lost []string
	note := func(declared, undeclared string) {
		lost = append(lost, fmt.Sprintf("%q is declared and %q is not", declared, undeclared))
	}

	// A declared key under a path the source already answers with a value of its own. Writing it puts a
	// table where that value was.
	for key := range resolved.Values {
		for _, under := range dottedPrefixes(key) {
			if _, declared := resolved.Values[under]; declared {
				continue // refuseColliding names this pair
			}
			if holdsAValue(target, under) {
				note(key, under)
			}
		}
	}
	// A key the source enumerates that nests under a declared key. The declared value shadows it, and a
	// read of it stops before the source consults the layer it came from.
	for key := range enumerated {
		if _, declared := resolved.Values[key]; declared {
			continue
		}
		for _, under := range dottedPrefixes(key) {
			if _, declared := resolved.Values[under]; declared {
				note(under, key)
			}
		}
	}
	if len(lost) == 0 {
		return nil
	}
	sort.Strings(lost)
	return fmt.Errorf("this configuration cannot be installed without losing a value nobody declared, "+
		"and no order of writes keeps both: %s. A source holds one value per path, so the two keys "+
		"cannot both occupy theirs. Either rename the undeclared key, or run a release whose sections "+
		"do not declare over it", strings.Join(lost, "; "))
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

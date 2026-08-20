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
	if err := refuseColliding(resolved); err != nil {
		return Report{}, err
	}

	// Described before anything is written, because installing makes every declared key enumerable and
	// Added is the set that was not. Taken afterwards it is always empty.
	report := describe(target, resolved)
	for key, value := range resolved.Values {
		target.Set(key, value)
	}
	return report, nil
}

// refuseColliding rejects two declared keys where one names a prefix of the other.
//
// A source holds a key's value at a path, so writing a.b turns a into a table and destroys whatever a
// held, and writing a afterwards destroys the table. Either order loses a value, so there is no order
// this can be installed in. Refused here rather than at registration because it is a property of how
// this source stores a key, not of whether the key space is coherent.
func refuseColliding(resolved registry.Resolved) error {
	keys := make([]string, 0, len(resolved.Values))
	for key := range resolved.Values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var collisions []string
	for i, key := range keys {
		// Sorted, so any key with this one as a prefix follows it directly and the scan stops at the
		// first that does not.
		for j := i + 1; j < len(keys); j++ {
			if !strings.HasPrefix(keys[j], key+".") {
				break
			}
			collisions = append(collisions, key+" and "+keys[j])
		}
	}
	if len(collisions) > 0 {
		return fmt.Errorf("these declared key pairs cannot both be installed, because one is a prefix "+
			"of the other and whichever is written second destroys the first: %s",
			strings.Join(collisions, ", "))
	}
	return nil
}

// describe sorts the keys of target and resolved into the three populations a Report names.
func describe(target *viper.Viper, resolved registry.Resolved) Report {
	// A source lower-cases a key on the way in, so what it enumerates is already lower case and a declared
	// key is too. The two compare directly.
	keys := target.AllKeys()
	enumerated := make(map[string]bool, len(keys))
	for _, key := range keys {
		enumerated[key] = true
	}

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

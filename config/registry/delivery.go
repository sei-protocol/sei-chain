package registry

import (
	"fmt"
	"sort"
)

// decodedNotLookedUp holds the sections whose values reach their reader by a decode, and why.
var decodedNotLookedUp = map[string]string{}

// DeclareDecodedNotLookedUp records that a section's reader decodes it into a struct before boot rather
// than looking keys up by name, so installing into the lookup source would not reach it. why must name
// that struct; an empty why is a Defect. A name matching no registered section is reported by Defects.
func DeclareDecodedNotLookedUp(section, why string) {
	mu.Lock()
	defer mu.Unlock()
	if why == "" {
		defects = append(defects, Defect{Section: section, Err: fmt.Errorf(
			"declared as decoded rather than looked up with no reason; the reason names the struct its " +
				"values are decoded into, which is what a reader checks the claim against")})
		return
	}
	decodedNotLookedUp[section] = why
}

// DecodedSections returns the sections declared decoded, with the reason each gave.
func DecodedSections() map[string]string {
	mu.RLock()
	defer mu.RUnlock()
	out := make(map[string]string, len(decodedNotLookedUp))
	for name, why := range decodedNotLookedUp {
		out[name] = why
	}
	return out
}

// ResolvedAndOwnedByDecodedSections returns, from one registry snapshot, the resolved values of each
// decoded section keyed by section name, and every key those sections own, sorted.
func ResolvedAndOwnedByDecodedSections(resolved Resolved) (map[string]map[string]any, []string) {
	registered, _, owning := snapshot()

	out := map[string]map[string]any{}
	var everyKey []string
	for _, section := range registered {
		if _, owned := owning[section.Name]; !owned {
			continue
		}
		everyKey = append(everyKey, section.Keys...)
		for _, key := range section.Keys {
			value, answered := resolved.Values[key]
			if !answered {
				continue
			}
			if out[section.Name] == nil {
				out[section.Name] = map[string]any{}
			}
			out[section.Name][key] = value
		}
	}
	sort.Strings(everyKey)
	return out, everyKey
}

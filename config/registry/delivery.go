package registry

import (
	"fmt"
	"sort"
)

// decodedNotLookedUp holds the sections whose values reach their reader by a decode, and why.
var decodedNotLookedUp = map[string]string{}

// DeclareDecodedNotLookedUp records that a section's values reach their reader by being decoded into a
// struct, rather than by a lookup in the source a node reads.
//
// Almost every section is read the other way: a reader asks for a key by name, so putting the resolved
// value into that source is the whole delivery. The sections this names are read once, by decoding a file
// into a struct before any of that happens, and a value put into the source afterwards reaches nothing.
// They need delivering a second way.
//
// The reason is required and names the struct the values are decoded into, which is what a reader has to
// check the claim against. A section declared with no reason is recorded as a defect rather than accepted,
// because the claim is the whole basis for delivering its keys differently.
//
// The section name is not checked here. A section registers itself and declares its delivery from two
// calls, and nothing fixes the order between them, so a name absent now may be registered a moment later.
// Defects answers instead, deriving it from the registry at every read: a name here that no section carries
// is reported, which is what catches the misspelling this call is most likely to contain. Left unreported,
// the section named delivers nothing and the correctly spelled section's keys install into a source its
// reader never asks.
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

// DecodedSections returns the sections whose values reach their reader by a decode, with the reason each
// gave, so a caller can report what it is about to do and to what.
func DecodedSections() map[string]string {
	mu.RLock()
	defer mu.RUnlock()
	out := make(map[string]string, len(decodedNotLookedUp))
	for name, why := range decodedNotLookedUp {
		out[name] = why
	}
	return out
}

// ResolvedAndOwnedByDecodedSections answers both halves of the decoded-delivery question from one read.
//
// The resolved values each decoded section has to be handed, and every key those sections own. A caller
// needs both: the first to deliver or report, the second to know what to leave out of a delivery that
// cannot carry it.
//
// Every key the resolution answered, not only the ones a source wrote. A key sei.toml leaves out takes the
// value this binary declares, and that has to reach the struct like any other, because the struct is what
// the node reads and config.toml is not consulted for a declared key. That does replace what an operator's
// config.toml said for a key their sei.toml does not mention, and it is meant to: one file states what a
// node runs.
//
// Together rather than from two calls, because a section arriving between two reads is absent from one
// answer and present in the other, so its keys are dropped from the install by the second and not reported
// by the first. Silently undelivered and unreported is the outcome one read exists to prevent.
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

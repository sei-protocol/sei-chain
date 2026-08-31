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

// SuppliedByDecodedSection splits a resolution into the values each decoded section has to deliver
// itself, keyed by section name and then by dotted key.
//
// Split per section rather than pooled, because a decode is all or nothing for whatever it is handed. One
// value a decoder refuses would otherwise cost every key in the file rather than the keys of the one
// section it appeared in, and an operator who fixed one setting and mistyped another would boot with
// neither applied and no way to tell which.
//
// The defaults are deliberately left out, and this is the difference between delivering a value and
// overwriting one. A section read by a lookup can be delivered whole, because its reader has nowhere else
// to get a value from. A section read by a decode already holds what its own file said, put there before
// any of this ran. Delivering a default over that replaces the operator's file with one nobody chose, on
// every boot, for every key their file does not mention.
//
// So a key that took its default is skipped and a key any other layer answered is delivered. That includes
// an operator writing the default value explicitly, because what is recorded is which layer answered and
// not whether the answer differs from the default: writing false where the file says true has to arrive.
func SuppliedByDecodedSection(resolved Resolved) map[string]map[string]any {
	// One read for the sections and the declarations together. Asking for each on its own leaves a window a
	// registration fits through, and a section arriving in it is declared by one answer and absent from the
	// other, so its keys are silently left undelivered.
	registered, _, owning := snapshot()

	supplied := make(map[string]bool, len(resolved.Overrides))
	for _, key := range resolved.Overrides {
		supplied[key] = true
	}

	out := map[string]map[string]any{}
	for _, section := range registered {
		if _, owned := owning[section.Name]; !owned {
			continue
		}
		for _, key := range section.Keys {
			if !supplied[key] {
				continue
			}
			if out[section.Name] == nil {
				out[section.Name] = map[string]any{}
			}
			out[section.Name][key] = resolved.Values[key]
		}
	}
	return out
}

// KeysADecodeDelivers returns the keys of every section whose values reach their reader by a decode, sorted.
//
// One read for the sections and the declarations together, for the reason SuppliedByDecodedSection takes
// one: asked separately, a section arriving between the two reads is named by one answer and absent from
// the other, so its keys are attributed to the wrong delivery.
func KeysADecodeDelivers() []string {
	registered, _, decoded := snapshot()
	var out []string
	for _, section := range registered {
		if _, owned := decoded[section.Name]; !owned {
			continue
		}
		out = append(out, section.Keys...)
	}
	sort.Strings(out)
	return out
}

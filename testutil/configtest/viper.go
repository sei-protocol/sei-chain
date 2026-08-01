package configtest

import (
	"sort"
	"strings"

	"github.com/spf13/viper"
)

// DumpViper renders a viper instance as the resolved key/value map every
// appOpts.Get() call site consumes: one sorted line per key, each value rendered
// by Dump so its concrete type is visible.
//
// One limitation is inherent and worth stating rather than working around.
// AllKeys enumerates only keys viper knows about structurally — from the config
// files it read, explicit defaults, overrides, bound flags and explicit BindEnv
// calls. Keys reachable *solely* through AutomaticEnv are invisible to it,
// because AutomaticEnv resolves at Get time against whatever the environment
// happens to hold. That is not a gap in the dump so much as a property of the
// legacy path: an env-only key has no enumerable existence, which is exactly why
// `SEID_SECTION_KEY` overrides a key present in app.toml but is ignored for a key
// that is absent from it. Tests that need to pin an env-only read must assert
// Get(key) directly; DumpViper pins everything else.
func DumpViper(v *viper.Viper) string {
	if v == nil {
		return "<nil-viper>"
	}
	keys := v.AllKeys()
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, DumpAt(k, v.Get(k)))
	}
	return strings.Join(lines, "\n")
}

// Settings renders a viper instance as a flat map from dotted key to resolved
// value: one entry per AllKeys entry, each value as Get returns it.
//
// Use this, not AllSettings, to compare two vipers. AllSettings re-nests the flat
// key space by splitting each key on "." and merging the pieces into a tree, and
// when one key is a dotted prefix of another — "giga" alongside "giga.x" — whether
// the scalar or the sub-tree survives depends on map iteration order. Two
// AllSettings calls over one file then disagree, so an equality assertion between
// two of them can fail on identical input. Get is unaffected: it tries longest
// prefixes first, so both keys resolve. Settings therefore keys on AllKeys and
// reads each key through Get, which is stable by construction.
//
// A flat map rather than DumpViper's rendered string, for two reasons. Values keep
// their concrete Go type, so int64(8) and "8" do not compare equal. And a key is a
// map key rather than a line in a newline-joined document, so a key containing a
// newline — legal in TOML, and reachable from a fuzz target that appends arbitrary
// bytes — cannot make two different key sets render identically. DumpViper stays
// the right tool for a readable failure message; this is the right tool for the
// assertion itself.
func Settings(v *viper.Viper) map[string]any {
	if v == nil {
		return nil
	}
	keys := v.AllKeys()
	settings := make(map[string]any, len(keys))
	for _, k := range keys {
		settings[k] = v.Get(k)
	}
	return settings
}

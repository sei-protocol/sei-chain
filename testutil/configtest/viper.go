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

// DumpViperKeys renders only the named keys, in the order given, via
// viper.Get. Unlike DumpViper it reaches AutomaticEnv-only keys, because it asks
// for each key by name instead of enumerating. Use it to pin a specific manifest
// row; use DumpViper to pin the whole resolved surface.
func DumpViperKeys(v *viper.Viper, keys ...string) string {
	if v == nil {
		return "<nil-viper>"
	}
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, DumpAt(k, v.Get(k)))
	}
	return strings.Join(lines, "\n")
}

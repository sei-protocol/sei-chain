// Package appopts builds the configuration a booting node reads.
//
// What a node reads is a source carrying one resolved value per key, never an in-memory struct. A
// struct silently drops any key it does not model, and a round-trip test over one passes while being
// wrong, so the shape that reaches app.New has to be able to hold a key nothing has migrated yet.
//
// Every value goes in at override precedence, so no resolution runs when the node reads. Whatever
// decided a value decided it here, and Get returns exactly what was installed rather than consulting
// an environment variable or a file again behind the reader's back.
//
// That is also what makes the migration incremental. A key no section declares comes from the
// configuration the node reads today and is reported as remaining work; a key a section declares comes
// from the registry instead. The key space does not change as keys move across, so no read site
// changes with them.
package appopts

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/viper"

	"github.com/sei-protocol/sei-chain/config/registry"
)

// Source is a resolved configuration that can be enumerated.
//
// Declared here rather than imported, so this package depends on no particular type carrying a node's
// configuration. Anything that lists its keys and answers for one satisfies it.
type Source interface {
	AllKeys() []string
	Get(string) any
}

// Report says where each key in the built configuration came from.
type Report struct {
	// Passthrough are keys the existing configuration supplies that no section declares, sorted.
	// This is the migration that remains, and it shrinks one section at a time.
	Passthrough []string
	// Migrated are keys both supply, sorted. The registry's value is the one installed.
	Migrated []string
	// Added are keys a section declares that the existing configuration does not enumerate, sorted.
	Added []string
}

// Build installs every resolved key into a fresh source a node can read.
//
// The existing configuration supplies the key space and the registry overrides it, so a key that has
// moved into a section reads from the section while every key that has not keeps working unchanged.
// Nothing is dropped in either direction: a key either side supplies is present in the result, which
// is the property an in-memory struct cannot offer.
func Build(existing Source, resolved registry.Resolved) (*viper.Viper, Report, error) {
	if existing == nil {
		return nil, Report{}, fmt.Errorf("no existing configuration to build from")
	}
	values, report := merge(existing, resolved)
	if err := refuseShadowedKeys(values); err != nil {
		return nil, report, err
	}
	return install(values), report, nil
}

// merge decides each key's value and records where it came from.
func merge(existing Source, resolved registry.Resolved) (map[string]any, Report) {
	values := map[string]any{}
	var report Report

	for _, key := range existing.AllKeys() {
		lowered := strings.ToLower(key)
		if _, declared := resolved.Keys[lowered]; declared {
			report.Migrated = append(report.Migrated, lowered)
			continue // the registry supplies it below
		}
		values[lowered] = existing.Get(key)
		report.Passthrough = append(report.Passthrough, lowered)
	}

	migrated := make(map[string]bool, len(report.Migrated))
	for _, key := range report.Migrated {
		migrated[key] = true
	}
	for key, resolution := range resolved.Keys {
		values[key] = resolution.Value
		if !migrated[key] {
			report.Added = append(report.Added, key)
		}
	}

	sort.Strings(report.Passthrough)
	sort.Strings(report.Migrated)
	sort.Strings(report.Added)
	return values, report
}

// refuseShadowedKeys rejects a key space where one key is a prefix of another.
//
// Installing both loses one of them. A leaf set before its parent becomes a map and the leaf's value
// is gone; a parent set before its leaf leaves the leaf unreadable. Which one survives depends on the
// order they were installed, so neither answer is right and the loss is invisible to the reader.
//
// No such pair exists in the key space a node reads today, which a test pins. This exists for the day
// somebody adds one, so it fails here rather than on whichever node happened to read the lost key.
func refuseShadowedKeys(values map[string]any) error {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	// Sorted, so a key's prefixes are the entries immediately before it.
	var shadowed []string
	for i, key := range keys {
		for j := i + 1; j < len(keys); j++ {
			if !strings.HasPrefix(keys[j], key+".") {
				break
			}
			shadowed = append(shadowed, key+" and "+keys[j])
		}
	}
	if len(shadowed) > 0 {
		return fmt.Errorf("these key pairs cannot both be installed, because one is a prefix of the "+
			"other and whichever is written second destroys the first: %s", strings.Join(shadowed, ", "))
	}
	return nil
}

// install writes every value at override precedence into a fresh source.
//
// Override precedence is what makes the result final. Anything lower that viper would otherwise
// consult, an environment variable or a config file, cannot change an answer, so what a node reads is
// what was resolved rather than whatever the process environment happens to hold at read time.
func install(values map[string]any) *viper.Viper {
	v := viper.New()
	for key, value := range values {
		v.Set(key, value)
	}
	return v
}

// Total is how many keys the report accounts for.
func (r Report) Total() int {
	return len(r.Passthrough) + len(r.Migrated) + len(r.Added)
}

// Summary is one line an operator or a log can carry.
func (r Report) Summary() string {
	return fmt.Sprintf("%d key(s) read from sei.toml, %d still read from the legacy configuration, "+
		"%d declared but absent from it", len(r.Migrated), len(r.Passthrough), len(r.Added))
}

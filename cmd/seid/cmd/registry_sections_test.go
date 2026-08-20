package cmd

import (
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
)

// TestEverySectionThisBinaryDeclaresIsUsable is the check no single section can make.
//
// A section registers during its own package's initialisation, and a registration the registry cannot use
// is recorded rather than panicked, so a section that failed to register is absent rather than loud. Two
// of the refusals depend on what else has registered: two sections declaring one key, and two keys that
// collapse onto one environment variable. Neither is visible from inside either section, and the section
// that loses is dropped whole, with every key it declared.
//
// This package links every section a node's configuration reaches, so asking here is asking about the set
// a node actually gets. Nothing is enumerated, so a section added later is covered without this file
// changing.
func TestEverySectionThisBinaryDeclaresIsUsable(t *testing.T) {
	for _, defect := range registry.Defects() {
		t.Errorf("%s was refused, so none of its keys is declared: %v", defect.Section, defect.Err)
	}
	if len(registry.Sections()) == 0 {
		t.Fatal("no section registered, so the checks above hold for an empty set. This package links " +
			"the packages that register, and one of those imports has gone")
	}
}

// TestEveryDeclaredKeyResolvesForEveryMode covers the half of a registration a section's own test cannot.
//
// Registering validates the struct a section declares against. Whether its defaults can state one value
// for every key it declared is checked when something resolves them, and until now nothing did outside the
// registry's own tests. A default that arrives short is refused rather than filled, so the failure is an
// error here instead of a key resolving to a zero nobody chose.
func TestEveryDeclaredKeyResolvesForEveryMode(t *testing.T) {
	for _, mode := range registry.Modes() {
		resolved, err := registry.Resolve(mode, registry.Sources{})
		if err != nil {
			t.Errorf("mode %q does not resolve: %v", mode, err)
			continue
		}
		for _, section := range registry.Sections() {
			for _, key := range section.Keys {
				if _, ok := resolved.Values[key]; !ok {
					t.Errorf("mode %q: %s declares %s and it did not resolve", mode, section.Name, key)
				}
			}
		}
	}
}

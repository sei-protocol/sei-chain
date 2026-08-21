package blocktest

import (
	"reflect"
	"sort"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
)

// TestDeclaredKeysAreTheOnesItsReaderResolves holds the derived keys against the reader's own constants.
//
// The section registers the struct its reader fills, so a mapstructure tag is the only spelling of these
// keys. What remains is the constants ReadConfig passes to Get, which state the same keys again in the same
// file, and a rename that moves one and not the other compiles.
//
// The section name is passed to the registry rather than derived, which is what keeps this section reachable
// at all: the struct that carries it in the generated file is tagged with a different spelling, and a
// registry that took the section name from a tag would declare a section no operator writes.
func TestDeclaredKeysAreTheOnesItsReaderResolves(t *testing.T) {
	for _, defect := range registry.Defects() {
		if defect.Section == SectionName {
			t.Fatalf("%s was refused, so none of its keys is declared: %v", SectionName, defect.Err)
		}
	}
	section, ok := registry.Lookup(SectionName)
	if !ok {
		t.Fatalf("%s is not registered, so nothing resolves its keys", SectionName)
	}
	want := []string{flagEnabled, flagTestDataPath}
	sort.Strings(want)
	if !reflect.DeepEqual(section.Keys, want) {
		t.Errorf("%s declares\n  %v\nand its reader resolves\n  %v", SectionName, section.Keys, want)
	}
}

// TestEachKeyResolvesToTheValueItsFieldHolds covers the binding a key set cannot show.
//
// These two fields carry different types, so a tag on the wrong field changes what a key resolves to
// without changing the key set at all.
func TestEachKeyResolvesToTheValueItsFieldHolds(t *testing.T) {
	for _, mode := range registry.Modes() {
		resolved, err := registry.Resolve(mode, registry.Sources{})
		if err != nil {
			t.Fatalf("mode %q: %v", mode, err)
		}
		for key, want := range map[string]any{
			flagEnabled:      DefaultConfig.Enabled,
			flagTestDataPath: DefaultConfig.TestDataPath,
		} {
			if got := resolved.Values[key]; !reflect.DeepEqual(got, want) {
				t.Errorf("mode %q: %s resolves to %#v (%T), want %#v (%T)", mode, key, got, got, want, want)
			}
		}
		if resolved.Values[flagEnabled] == true {
			t.Errorf("mode %q resolves the block-test harness on, which replays recorded data instead of "+
				"following the chain", mode)
		}
	}
}

package querier

import (
	"reflect"
	"sort"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
)

// TestDeclaredKeysAreTheOnesItsReaderResolves holds the derived key against the reader's own constant.
//
// The section registers the struct its reader fills, so a mapstructure tag is the only spelling of its key.
// What remains is the constant ReadConfig passes to Get, which states the same key again a few lines away,
// and a rename that moves one and not the other compiles.
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
	want := []string{flagGasLimit}
	sort.Strings(want)
	if !reflect.DeepEqual(section.Keys, want) {
		t.Errorf("%s declares\n  %v\nand its reader resolves\n  %v", SectionName, section.Keys, want)
	}
}

// TestEachKeyResolvesToTheValueItsFieldHolds covers the binding a key set cannot show.
//
// Resolving carries the key a tag produced together with the value that tag's field held, so this notices a
// tag sitting on the wrong field. Comparing the defaults struct against itself does not: the key set stays
// the same and every field still holds the value it always did.
func TestEachKeyResolvesToTheValueItsFieldHolds(t *testing.T) {
	for _, mode := range registry.Modes() {
		resolved, err := registry.Resolve(mode, registry.Sources{})
		if err != nil {
			t.Fatalf("mode %q: %v", mode, err)
		}
		if got, want := resolved.Values[flagGasLimit], DefaultConfig.GasLimit; !reflect.DeepEqual(got, want) {
			t.Errorf("mode %q: %s resolves to %#v (%T), want %#v (%T)", mode, flagGasLimit, got, got, want, want)
		}
	}
}

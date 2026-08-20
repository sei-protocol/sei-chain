package config

import (
	"reflect"
	"sort"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
)

// TestTheDeclaredKeysAreTheKeysThisReaderResolves holds the declaration against the reader.
//
// The registry derives a key from the section name and a mapstructure tag; ReadConfig asks for a flag
// constant. Those are two spellings of one key, and a section is only useful if they are the same string.
// Checked against the constants rather than against a written-out list, so a rename of either moves both
// or fails here.
func TestTheDeclaredKeysAreTheKeysThisReaderResolves(t *testing.T) {
	for _, defect := range registry.Defects() {
		if defect.Section == SectionName {
			t.Fatalf("%s was refused: %v", SectionName, defect.Err)
		}
	}
	section, ok := registry.Lookup(SectionName)
	if !ok {
		t.Fatalf("%s is not registered, so nothing resolves its keys", SectionName)
	}

	want := []string{FlagEnabled, FlagOCCEnabled}
	sort.Strings(want)
	if got := section.Keys; !reflect.DeepEqual(got, want) {
		t.Errorf("declared keys are %v, want the keys the reader asks for, %v", got, want)
	}
}

// TestTheDefaultsAreWhatTheNodeAlreadyRuns keeps the section from restating the values by hand.
func TestTheDefaultsAreWhatTheNodeAlreadyRuns(t *testing.T) {
	for _, mode := range registry.Modes() {
		got, ok := defaults(mode).(Config)
		if !ok {
			t.Fatalf("mode %q: defaults returned %T, want Config", mode, defaults(mode))
		}
		if got != DefaultConfig {
			t.Errorf("mode %q resolves to %+v, want the package default %+v. A section states what the "+
				"binary already runs; a different value here is a behaviour change nobody asked for",
				mode, got, DefaultConfig)
		}
	}
}

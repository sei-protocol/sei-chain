package admin

import (
	"reflect"
	"sort"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
)

// TestTheDeclaredKeysAreTheKeysThisReaderResolves holds the declaration against the reader.
//
// The tags derive the keys and the constants below are what the reader passes to Get, so this compares
// two statements that are edited for different reasons rather than one written out twice.
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

	want := []string{flagAdminAddress, flagAdminEnabled}
	sort.Strings(want)
	if got := section.Keys; !reflect.DeepEqual(got, want) {
		t.Errorf("declared keys are %v, want %v", got, want)
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
			t.Errorf("mode %q resolves to %+v, want the package default %+v", mode, got, DefaultConfig)
		}
	}
}

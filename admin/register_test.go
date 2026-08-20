package admin

import (
	"reflect"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
)

// TestTheDeclaredKeysAreTheKeysThisReaderResolves holds the declaration against the reader.
//
// This package names its keys only in its mapstructure tags, so the check is that the registry derives
// exactly the two the reader resolves and no third.
func TestTheDeclaredKeysAreTheKeysThisReaderResolves(t *testing.T) {
	section, ok := registry.Lookup(SectionName)
	if !ok {
		t.Fatalf("%s is not registered, so nothing resolves its keys", SectionName)
	}

	want := []string{"admin_server.admin_address", "admin_server.admin_enabled"}
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

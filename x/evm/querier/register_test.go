package querier

import (
	"reflect"
	"sort"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
)

// TestDeclaredKeysAreTheOnesItsReaderResolves holds the derived keys against the reader's own constant.
//
// The section registers the struct its reader fills, so a mapstructure tag is the only spelling of its
// key and there is no second list to fall behind. What remains is the constant ReadConfig looks up, which
// states the same key again a few lines away, and a rename that moves one and not the other compiles.
func TestDeclaredKeysAreTheOnesItsReaderResolves(t *testing.T) {
	want := []string{flagGasLimit}
	sort.Strings(want)

	section, ok := registry.Lookup(SectionName)
	if !ok {
		t.Fatalf("%s is not registered, so nothing resolves its keys", SectionName)
	}
	if !reflect.DeepEqual(section.Keys, want) {
		t.Errorf("%s declares\n  %v\nand its reader resolves\n  %v", SectionName, section.Keys, want)
	}
}

// TestDefaultsAreTheReaderOwnForEveryMode covers the value side of the same registration.
func TestDefaultsAreTheReaderOwnForEveryMode(t *testing.T) {
	for _, mode := range registry.Modes() {
		got, ok := defaults(mode).(Config)
		if !ok {
			t.Fatalf("mode %q: defaults returned %T, want the type its reader fills", mode, defaults(mode))
		}
		if got != DefaultConfig {
			t.Errorf("mode %q resolves to %+v, want the reader's own default %+v", mode, got, DefaultConfig)
		}
	}
}

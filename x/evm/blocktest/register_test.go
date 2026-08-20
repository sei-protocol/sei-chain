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
// keys and there is no second list to fall behind. What remains is the constants ReadConfig looks up,
// which state the same keys again a few lines away, and a rename that moves one and not the other
// compiles.
func TestDeclaredKeysAreTheOnesItsReaderResolves(t *testing.T) {
	want := []string{flagEnabled, flagTestDataPath}
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
//
// Off for every mode, which is the value worth pinning: a mode that resolved this on would have those
// nodes replay recorded data instead of serving the chain.
func TestDefaultsAreTheReaderOwnForEveryMode(t *testing.T) {
	for _, mode := range registry.Modes() {
		got, ok := defaults(mode).(Config)
		if !ok {
			t.Fatalf("mode %q: defaults returned %T, want the type its reader fills", mode, defaults(mode))
		}
		if got != DefaultConfig {
			t.Errorf("mode %q resolves to %+v, want the reader's own default %+v", mode, got, DefaultConfig)
		}
		if got.Enabled {
			t.Errorf("mode %q resolves the block-test harness on", mode)
		}
	}
}

package replay

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
	want := []string{flagEnabled, flagEthRPC, flagEthDataDir, flagContractStateChecks}
	sort.Strings(want)

	section, ok := registry.Lookup(SectionName)
	if !ok {
		t.Fatalf("%s is not registered, so nothing resolves its keys", SectionName)
	}
	if !reflect.DeepEqual(section.Keys, want) {
		t.Errorf("%s declares\n  %v\nand its reader resolves\n  %v", SectionName, section.Keys, want)
	}
}

// TestTheWrittenSpellingOfTheStateCheckIsNotDeclared covers a name that is written and never read.
//
// The app.toml template renders eth_replay_contract_state_checks and the reader looks up
// contract_state_checks, so every generated file carries a name nothing resolves. Declaring that name
// would add a key an operator can set and no reader answers, which is the one outcome worse than the
// mismatch itself: a value that looks as though it applied.
func TestTheWrittenSpellingOfTheStateCheckIsNotDeclared(t *testing.T) {
	section, ok := registry.Lookup(SectionName)
	if !ok {
		t.Fatalf("%s is not registered, so nothing resolves its keys", SectionName)
	}
	for _, key := range section.Keys {
		if key == SectionName+".eth_replay_contract_state_checks" {
			t.Errorf("%s is declared and no reader looks it up", key)
		}
	}
}

// TestDefaultsAreTheReaderOwnForEveryMode covers the value side of the same registration.
//
// Off for every mode, which is the value worth pinning: turning replay on makes application construction
// dial the endpoint, so a mode that resolved it on would stop those nodes booting.
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
			t.Errorf("mode %q resolves replay on, which makes those nodes dial %q at construction",
				mode, got.EthRPC)
		}
	}
}

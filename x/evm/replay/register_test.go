package replay

import (
	"reflect"
	"sort"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
)

// TestDeclaredKeysAreTheOnesItsReaderResolves holds the derived keys against the reader's own constants.
//
// Three of the four keys carry the name the template writes and one does not: the template renders
// eth_replay_contract_state_checks and the reader looks up contract_state_checks. The declared key is the
// one a value reaches a reader through, and the exact comparison below is what keeps the other out.
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
	want := []string{flagEnabled, flagEthRPC, flagEthDataDir, flagContractStateChecks}
	sort.Strings(want)
	if !reflect.DeepEqual(section.Keys, want) {
		t.Errorf("%s declares\n  %v\nand its reader resolves\n  %v", SectionName, section.Keys, want)
	}
}

// TestEachKeyResolvesToTheValueItsFieldHolds covers the binding a key set cannot show.
//
// Two of these fields are strings holding an endpoint and a directory. A tag on the wrong field leaves the
// key set identical and resolves a filesystem path where a reader expects a URL.
func TestEachKeyResolvesToTheValueItsFieldHolds(t *testing.T) {
	for _, mode := range registry.Modes() {
		resolved, err := registry.Resolve(mode, registry.Sources{})
		if err != nil {
			t.Fatalf("mode %q: %v", mode, err)
		}
		for key, want := range map[string]any{
			flagEnabled:             DefaultConfig.Enabled,
			flagEthRPC:              DefaultConfig.EthRPC,
			flagEthDataDir:          DefaultConfig.EthDataDir,
			flagContractStateChecks: DefaultConfig.ContractStateChecks,
		} {
			if got := resolved.Values[key]; !reflect.DeepEqual(got, want) {
				t.Errorf("mode %q: %s resolves to %#v (%T), want %#v (%T)", mode, key, got, got, want, want)
			}
		}
		if resolved.Values[flagEnabled] == true {
			t.Errorf("mode %q resolves replay on, so those nodes would replay recorded data from %v instead "+
				"of following the chain", mode, resolved.Values[flagEthRPC])
		}
	}
}

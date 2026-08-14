package querier

import (
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

func TestTheRegisteredBaselineIsWhatANodeRunsToday(t *testing.T) {
	section, ok := registry.Lookup(SectionName)
	if !ok {
		t.Fatalf("%s did not register, so nothing below measures anything", SectionName)
	}
	for _, mode := range registry.Modes() {
		got, isConfig := section.Defaults(mode).(Config)
		if !isConfig {
			t.Fatalf("the baseline for %q is %T, not this package's own type", mode, section.Defaults(mode))
		}
		if got != DefaultConfig {
			t.Errorf("the baseline for %q mode is %+v and this package's default is %+v. Registering a "+
				"section must not change what a node runs, and a difference here changes how much work "+
				"a query may do while reading as a refactor", mode, got, DefaultConfig)
		}
	}
}

func TestTheDerivedKeysAreTheKeysThisReaderResolves(t *testing.T) {
	section, ok := registry.Lookup(SectionName)
	if !ok {
		t.Fatalf("%s did not register", SectionName)
	}
	derived := map[string]bool{}
	for _, key := range section.Keys {
		derived[key] = true
	}
	if !derived[flagGasLimit] {
		t.Errorf("this package's reader resolves %q and the registry derives %v. An operator's value "+
			"reaches one of those spellings and not the other", flagGasLimit, section.Keys)
	}
	if len(section.Keys) != 1 {
		t.Errorf("the registry derived %d keys from a one-field struct: %v", len(section.Keys), section.Keys)
	}
}

func TestRegisteringProducedNoDefect(t *testing.T) {
	for _, defect := range registry.Defects() {
		if defect.Section == SectionName {
			t.Errorf("registering %s was refused: %v\n\nThe section is absent from the registry, so "+
				"every one of its keys silently reads from the legacy path instead",
				defect.Section, defect.Err)
		}
	}
}

func TestNoExperimentalKeyShadowsThisSectionAfterRegistration(t *testing.T) {
	section, ok := registry.Lookup(SectionName)
	if !ok {
		t.Fatalf("%s did not register", SectionName)
	}
	specs := make([]configtest.KeySpec, 0, len(section.Keys))
	for _, key := range section.Keys {
		specs = append(specs, configtest.KeySpec{Key: key})
	}
	configtest.CheckNoExperimentalKeyShadowsThisSection(t, "evm_query", specs)
}

// TestTheZeroWhenAbsentDeclarationMatchesThisReader holds what a migration writes for a key this
// section's keys are absent from, against what the reader actually does with an absent key.
func TestTheZeroWhenAbsentDeclarationMatchesThisReader(t *testing.T) {
	configtest.CheckZeroWhenAbsentMatchesTheReader(t, "evm_query",
		func(o configtest.AppOpts) (any, error) { return ReadConfig(o) })
}

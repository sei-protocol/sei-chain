package app

import (
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

func TestTheRegisteredLightInvarianceBaselineIsWhatANodeRunsToday(t *testing.T) {
	section, ok := registry.Lookup(LightInvarianceSectionName)
	if !ok {
		t.Fatalf("%s did not register, so nothing below measures anything", LightInvarianceSectionName)
	}
	for _, mode := range registry.Modes() {
		got, isConfig := section.Defaults(mode).(LightInvarianceConfig)
		if !isConfig {
			t.Fatalf("the baseline for %q is %T, not this package's own type", mode, section.Defaults(mode))
		}
		if got != DefaultLightInvarianceConfig {
			t.Errorf("the baseline for %q mode is %+v and this package's default is %+v. Registering a "+
				"section must not change what a node runs, and a difference here reads as a refactor",
				mode, got, DefaultLightInvarianceConfig)
		}
		if !got.SupplyEnabled {
			t.Errorf("the baseline for %q mode turns the supply check off. That check is what tells a "+
				"node its recorded total supply no longer matches what its store holds", mode)
		}
	}
}

func TestTheDerivedLightInvarianceKeysAreTheKeysThisReaderResolves(t *testing.T) {
	section, ok := registry.Lookup(LightInvarianceSectionName)
	if !ok {
		t.Fatalf("%s did not register", LightInvarianceSectionName)
	}
	derived := map[string]bool{}
	for _, key := range section.Keys {
		derived[key] = true
	}
	if !derived[flagSupplyEnabled] {
		t.Errorf("this package's reader resolves %q and the registry derives %v. An operator's value "+
			"reaches one of those spellings and not the other", flagSupplyEnabled, section.Keys)
	}
	if len(section.Keys) != 1 {
		t.Errorf("the registry derived %d keys from a one-field struct: %v", len(section.Keys), section.Keys)
	}
}

func TestRegisteringLightInvarianceProducedNoDefect(t *testing.T) {
	for _, defect := range registry.Defects() {
		if defect.Section == LightInvarianceSectionName {
			t.Errorf("registering %s was refused: %v\n\nThe section is absent from the registry, so "+
				"every one of its keys silently reads from the legacy path instead",
				defect.Section, defect.Err)
		}
	}
}

func TestNoExperimentalKeyShadowsLightInvarianceAfterRegistration(t *testing.T) {
	section, ok := registry.Lookup(LightInvarianceSectionName)
	if !ok {
		t.Fatalf("%s did not register", LightInvarianceSectionName)
	}
	specs := make([]configtest.KeySpec, 0, len(section.Keys))
	for _, key := range section.Keys {
		specs = append(specs, configtest.KeySpec{Key: key})
	}
	configtest.CheckNoExperimentalKeyShadowsThisSection(t, LightInvarianceSectionName, specs)
}

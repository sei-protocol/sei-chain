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

// TestTheGenesisSchemaDescribesTheReaderItStandsInFor is what holds the two apart-ness together.
//
// The schema declares the spelling and genesistypes.GenesisImportConfig holds the values, and nothing
// in the code connects a schema field to the setting it stands for. This writes a value under each
// declared key, asks the reader which setting changed, and checks the baseline against what the reader
// leaves that setting at when nothing is written. A field paired with the wrong setting fails here
// rather than resolving one operator's value into another's setting.
func TestTheGenesisSchemaDescribesTheReaderItStandsInFor(t *testing.T) {
	for _, mode := range registry.Modes() {
		// The section name stays a literal here. The wiring record reads it from this call's second
		// argument, and a constant or a table entry would record every schema check under one
		// placeholder row, so removing three of four would not show up as lost coverage.
		configtest.CheckSchemaMatchesTheReader(t, "genesis", configtest.SchemaCheck{
			Mode: mode,
			Read: func(opts configtest.AppOpts) (any, error) {
				return ReadGenesisImportConfig(opts)
			},
			Probe: map[string]any{
				flagGenesisStreamImport: true,
				flagGenesisImportFile:   "/mnt/genesis/stream.json",
			},
		})
	}
}

func TestTheDerivedGenesisKeysAreTheKeysThisReaderResolves(t *testing.T) {
	section, ok := registry.Lookup(GenesisSectionName)
	if !ok {
		t.Fatalf("%s did not register", GenesisSectionName)
	}
	derived := map[string]bool{}
	for _, key := range section.Keys {
		derived[key] = true
	}
	for _, live := range []string{flagGenesisStreamImport, flagGenesisImportFile} {
		if !derived[live] {
			t.Errorf("this package's reader resolves %q and the registry derives %v. An operator's "+
				"value reaches one of those spellings and not the other", live, section.Keys)
		}
	}
	if len(section.Keys) != 2 {
		t.Errorf("the registry derived %d keys from a two-field schema: %v", len(section.Keys), section.Keys)
	}
}

func TestRegisteringGenesisProducedNoDefect(t *testing.T) {
	for _, defect := range registry.Defects() {
		if defect.Section == GenesisSectionName {
			t.Errorf("registering %s was refused: %v\n\nThe section is absent from the registry, so "+
				"every one of its keys silently reads from the legacy path instead",
				defect.Section, defect.Err)
		}
	}
}

func TestNoExperimentalKeyShadowsGenesisAfterRegistration(t *testing.T) {
	section, ok := registry.Lookup(GenesisSectionName)
	if !ok {
		t.Fatalf("%s did not register", GenesisSectionName)
	}
	specs := make([]configtest.KeySpec, 0, len(section.Keys))
	for _, key := range section.Keys {
		specs = append(specs, configtest.KeySpec{Key: key})
	}
	configtest.CheckNoExperimentalKeyShadowsThisSection(t, GenesisSectionName, specs)
}

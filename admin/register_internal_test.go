package admin

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
				"section must not change what a node runs, and a difference here starts an "+
				"administrative surface on one mode's nodes while reading as a refactor",
				mode, got, DefaultConfig)
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
	for _, live := range []string{flagEnabled, flagAddress} {
		if !derived[live] {
			t.Errorf("this package's reader resolves %q and the registry derives %v. An operator's "+
				"value reaches one of those spellings and not the other", live, section.Keys)
		}
	}
	if len(section.Keys) != 2 {
		t.Errorf("the registry derived %d keys from a two-field struct: %v", len(section.Keys), section.Keys)
	}
}

// TestTheRegistryChecksTheLoopbackRule holds the one rule this section states.
//
// The address decides who can reach a surface that changes a running node's log level. The boot
// refuses a non-loopback address, and this is what makes a diagnostic refuse the same file rather
// than reporting it as fine and leaving the refusal for the next restart.
func TestTheRegistryChecksTheLoopbackRule(t *testing.T) {
	section, ok := registry.Lookup(SectionName)
	if !ok {
		t.Fatalf("%s did not register", SectionName)
	}
	if section.Validate == nil {
		t.Fatal("the registry states no rule for this section, so a resolved configuration binding " +
			"the administrative surface to a routable address reads as usable")
	}

	exposed := map[string]any{"admin_enabled": true, "admin_address": "0.0.0.0:9095"}
	if err := section.Validate(exposed); err == nil {
		t.Error("a routable administrative address was accepted; the boot refuses it, so a diagnostic " +
			"reporting it as usable sends an operator into a node that will not start")
	}
	if err := section.Validate(map[string]any{"admin_enabled": true, "admin_address": DefaultAddress}); err != nil {
		t.Errorf("the default address was refused: %v", err)
	}
	// Disabled binds nothing, so the address is not the operator's problem. Refusing here would
	// refuse the configuration every node that never wrote the key resolves to.
	if err := section.Validate(map[string]any{"admin_enabled": false, "admin_address": "0.0.0.0:9095"}); err != nil {
		t.Errorf("a disabled server with an unused address was refused: %v", err)
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
	configtest.CheckNoExperimentalKeyShadowsThisSection(t, SectionName, specs)
}

// TestTheZeroWhenAbsentDeclarationMatchesThisReader holds what a migration writes for a key this
// section's keys are absent from, against what the reader actually does with an absent key.
func TestTheZeroWhenAbsentDeclarationMatchesThisReader(t *testing.T) {
	configtest.CheckZeroWhenAbsentMatchesTheReader(t, "admin_server",
		func(o configtest.AppOpts) (any, error) { return ReadConfig(o) })
}

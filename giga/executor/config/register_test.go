package config_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
	gigaconfig "github.com/sei-protocol/sei-chain/giga/executor/config"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// This section is the first to enter the configuration registry, so it is where the rules a migrating
// section has to satisfy get held. Every section that follows repeats these three.

// TestTheRegisteredBaselineIsWhatANodeRunsToday is the rule that makes migrating a section safe.
//
// Declaring a section moves where its values come from. If the registered baseline differed from the
// package's own defaults, that move would change what a node does, and the change would arrive
// disguised as a refactor with nothing in the diff naming it.
//
// Held for every mode, because a mode-varying baseline is exactly how such a change would enter: the
// design's worked example turns OCC off on an archive node, which is a decision about the executor
// and not something this registration may make on its behalf.
func TestTheRegisteredBaselineIsWhatANodeRunsToday(t *testing.T) {
	section, ok := registry.Lookup(gigaconfig.SectionName)
	if !ok {
		t.Fatalf("%s did not register, so nothing below measures anything. Importing this package is "+
			"what registers it", gigaconfig.SectionName)
	}

	for _, mode := range registry.Modes() {
		got, isConfig := section.Defaults(mode).(gigaconfig.Config)
		if !isConfig {
			t.Fatalf("the baseline for %q is %T, not this package's own type", mode, section.Defaults(mode))
		}
		if got != gigaconfig.DefaultConfig {
			t.Errorf("the baseline for %q mode is %+v and this package's default is %+v. Registering a "+
				"section must not change what a node runs, and a difference here does exactly that "+
				"while reading as a refactor", mode, got, gigaconfig.DefaultConfig)
		}
	}
}

// TestTheDerivedKeysAreTheKeysThisReaderResolves holds the spelling against the live reader.
//
// The registry derives dotted keys from the struct tags while this package's reader resolves the flag
// constants. A difference between them renames a key operators already have in their files, and the
// rename would be invisible: both spellings look plausible and only one is read.
func TestTheDerivedKeysAreTheKeysThisReaderResolves(t *testing.T) {
	section, ok := registry.Lookup(gigaconfig.SectionName)
	if !ok {
		t.Fatalf("%s did not register", gigaconfig.SectionName)
	}

	derived := map[string]bool{}
	for _, key := range section.Keys {
		derived[key] = true
	}
	for _, live := range []string{gigaconfig.FlagEnabled, gigaconfig.FlagOCCEnabled} {
		if !derived[live] {
			t.Errorf("this package's reader resolves %q and the registry derives %v. An operator's "+
				"value reaches one of those spellings and not the other", live, section.Keys)
		}
	}
	if len(section.Keys) != 2 {
		t.Errorf("the registry derived %d keys from a two-field struct: %v. An extra key is one no "+
			"operator writes, and a missing one is a setting the registry cannot see",
			len(section.Keys), section.Keys)
	}
}

// TestRegisteringProducedNoDefect is what turns a refused registration into a failure here.
//
// A defective registration is recorded rather than panicked, because this package is linked into every
// seid invocation and a panic at init would take down seid --help. Recorded, it is inert: the section
// is absent and every key silently falls back to whatever the legacy path resolves. This is where that
// becomes visible.
func TestRegisteringProducedNoDefect(t *testing.T) {
	for _, defect := range registry.Defects() {
		if defect.Section == gigaconfig.SectionName {
			t.Errorf("registering %s was refused: %v\n\nThe section is absent from the registry, so "+
				"every one of its keys silently reads from the legacy path instead",
				defect.Section, defect.Err)
		}
	}
}

// TestNoExperimentalKeyShadowsThisSection is this section's half of the experimental collision check.
//
// It has to run from this package's own test binary, because a KeySpec manifest is an unexported
// package-level var in a _test.go file and no test elsewhere can reference it.
func TestNoExperimentalKeyShadowsThisSectionAfterRegistration(t *testing.T) {
	section, ok := registry.Lookup(gigaconfig.SectionName)
	if !ok {
		t.Fatalf("%s did not register", gigaconfig.SectionName)
	}

	specs := make([]configtest.KeySpec, 0, len(section.Keys))
	for _, key := range section.Keys {
		specs = append(specs, configtest.KeySpec{Key: key})
	}
	configtest.CheckNoExperimentalKeyShadowsThisSection(t, gigaconfig.SectionName, specs)
}

// TestTheZeroWhenAbsentDeclarationMatchesThisReader holds what a migration writes for a key this
// section's keys are absent from, against what the reader actually does with an absent key.
func TestTheZeroWhenAbsentDeclarationMatchesThisReader(t *testing.T) {
	configtest.CheckZeroWhenAbsentMatchesTheReader(t, "giga_executor",
		func(o configtest.AppOpts) (any, error) { return gigaconfig.ReadConfig(o) })
}

package cosmosbase_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/config/cosmosbase"
	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	srvconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

func TestTheRegisteredStateSyncBaselineIsWhatANodeRunsToday(t *testing.T) {
	section, ok := registry.Lookup(cosmosbase.StateSyncSectionName)
	if !ok {
		t.Fatalf("%s did not register, so nothing below measures anything", cosmosbase.StateSyncSectionName)
	}
	want := srvconfig.DefaultConfig().StateSync
	for _, mode := range registry.Modes() {
		got, isConfig := section.Defaults(mode).(srvconfig.StateSyncConfig)
		if !isConfig {
			t.Fatalf("the baseline for %q is %T, not the upstream type", mode, section.Defaults(mode))
		}
		if got != want {
			t.Errorf("the baseline for %q mode is %+v and the upstream default is %+v. Registering a "+
				"section must not change what a node runs, and a difference here changes how often a "+
				"node snapshots while reading as a refactor", mode, got, want)
		}
	}
}

// TestTheDerivedKeysAreTheKeysTheReadersResolve holds the spelling against the constants.
//
// This section needs no schema written for it, unlike the two SeiDB sections, because the upstream type's
// mapstructure tags already name the keys the readers look up. Held rather than assumed: if those tags
// ever drift, this section quietly starts declaring keys nothing reads.
func TestTheDerivedKeysAreTheKeysTheReadersResolve(t *testing.T) {
	section, ok := registry.Lookup(cosmosbase.StateSyncSectionName)
	if !ok {
		t.Fatalf("%s did not register", cosmosbase.StateSyncSectionName)
	}
	derived := map[string]bool{}
	for _, key := range section.Keys {
		derived[key] = true
	}
	live := []string{
		server.FlagStateSyncSnapshotInterval,
		server.FlagStateSyncSnapshotKeepRecent,
		server.FlagStateSyncSnapshotDir,
	}
	for _, key := range live {
		if !derived[key] {
			t.Errorf("a reader resolves %q and the registry derives %v. An operator's value reaches one "+
				"of those spellings and not the other", key, section.Keys)
		}
	}
	if len(section.Keys) != len(live) {
		t.Errorf("the registry derived %d keys and the readers resolve %d state-sync settings: %v",
			len(section.Keys), len(live), section.Keys)
	}
}

func TestRegisteringProducedNoDefect(t *testing.T) {
	for _, defect := range registry.Defects() {
		if defect.Section == cosmosbase.StateSyncSectionName {
			t.Errorf("registering %s was refused: %v\n\nThe section is absent from the registry, so "+
				"every one of its keys silently reads from the legacy path instead",
				defect.Section, defect.Err)
		}
	}
}

func TestNoExperimentalKeyShadowsThisSectionAfterRegistration(t *testing.T) {
	section, ok := registry.Lookup(cosmosbase.StateSyncSectionName)
	if !ok {
		t.Fatalf("%s did not register", cosmosbase.StateSyncSectionName)
	}
	specs := make([]configtest.KeySpec, 0, len(section.Keys))
	for _, key := range section.Keys {
		specs = append(specs, configtest.KeySpec{Key: key})
	}
	// The section name stays a literal. The wiring record reads it from this call's second argument, so a
	// constant would record every section in this package under one placeholder row.
	configtest.CheckNoExperimentalKeyShadowsThisSection(t, "state-sync", specs)
}

// TestWiringMatchesTheRecord records which checks this package calls.
//
// A deleted check is the one edit the remaining checks cannot report, so the set of calls is held in a
// file rather than in whoever last read this directory.
func TestWiringMatchesTheRecord(t *testing.T) {
	configtest.CheckWiring(t)
}

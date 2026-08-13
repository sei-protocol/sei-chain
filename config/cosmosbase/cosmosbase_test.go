package cosmosbase_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/config/cosmosbase"
	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	srvconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/spf13/viper"
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

// TestTheBaseSectionDescribesTheReaderItStandsInFor holds the node-wide keys against GetConfig.
//
// These keys need no schema written for them: BaseConfig's own mapstructure tags are the keys GetConfig
// reads, so it registers directly. What is unusual is only that the keys carry no prefix.
//
// GetConfig is the reader a running node uses. The other reader of this configuration, ParseConfig, is not
// a runtime path at all: it unmarshals into a struct to generate app.toml when the file is absent, so the
// tags decide what a fresh file contains while the keys decide what a node reads.
func TestTheBaseSectionDescribesTheReaderItStandsInFor(t *testing.T) {
	// The section name stays a literal. The wiring record reads it from this call's second argument.
	configtest.CheckSchemaMatchesTheReader(t, "base", configtest.SchemaCheck{
		Read: readBaseConfig,
		// Each probe differs from what an absent key casts to, which for every one of these is zero.
		Probe: map[string]any{
			"minimum-gas-prices":  "0.02usei",
			"pruning":             "everything",
			"pruning-keep-recent": "5",
			"pruning-interval":    "7",
			"halt-height":         uint64(100),
			"halt-time":           uint64(200),
			"min-retain-blocks":   uint64(300),
			"inter-block-cache":   true,
			"index-events":        []string{"tx.height"},
			"compaction-interval": uint64(400),
			"concurrency-workers": 9,
			"occ-enabled":         true,
		},
		Skip: map[string]string{
			"pruning-keep-every": "GetConfig does not read it; the pruning options a node runs are built " +
				"by GetPruningOptionsFromFlags, and TestPruningKeepEveryIsReadByThePruningOptions covers it",
		},
	})
}

// readBaseConfig runs the reader a running node uses over a set of written values.
//
// The telemetry labels are supplied because GetConfig asserts their type outright rather than casting, and
// refuses the whole configuration when the assertion fails. That is the one read of seventy-two that can
// stop a node, and it is why the telemetry table needs deciding about separately from these keys.
func readBaseConfig(opts configtest.AppOpts) (any, error) {
	v := viper.New()
	v.Set("telemetry.global-labels", []any{})
	for key, value := range opts {
		v.Set(key, value)
	}
	cfg, err := srvconfig.GetConfig(v)
	if err != nil {
		return nil, err
	}
	return cfg.BaseConfig, nil
}

// TestPruningKeepEveryIsReadByThePruningOptions covers the one key GetConfig leaves alone.
//
// Its value reaches a node through the pruning options the application is built with, not through the
// server configuration, so it is declared here and checked against the reader that resolves it.
func TestPruningKeepEveryIsReadByThePruningOptions(t *testing.T) {
	section, ok := registry.Lookup(cosmosbase.BaseSectionName)
	if !ok {
		t.Fatalf("%s did not register", cosmosbase.BaseSectionName)
	}
	declared := false
	for _, key := range section.Keys {
		if key == "pruning-keep-every" {
			declared = true
		}
	}
	if !declared {
		t.Fatal("pruning-keep-every is not declared, so skipping it in the check above hides nothing " +
			"and this test measures nothing")
	}

	// Custom is the one strategy that reads the three interval keys at all.
	opts := configtest.AppOpts{
		server.FlagPruning:           "custom",
		server.FlagPruningKeepRecent: "100",
		server.FlagPruningInterval:   "10",
		server.FlagPruningKeepEvery:  "3",
	}
	got, err := server.GetPruningOptionsFromFlags(opts)
	if err != nil {
		t.Fatalf("the reader refused a usable custom pruning configuration: %v", err)
	}
	if got.KeepEvery != 3 {
		t.Errorf("pruning-keep-every = 3 resolved to KeepEvery %d. The key is declared, so a value an "+
			"operator writes has to reach the setting it names", got.KeepEvery)
	}
}

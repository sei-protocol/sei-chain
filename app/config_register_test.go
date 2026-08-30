package app

import (
	"reflect"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
	srvconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	"github.com/sei-protocol/sei-chain/sei-db/config"
)

// requireResolves holds a section's resolved values against what its reader's own defaults hold.
//
// Resolving renders every registered section, so a section elsewhere whose defaults cannot state a value
// for a key it declares fails here too. The registry names that section in the error, so the message
// points at the real one rather than at whichever test asked.
//
// Resolving is what to compare against rather than the registered struct, because the resolved map carries
// the key a tag produced and the value that tag's field held. A comparison of struct to struct agrees with
// itself while two tags sit on the wrong fields, since each field still holds the value the test names for
// it. The swap moves the value to the other key, and this notices.
func requireResolves(t *testing.T, mode registry.Mode, section string, want map[string]any) {
	t.Helper()
	resolved, err := registry.Resolve(mode, registry.Sources{})
	if err != nil {
		t.Fatalf("mode %q: %v", mode, err)
	}
	for key, expected := range want {
		if got := resolved.Values[key]; !reflect.DeepEqual(got, expected) {
			t.Errorf("mode %q: %s resolves to %#v (%T), want %#v (%T)",
				mode, key, got, got, expected, expected)
		}
	}
}

// TestLightInvarianceResolves covers the one section registered as the type its reader fills.
func TestLightInvarianceResolves(t *testing.T) {
	for _, mode := range registry.Modes() {
		requireResolves(t, mode, LightInvarianceSectionName, map[string]any{
			flagSupplyEnabled: DefaultLightInvarianceConfig.SupplyEnabled,
		})
	}
}

// TestGenesisResolves holds the genesis schema's values against what its readers' defaults hold.
//
// Three keys. Two are read by this package: stream-import through a guarded cast, and import-file through a
// type assertion. The third is read by the upstream server into its own configuration and not by this
// package at all.
func TestGenesisResolves(t *testing.T) {
	for _, mode := range registry.Modes() {
		requireResolves(t, mode, GenesisSectionName, map[string]any{
			flagGenesisStreamImport:       DefaultGenesisConfig.StreamGenesisImport,
			flagGenesisImportFile:         DefaultGenesisConfig.GenesisStreamFile,
			"genesis.genesis-stream-file": srvconfig.DefaultConfig().Genesis.GenesisStreamFile,
		})
	}
}

// TestStateStoreResolvesWhatEachKindOfNodeNeeds is the mode-varying half of this section.
//
// Two of these settings mean something different depending on what kind of node asks, and the values are
// written out here rather than taken from the same rules the section reads. An archive node exists to keep
// history, so a retention that pruned it would be the one declaration here that destroys data, and it
// would do so with nothing to alert on, because pruning frees disk rather than filling it.
func TestStateStoreResolvesWhatEachKindOfNodeNeeds(t *testing.T) {
	byMode := map[registry.Mode]struct {
		enable     bool
		keepRecent int
	}{
		registry.ModeValidator: {enable: false, keepRecent: 100000},
		registry.ModeSeed:      {enable: false, keepRecent: 100000},
		registry.ModeFull:      {enable: true, keepRecent: 100000},
		registry.ModeArchive:   {enable: true, keepRecent: 0},
	}
	for _, mode := range registry.Modes() {
		want, named := byMode[mode]
		if !named {
			t.Fatalf("mode %q has no expectation here, so a mode was added and this was not revisited", mode)
		}
		requireResolves(t, mode, StateStoreSectionName, map[string]any{
			FlagSSEnable:     want.enable,
			FlagSSKeepRecent: want.keepRecent,
		})
	}
}

// TestStateStoreResolvesItsOtherValuesTheSameForEveryMode covers the ten settings a mode does not change.
func TestStateStoreResolvesItsOtherValuesTheSameForEveryMode(t *testing.T) {
	live := config.DefaultStateStoreConfig()
	for _, mode := range registry.Modes() {
		requireResolves(t, mode, StateStoreSectionName, map[string]any{
			FlagSSDirectory:         live.DBDirectory,
			FlagSSBackend:           live.Backend,
			FlagSSAsyncWriterBuffer: live.AsyncWriteBuffer,
			FlagSSPruneInterval:     live.PruneIntervalSeconds,
			FlagSSImportNumWorkers:  live.ImportNumWorkers,
			FlagSSReadWriteMetrics:  live.EnableReadWriteMetrics,
			FlagSSSnapshotEnable:    live.SnapshotEnable,
			FlagEVMSSDirectory:      live.EVMDBDirectory,
			FlagEVMSSSeparateDBs:    live.SeparateEVMSubDBs,
			FlagEVMSSSplit:          live.EVMSplit,
		})
	}
}

// TestStateCommitResolvesTheModuleDeclaredValues covers the value side of this registration.
//
// The write mode is a plain string here because the reader parses a written name into its own type, and
// comparing values is what holds it to that: the named type carries the same text and is not the same
// value.
func TestStateCommitResolvesTheModuleDeclaredValues(t *testing.T) {
	live := config.DefaultStateCommitConfig()
	for _, mode := range registry.Modes() {
		requireResolves(t, mode, StateCommitSectionName, map[string]any{
			FlagSCEnable:                     live.Enable,
			FlagSCIngressProfile:             live.IngressProfile,
			FlagSCDirectory:                  live.Directory,
			FlagSCAsyncCommitBuffer:          live.MemIAVLConfig.AsyncCommitBuffer,
			FlagSCSnapshotKeepRecent:         live.MemIAVLConfig.SnapshotKeepRecent,
			FlagSCSnapshotInterval:           live.MemIAVLConfig.SnapshotInterval,
			FlagSCSnapshotMinTimeInterval:    live.MemIAVLConfig.SnapshotMinTimeInterval,
			FlagSCSnapshotWriterLimit:        live.MemIAVLConfig.SnapshotWriterLimit,
			FlagSCSnapshotPrefetchThreshold:  live.MemIAVLConfig.SnapshotPrefetchThreshold,
			FlagSCSnapshotWriteRateMBps:      live.MemIAVLConfig.SnapshotWriteRateMBps,
			FlagSCHistoricalProofMaxInFlight: live.HistoricalProofMaxInFlight,
			FlagSCHistoricalProofRateLimit:   live.HistoricalProofRateLimit,
			FlagSCHistoricalProofBurst:       live.HistoricalProofBurst,
			FlagSCSubspaceQueryMaxInFlight:   live.SubspaceQueryMaxInFlight,
			FlagSCSubspaceMaxPairs:           live.SubspaceMaxPairs,
			FlagSCSubspaceMaxBytes:           live.SubspaceMaxBytes,
			FlagSCWriteMode:                  string(live.WriteMode),
			FlagSCWriteModeEnableAuto:        live.WriteModeEnableAuto,
			FlagSCHashLoggerEnable:           live.HashLogger.Enable,
			FlagSCHashLoggerDirectory:        live.HashLogger.Directory,
			FlagSCHashLoggerBlocksToRetain:   live.HashLogger.BlocksToRetain,
			FlagSCHashLoggerTargetFileSize:   live.HashLogger.TargetFileSize,
			FlagSCHashLoggerMaxDiskSize:      live.HashLogger.MaxDiskSize,
			FlagSCFlatKVReadWriteMetrics:     live.FlatKVConfig.EnableReadWriteMetrics,
		})
	}
}

// TestStateCommitWriteModeDefaultIsOneTheReaderAccepts covers the one declared value that is parsed text.
//
// Every other declared value is used as it stands. This one is a name the reader turns into a mode, so a
// default nothing parses would put a value in a generated file that stops the node it was generated for.
func TestStateCommitWriteModeDefaultIsOneTheReaderAccepts(t *testing.T) {
	for _, mode := range registry.Modes() {
		resolved, err := registry.Resolve(mode, registry.Sources{})
		if err != nil {
			t.Fatalf("mode %q: %v", mode, err)
		}
		declared, ok := resolved.Values[FlagSCWriteMode].(string)
		if !ok {
			t.Fatalf("mode %q: %s resolves to %T, and the reader parses text",
				mode, FlagSCWriteMode, resolved.Values[FlagSCWriteMode])
		}
		if _, err := config.ParseSCWriteMode(declared); err != nil {
			t.Errorf("mode %q: %s resolves to %q, which this binary's own reader refuses: %v",
				mode, FlagSCWriteMode, declared, err)
		}
	}
}

// TestTheSectionsThisPackageRegistersAreUsable covers what the registry refuses.
//
// Scoped to the four names this file registers. The whole-registry sweep belongs where every section is
// linked, because a refusal that depends on what else registered is not this package's to answer for.
func TestTheSectionsThisPackageRegistersAreUsable(t *testing.T) {
	mine := map[string]bool{
		LightInvarianceSectionName: true,
		GenesisSectionName:         true,
		StateStoreSectionName:      true,
		StateCommitSectionName:     true,
	}
	for _, defect := range registry.Defects() {
		if mine[defect.Section] {
			t.Errorf("%s was refused, so none of its keys is declared: %v", defect.Section, defect.Err)
		}
	}
}

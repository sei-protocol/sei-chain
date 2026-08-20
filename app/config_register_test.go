package app

import (
	"reflect"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/sei-db/config"
)

// requireSectionResolves holds one section's resolved keys and values against what its reader asks for.
//
// Resolving is what to compare against rather than the registered struct, because the resolved map is
// what a caller reads: it carries the key a tag produced and the value that tag's field held. A
// comparison of struct to struct agrees with itself while two tags are on the wrong fields, since each
// field still holds the value the test names for it. This one does not, because the swap moves the value
// to the other key.
//
// The values come from the reader's own constants and its own defaults, so a renamed key or a changed
// default fails here rather than being restated correctly in two places and wrongly in a third.
func requireSectionResolves(t *testing.T, mode registry.Mode, section string, want map[string]any) {
	t.Helper()
	registered, ok := registry.Lookup(section)
	if !ok {
		t.Fatalf("%s is not registered, so nothing resolves its keys", section)
	}
	resolved, err := registry.Resolve(mode, registry.Sources{})
	if err != nil {
		t.Fatalf("mode %q: %v", mode, err)
	}

	declared := make(map[string]bool, len(registered.Keys))
	for _, key := range registered.Keys {
		declared[key] = true
		expected, named := want[key]
		if !named {
			t.Errorf("mode %q: %s declares %s and nothing here names a value for it, so either its reader "+
				"resolves the key and this list is short, or no reader does and an operator has a setting "+
				"that changes nothing", mode, section, key)
			continue
		}
		if got := resolved.Values[key]; !reflect.DeepEqual(got, expected) {
			t.Errorf("mode %q: %s resolves to %#v (%T), want %#v (%T)", mode, key, got, got, expected, expected)
		}
	}
	for key := range want {
		if !declared[key] {
			t.Errorf("mode %q: %s does not declare %s, which its reader resolves, so that setting stays "+
				"answered by whatever answers it today and nothing reports it", mode, section, key)
		}
	}
}

// TestLightInvarianceResolves covers the one section registered as the type its reader fills.
//
// Every mode, because this section's defaults are the same for all of them: the check compares the bank
// module's recorded total supply against what the store holds, which is a property of every node.
func TestLightInvarianceResolves(t *testing.T) {
	for _, mode := range registry.Modes() {
		requireSectionResolves(t, mode, LightInvarianceSectionName, map[string]any{
			flagSupplyEnabled: DefaultLightInvarianceConfig.SupplyEnabled,
		})
	}
}

// TestGenesisResolves holds the genesis schema against the reader's own constants.
//
// The two keys carry different types, which is what makes the pairing worth asserting: the schema states
// the tags in one place and the reader looks them up in another, and nothing but this holds the two
// together.
func TestGenesisResolves(t *testing.T) {
	for _, mode := range registry.Modes() {
		requireSectionResolves(t, mode, GenesisSectionName, map[string]any{
			flagGenesisStreamImport: DefaultGenesisConfig.StreamGenesisImport,
			flagGenesisImportFile:   DefaultGenesisConfig.GenesisStreamFile,
		})
	}
}

// TestStateStoreResolves holds the state store schema against every key parseSSConfigs resolves.
//
// Twelve keys, including ss-snapshot-enable, which is the one read that checks whether the key was
// present. A schema short of a key its reader resolves leaves that setting undeclared, so it keeps
// whatever answers it today and no diagnostic names it.
func TestStateStoreResolves(t *testing.T) {
	live := config.DefaultStateStoreConfig()
	for _, mode := range registry.Modes() {
		requireSectionResolves(t, mode, StateStoreSectionName, map[string]any{
			FlagSSEnable:            live.Enable,
			FlagSSDirectory:         live.DBDirectory,
			FlagSSBackend:           live.Backend,
			FlagSSAsyncWriterBuffer: live.AsyncWriteBuffer,
			FlagSSKeepRecent:        live.KeepRecent,
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

// TestStateCommitResolves holds the state commit schema against every key parseSCConfigs resolves.
//
// Twenty keys, one of them a segment below the section, since the flat key-value read is
// state-commit.flatkv.enable-read-write-metrics. The write mode is a plain string here because the
// reader parses a written name into its own type, and comparing values is what holds it to that: the
// named type carries the same text and is not the same value.
func TestStateCommitResolves(t *testing.T) {
	live := config.DefaultStateCommitConfig()
	for _, mode := range registry.Modes() {
		requireSectionResolves(t, mode, StateCommitSectionName, map[string]any{
			FlagSCEnable:                     live.Enable,
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
// Every other declared default is a value its reader uses as it stands. This one is a name the reader
// turns into a mode, so a default nothing parses would put a value in a generated file that stops the
// node it was generated for.
func TestStateCommitWriteModeDefaultIsOneTheReaderAccepts(t *testing.T) {
	resolved, err := registry.Resolve(registry.ModeValidator, registry.Sources{})
	if err != nil {
		t.Fatalf("%v", err)
	}
	declared, ok := resolved.Values[FlagSCWriteMode].(string)
	if !ok {
		t.Fatalf("%s resolves to %T, and the reader parses text", FlagSCWriteMode, resolved.Values[FlagSCWriteMode])
	}
	if _, err := config.ParseSCWriteMode(declared); err != nil {
		t.Errorf("%s resolves to %q, which this binary's own reader refuses: %v", FlagSCWriteMode, declared, err)
	}
}

// TestEverySectionThisPackageRegistersIsWellFormed covers what the registry itself refuses.
//
// A section with a tag the registry cannot read is reported rather than returned, so a defect here is a
// section that registered and declares nothing a caller can resolve.
func TestEverySectionThisPackageRegistersIsWellFormed(t *testing.T) {
	for _, defect := range registry.Defects() {
		t.Errorf("%s is registered and defective: %v", defect.Section, defect.Err)
	}
}

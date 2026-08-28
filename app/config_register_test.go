package app

import (
	"reflect"
	"sort"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
	srvconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// manifestKeys returns the keys a section's read-site record names, plus any named here.
//
// The record is this package's own statement of which keys each reader looks up, kept for another purpose
// and held against a golden file. Taking the key set from it means a section's declaration is compared
// against something maintained under a different discipline, rather than against a list written beside it
// by the same hand in the same commit.
func manifestKeys(specs []configtest.KeySpec, also ...string) []string {
	out := make([]string, 0, len(specs)+len(also))
	for _, spec := range specs {
		out = append(out, spec.Key)
	}
	out = append(out, also...)
	sort.Strings(out)
	return out
}

// requireDeclares holds a section's declared keys against the record of what its reader looks up.
func requireDeclares(t *testing.T, section string, want []string) {
	t.Helper()
	for _, defect := range registry.Defects() {
		if defect.Section == section {
			t.Fatalf("%s was refused, so none of its keys is declared: %v", section, defect.Err)
		}
	}
	registered, ok := registry.Lookup(section)
	if !ok {
		t.Fatalf("%s is not registered, so nothing resolves its keys", section)
	}
	if !reflect.DeepEqual(registered.Keys, want) {
		t.Errorf("%s declares\n  %v\nand its read-site record names\n  %v\nA key on one side only is either "+
			"a setting an operator writes that no reader fills, or one this package reads and nothing "+
			"declares", section, registered.Keys, want)
	}
}

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

// TestLightInvarianceDeclaresAndResolves covers the one section registered as the type its reader fills.
func TestLightInvarianceDeclaresAndResolves(t *testing.T) {
	requireDeclares(t, LightInvarianceSectionName, manifestKeys(lightInvarianceKeys))
	for _, mode := range registry.Modes() {
		requireResolves(t, mode, LightInvarianceSectionName, map[string]any{
			flagSupplyEnabled: DefaultLightInvarianceConfig.SupplyEnabled,
		})
	}
}

// TestGenesisDeclaresAndResolves holds the genesis schema against the record and the reader's defaults.
//
// Three keys, and the record is what says so rather than a list written again here. One is a row. The other
// two have targets of their own: one is read as a type assertion rather than a guarded cast, so a row would
// predict the wrong resolution, and one is read by the upstream server into its own configuration and not by
// this package at all.
//
// All three are declared, because a section name is the whole of what a registration owns and a dotted one
// is refused, so a key under genesis is this registration's or nobody's.
func TestGenesisDeclaresAndResolves(t *testing.T) {
	want := manifestKeys(genesisKeys)
	for _, key := range genesisKeysWithTargetsOfTheirOwn {
		want = append(want, string(key))
	}
	sort.Strings(want)
	requireDeclares(t, GenesisSectionName, want)

	for _, mode := range registry.Modes() {
		requireResolves(t, mode, GenesisSectionName, map[string]any{
			flagGenesisStreamImport:       DefaultGenesisConfig.StreamGenesisImport,
			flagGenesisImportFile:         DefaultGenesisConfig.GenesisStreamFile,
			"genesis.genesis-stream-file": srvconfig.DefaultConfig().Genesis.GenesisStreamFile,
		})
	}
}

// TestStateStoreDeclaresEveryKeyItsReaderResolves holds the schema against the read-site record.
func TestStateStoreDeclaresEveryKeyItsReaderResolves(t *testing.T) {
	requireDeclares(t, StateStoreSectionName, manifestKeys(ssKeys))
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

// TestStateCommitDeclaresEveryKeyItsReaderResolves holds the schema against the read-site record.
//
// Twenty-three keys: the twenty the record holds as rows, and three it names beside them because each has
// a target of its own. The four keys under this section's flat key-value name that only the Cosmos server's
// reader resolves are not among them, and are not this section's to declare.
func TestStateCommitDeclaresEveryKeyItsReaderResolves(t *testing.T) {
	requireDeclares(t, StateCommitSectionName, manifestKeys(scKeys,
		FlagSCWriteMode, FlagSCWriteModeEnableAuto, FlagSCHashLoggerTargetFileSize))
}

// TestStateCommitResolvesTheModuleDeclaredValues covers the value side of the same registration.
//
// The write mode is a plain string here because the reader parses a written name into its own type, and
// comparing values is what holds it to that: the named type carries the same text and is not the same
// value.
func TestStateCommitResolvesTheModuleDeclaredValues(t *testing.T) {
	live := config.DefaultStateCommitConfig()
	for _, mode := range registry.Modes() {
		requireResolves(t, mode, StateCommitSectionName, map[string]any{
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

// TestTheseDeparturesFromWhatTheCommandWritesAreTheRecordedOnes measures every one of them.
//
// A declared value is what the seid init command writes for a kind of node. This section departs from that
// wherever the mode rules moved a value, and for one cause: the type the command renders declares a state
// store field of its own and fills it from the mode-blind default, so every rule this section applies is
// applied and then thrown away.
//
// PLT-955 records the defect and the decision to correct it in the versioned declaration rather than at the
// point that loses it. So the departures are intended, and the set is held rather than described. A row that
// stops departing fails, which is the day that row should be deleted. A row that starts departing fails too,
// so a rule added to this section has to account for what the command does with it.
//
// What this cannot see is the command. This package cannot import the one that renders the file, because
// that direction is the cycle, so the mode-blind side here is read from the same default the command reads
// rather than from the command's output. A change that made the command keep the rules would leave this
// green, and the test under cmd/seid/cmd is the half that fails then.
func TestTheseDeparturesFromWhatTheCommandWritesAreTheRecordedOnes(t *testing.T) {
	// The mode-blind values the command's own field carries, which is what reaches a generated file.
	blind := config.DefaultStateStoreConfig()

	// Every departure, by key and mode, with what each side says.
	recorded := map[string]map[registry.Mode][2]any{
		FlagSSKeepRecent: {
			registry.ModeArchive: {0, blind.KeepRecent},
		},
		FlagSSEnable: {
			registry.ModeValidator: {false, blind.Enable},
			registry.ModeSeed:      {false, blind.Enable},
		},
	}

	measured := map[string]map[registry.Mode]bool{}
	for _, mode := range registry.Modes() {
		resolved, err := registry.Resolve(mode, registry.Sources{})
		if err != nil {
			t.Fatalf("Resolve(%s): %v", mode, err)
		}
		for _, key := range []string{FlagSSKeepRecent, FlagSSEnable} {
			declared := resolved.Values[key]
			written := blindValueFor(key, blind)
			if declared == written {
				if _, listed := recorded[key][mode]; listed {
					t.Errorf("%s for %s no longer departs, both sides being %v. Take the row off, so "+
						"the record stays the set of values this section states differently from the "+
						"file the command writes", key, mode, declared)
				}
				continue
			}
			if measured[key] == nil {
				measured[key] = map[registry.Mode]bool{}
			}
			measured[key][mode] = true

			want, listed := recorded[key][mode]
			switch {
			case !listed:
				t.Errorf("%s for %s is declared as %v and the command writes %v, and nothing records "+
					"that", key, mode, declared, written)
			case declared != want[0] || written != want[1]:
				t.Errorf("%s for %s is recorded as declaring %v against a written %v, and declares %v "+
					"against %v", key, mode, want[0], want[1], declared, written)
			}
		}
	}

	for key, modes := range recorded {
		if len(measured[key]) != len(modes) {
			t.Errorf("%s is recorded as departing for %d modes and departs for %d", key,
				len(modes), len(measured[key]))
		}
	}
}

// blindValueFor returns the value the command's own state store field carries for a key.
func blindValueFor(key string, blind config.StateStoreConfig) any {
	switch key {
	case FlagSSKeepRecent:
		return blind.KeepRecent
	case FlagSSEnable:
		return blind.Enable
	}
	return nil
}

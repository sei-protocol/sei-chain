package app

import (
	"fmt"
	"sort"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// whatANodeRunsToday is what each diverging key resolves to for a file carrying no keys at all.
//
// Every entry is a read that takes no account of whether the key was present, or a value another key
// transforms afterwards. Held separately from the modes because the reader takes no mode: it produces one
// answer, and which modes disagree with it depends on what the section declares.
var whatANodeRunsToday = map[string]string{
	FlagSSEnable:            "false",
	FlagSSBackend:           "",
	FlagSSAsyncWriterBuffer: "0",
	FlagSSKeepRecent:        "0",
	FlagSSPruneInterval:     "0",
	FlagSSImportNumWorkers:  "0",
	FlagSCEnable:            "false",
	FlagSCWriteMode:         "auto",
}

// whyItMatters says what a node gets today, for the keys where that is worth stating.
var whyItMatters = map[string]string{
	FlagSSPruneInterval: "pruning is off, in the store and in the write-ahead log, so installing the " +
		"declared value starts deleting what the node was retaining",
	FlagSSKeepRecent: "every version is kept, so for an archive node what is declared and what runs " +
		"agree about keeping history and for the others they do not",
	FlagSCEnable: "state commitment reads as disabled, and a node started that way stops, which is why " +
		"no running node has this key missing",
	FlagSCWriteMode: "another key transforms this one after it is read, so the mode a node commits " +
		"through is derived rather than carried by this key",
}

// theDivergences is which keys disagree with the reader, per mode.
//
// Per mode because the section answers per mode for two of these settings and the reader does not answer
// per mode at all. An archive node declares the retention the reader also produces, so that key agrees for
// archive and disagrees everywhere else; the store toggle is the reverse.
var theDivergences = map[registry.Mode][]string{
	registry.ModeValidator: {FlagSSBackend, FlagSSAsyncWriterBuffer, FlagSSKeepRecent,
		FlagSSPruneInterval, FlagSSImportNumWorkers, FlagSCEnable, FlagSCWriteMode},
	registry.ModeSeed: {FlagSSBackend, FlagSSAsyncWriterBuffer, FlagSSKeepRecent,
		FlagSSPruneInterval, FlagSSImportNumWorkers, FlagSCEnable, FlagSCWriteMode},
	registry.ModeFull: {FlagSSEnable, FlagSSBackend, FlagSSAsyncWriterBuffer, FlagSSKeepRecent,
		FlagSSPruneInterval, FlagSSImportNumWorkers, FlagSCEnable, FlagSCWriteMode},
	registry.ModeArchive: {FlagSSEnable, FlagSSBackend, FlagSSAsyncWriterBuffer,
		FlagSSPruneInterval, FlagSSImportNumWorkers, FlagSCEnable, FlagSCWriteMode},
}

// readerValues is what each section's reader produces for a file carrying no keys at all.
//
// Written as a map from key to the field that key fills, because that pairing is what the comparison
// needs and neither the reader nor the section states it: the reader takes a key and assigns a field, and
// the section declares a key and a value.
func readerValues(t *testing.T) map[string]string {
	t.Helper()
	ss := parseSSConfigs(configtest.AppOpts{})
	sc := parseSCConfigs(configtest.AppOpts{})
	return map[string]string{
		FlagSSEnable:                     fmt.Sprint(ss.Enable),
		FlagSSDirectory:                  fmt.Sprint(ss.DBDirectory),
		FlagSSBackend:                    fmt.Sprint(ss.Backend),
		FlagSSAsyncWriterBuffer:          fmt.Sprint(ss.AsyncWriteBuffer),
		FlagSSKeepRecent:                 fmt.Sprint(ss.KeepRecent),
		FlagSSPruneInterval:              fmt.Sprint(ss.PruneIntervalSeconds),
		FlagSSImportNumWorkers:           fmt.Sprint(ss.ImportNumWorkers),
		FlagSSReadWriteMetrics:           fmt.Sprint(ss.EnableReadWriteMetrics),
		FlagSSSnapshotEnable:             fmt.Sprint(ss.SnapshotEnable),
		FlagEVMSSDirectory:               fmt.Sprint(ss.EVMDBDirectory),
		FlagEVMSSSeparateDBs:             fmt.Sprint(ss.SeparateEVMSubDBs),
		FlagEVMSSSplit:                   fmt.Sprint(ss.EVMSplit),
		FlagSCEnable:                     fmt.Sprint(sc.Enable),
		FlagSCDirectory:                  fmt.Sprint(sc.Directory),
		FlagSCAsyncCommitBuffer:          fmt.Sprint(sc.MemIAVLConfig.AsyncCommitBuffer),
		FlagSCSnapshotKeepRecent:         fmt.Sprint(sc.MemIAVLConfig.SnapshotKeepRecent),
		FlagSCSnapshotInterval:           fmt.Sprint(sc.MemIAVLConfig.SnapshotInterval),
		FlagSCSnapshotMinTimeInterval:    fmt.Sprint(sc.MemIAVLConfig.SnapshotMinTimeInterval),
		FlagSCSnapshotWriterLimit:        fmt.Sprint(sc.MemIAVLConfig.SnapshotWriterLimit),
		FlagSCSnapshotPrefetchThreshold:  fmt.Sprint(sc.MemIAVLConfig.SnapshotPrefetchThreshold),
		FlagSCSnapshotWriteRateMBps:      fmt.Sprint(sc.MemIAVLConfig.SnapshotWriteRateMBps),
		FlagSCHistoricalProofMaxInFlight: fmt.Sprint(sc.HistoricalProofMaxInFlight),
		FlagSCHistoricalProofRateLimit:   fmt.Sprint(sc.HistoricalProofRateLimit),
		FlagSCHistoricalProofBurst:       fmt.Sprint(sc.HistoricalProofBurst),
		FlagSCWriteMode:                  fmt.Sprint(sc.WriteMode),
		FlagSCWriteModeEnableAuto:        fmt.Sprint(sc.WriteModeEnableAuto),
		FlagSCKeysToMigratePerBlock:      fmt.Sprint(sc.KeysToMigratePerBlock),
		FlagSCHashLoggerEnable:           fmt.Sprint(sc.HashLogger.Enable),
		FlagSCHashLoggerDirectory:        fmt.Sprint(sc.HashLogger.Directory),
		FlagSCHashLoggerBlocksToRetain:   fmt.Sprint(sc.HashLogger.BlocksToRetain),
		FlagSCHashLoggerTargetFileSize:   fmt.Sprint(sc.HashLogger.TargetFileSize),
		FlagSCHashLoggerMaxDiskSize:      fmt.Sprint(sc.HashLogger.MaxDiskSize),
		FlagSCFlatKVReadWriteMetrics:     fmt.Sprint(sc.FlatKVConfig.EnableReadWriteMetrics),
	}
}

// TestTheDivergencesFromTheReaderAreTheRecordedOnes measures what the doc comments describe.
//
// The two storage sections declare defaults their readers do not produce for a file missing the keys,
// because most of those reads take no account of whether the key was present. Prose describing which keys
// those are cannot fail when it is wrong, and it was: it named four of the six store settings and one
// commitment setting that does not in fact differ, and missed the setting that selects how a node commits.
//
// So the set is measured here rather than described. A key that starts diverging fails this test, and so
// does one that stops: guarding a read means deleting its row, which is what makes the reconciliation
// something a change has to account for rather than something a comment claims.
func TestTheDivergencesFromTheReaderAreTheRecordedOnes(t *testing.T) {
	reader := readerValues(t)
	for _, mode := range registry.Modes() {
		resolved, err := registry.Resolve(mode, registry.Sources{})
		if err != nil {
			t.Fatalf("mode %q: %v", mode, err)
		}
		recorded, named := theDivergences[mode]
		if !named {
			t.Fatalf("mode %q has no record here, so a mode was added and this was not revisited", mode)
		}
		listed := make(map[string]bool, len(recorded))
		for _, key := range recorded {
			listed[key] = true
		}

		var measured []string
		for key, got := range reader {
			declared, declares := resolved.Values[key]
			if !declares {
				t.Errorf("mode %q: %s is read by this package and no section declares it", mode, key)
				continue
			}
			if fmt.Sprint(declared) == got {
				if listed[key] {
					t.Errorf("mode %q: %s no longer diverges, both sides being %v. Take it off that "+
						"mode's list, so the list stays the set of keys installing this section changes",
						mode, key, declared)
				}
				continue
			}
			measured = append(measured, key)
			if !listed[key] {
				t.Errorf("mode %q: %s declares %v and its reader produces %q for a file with no keys, and "+
					"nothing records that. Installing this section changes what such a node runs. %s",
					mode, key, declared, got, whyItMatters[key])
			}
			if want, stated := whatANodeRunsToday[key]; stated && want != got {
				t.Errorf("mode %q: %s is recorded as producing %q and produces %q", mode, key, want, got)
			}
		}

		sort.Strings(measured)
		if len(measured) != len(recorded) {
			t.Errorf("mode %q: measured %d divergences and %d are recorded: %v",
				mode, len(measured), len(recorded), measured)
		}
	}
}

// TestEveryKeyThisPackageDeclaresIsOneItsReadersFill holds the two lists against each other.
//
// The declared keys come from the schemas and the read keys from the map above, so a key on one side only
// is either a setting an operator writes that no reader fills, or one this package reads and nothing
// declares.
func TestEveryKeyThisPackageDeclaresIsOneItsReadersFill(t *testing.T) {
	reader := readerValues(t)
	for _, section := range []string{StateStoreSectionName, StateCommitSectionName} {
		registered, ok := registry.Lookup(section)
		if !ok {
			t.Fatalf("%s is not registered", section)
		}
		for _, key := range registered.Keys {
			if _, filled := reader[key]; !filled {
				t.Errorf("%s declares %s and no field above is paired with it", section, key)
			}
		}
	}
}

package app

import (
	"fmt"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

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
		FlagSCSubspaceQueryMaxInFlight:   fmt.Sprint(sc.SubspaceQueryMaxInFlight),
		FlagSCSubspaceMaxPairs:           fmt.Sprint(sc.SubspaceMaxPairs),
		FlagSCSubspaceMaxBytes:           fmt.Sprint(sc.SubspaceMaxBytes),
		FlagSCWriteMode:                  fmt.Sprint(sc.WriteMode),
		FlagSCWriteModeEnableAuto:        fmt.Sprint(sc.WriteModeEnableAuto),
		FlagSCHashLoggerEnable:           fmt.Sprint(sc.HashLogger.Enable),
		FlagSCHashLoggerDirectory:        fmt.Sprint(sc.HashLogger.Directory),
		FlagSCHashLoggerBlocksToRetain:   fmt.Sprint(sc.HashLogger.BlocksToRetain),
		FlagSCHashLoggerTargetFileSize:   fmt.Sprint(sc.HashLogger.TargetFileSize),
		FlagSCHashLoggerMaxDiskSize:      fmt.Sprint(sc.HashLogger.MaxDiskSize),
		FlagSCFlatKVReadWriteMetrics:     fmt.Sprint(sc.FlatKVConfig.EnableReadWriteMetrics),
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

package app

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/stretchr/testify/assert"
)

type TestSeiDBAppOpts struct {
}

func (t TestSeiDBAppOpts) Get(s string) interface{} {
	defaultSCConfig := config.DefaultStateCommitConfig()
	defaultSSConfig := config.DefaultStateStoreConfig()
	switch s {
	case FlagSCEnable:
		return defaultSCConfig.Enable
	case FlagSCAsyncCommitBuffer:
		return defaultSCConfig.MemIAVLConfig.AsyncCommitBuffer
	case FlagSCDirectory:
		return defaultSCConfig.Directory
	case FlagSCSnapshotInterval:
		return defaultSCConfig.MemIAVLConfig.SnapshotInterval
	case FlagSCSnapshotKeepRecent:
		return defaultSCConfig.MemIAVLConfig.SnapshotKeepRecent
	case FlagSCSnapshotMinTimeInterval:
		return defaultSCConfig.MemIAVLConfig.SnapshotMinTimeInterval
	case FlagSCSnapshotWriterLimit:
		return defaultSCConfig.MemIAVLConfig.SnapshotWriterLimit
	case FlagSCSnapshotPrefetchThreshold:
		return defaultSCConfig.MemIAVLConfig.SnapshotPrefetchThreshold
	case FlagSCSnapshotWriteRateMBps:
		return defaultSCConfig.MemIAVLConfig.SnapshotWriteRateMBps
	case FlagSSEnable:
		return defaultSSConfig.Enable
	case FlagSSBackend:
		return defaultSSConfig.Backend
	case FlagSSAsyncWriterBuffer:
		return defaultSSConfig.AsyncWriteBuffer
	case FlagSSDirectory:
		return defaultSSConfig.DBDirectory
	case FlagSSKeepRecent:
		return defaultSSConfig.KeepRecent
	case FlagSSPruneInterval:
		return defaultSSConfig.PruneIntervalSeconds
	case FlagSSImportNumWorkers:
		return defaultSSConfig.ImportNumWorkers
	case FlagEVMSSDirectory:
		return defaultSSConfig.EVMDBDirectory
	case FlagEVMSSWriteMode:
		return "" // empty means use default
	case FlagEVMSSReadMode:
		return "" // empty means use default
	}
	return nil
}

func TestNewDefaultConfig(t *testing.T) {
	// Make sure when adding a new default config, it should apply to SeiDB during initialization
	appOpts := TestSeiDBAppOpts{}
	scConfig := parseSCConfigs(appOpts)
	ssConfig := parseSSConfigs(appOpts)
	assert.Equal(t, scConfig, config.DefaultStateCommitConfig())
	assert.Equal(t, ssConfig, config.DefaultStateStoreConfig())
}

type mapAppOpts map[string]interface{}

func (m mapAppOpts) Get(s string) interface{} {
	return m[s]
}

func TestParseSCConfigs_HistoricalProofFlags(t *testing.T) {
	appOpts := mapAppOpts{
		FlagSCEnable: true,

		FlagSCHistoricalProofMaxInFlight: 7,
		FlagSCHistoricalProofRateLimit:   12.5,
		FlagSCHistoricalProofBurst:       3,
	}

	scConfig := parseSCConfigs(appOpts)
	assert.Equal(t, 7, scConfig.HistoricalProofMaxInFlight)
	assert.Equal(t, 12.5, scConfig.HistoricalProofRateLimit)
	assert.Equal(t, 3, scConfig.HistoricalProofBurst)
}

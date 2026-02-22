package app

import (
	"testing"

	"github.com/sei-protocol/sei-db/config"
	"github.com/stretchr/testify/assert"
)

type TestSeiDBAppOpts struct {
}

func (t TestSeiDBAppOpts) Get(s string) interface{} {
	switch s {
	case FlagSCEnable:
		return config.DefaultStateCommitConfig().Enable
	case FlagSCAsyncCommitBuffer:
		return config.DefaultStateCommitConfig().AsyncCommitBuffer
	case FlagSCDirectory:
		return config.DefaultStateCommitConfig().Directory
	case FlagSCCacheSize:
		return config.DefaultStateCommitConfig().CacheSize
	case FlagSCSnapshotInterval:
		return config.DefaultStateCommitConfig().SnapshotInterval
	case FlagSCSnapshotKeepRecent:
		return config.DefaultStateCommitConfig().SnapshotKeepRecent
	case FlagSCSnapshotMinTimeInterval:
		return config.DefaultStateCommitConfig().SnapshotMinTimeInterval
	case FlagSCSnapshotWriterLimit:
		return config.DefaultStateCommitConfig().SnapshotWriterLimit
	case FlagSCSnapshotPrefetchThreshold:
		return config.DefaultStateCommitConfig().SnapshotPrefetchThreshold
	case FlagSCSnapshotWriteRateMBps:
		return config.DefaultStateCommitConfig().SnapshotWriteRateMBps
	case FlagSSEnable:
		return config.DefaultStateStoreConfig().Enable
	case FlagSSBackend:
		return config.DefaultStateStoreConfig().Backend
	case FlagSSAsyncWriterBuffer:
		return config.DefaultStateStoreConfig().AsyncWriteBuffer
	case FlagSSDirectory:
		return config.DefaultStateStoreConfig().DBDirectory
	case FlagSSKeepRecent:
		return config.DefaultStateStoreConfig().KeepRecent
	case FlagSSPruneInterval:
		return config.DefaultStateStoreConfig().PruneIntervalSeconds
	case FlagSSImportNumWorkers:
		return config.DefaultStateStoreConfig().ImportNumWorkers
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

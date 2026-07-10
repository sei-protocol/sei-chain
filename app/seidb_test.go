package app

import (
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type TestSeiDBAppOpts struct {
}

func (t TestSeiDBAppOpts) Get(s string) interface{} {
	defaultSCConfig := config.DefaultStateCommitConfig()
	defaultSSConfig := config.DefaultStateStoreConfig()
	defaultReceiptConfig := config.DefaultReceiptStoreConfig()
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
	case FlagSCFlatKVReadWriteMetrics:
		return defaultSCConfig.FlatKVConfig.EnableReadWriteMetrics
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
	case FlagSSReadWriteMetrics:
		return defaultSSConfig.EnableReadWriteMetrics
	case receiptStoreBackendKey:
		return defaultReceiptConfig.Backend
	case receiptStoreReadWriteMetricsKey:
		return defaultReceiptConfig.EnableReadWriteMetrics
	case FlagEVMSSDirectory:
		return defaultSSConfig.EVMDBDirectory
	case FlagEVMSSSplit:
		return defaultSSConfig.EVMSplit
	case FlagEVMSSSeparateDBs:
		return defaultSSConfig.SeparateEVMSubDBs
	}
	return nil
}

func TestNewDefaultConfig(t *testing.T) {
	// Make sure when adding a new default config, it should apply to SeiDB during initialization
	appOpts := TestSeiDBAppOpts{}
	scConfig := parseSCConfigs(appOpts)
	ssConfig := parseSSConfigs(appOpts)
	receiptConfig, err := config.ReadReceiptConfig(appOpts)
	assert.NoError(t, err)
	// WriteModeEnableAuto defaults to true, so parseSCConfigs resolves the effective
	// WriteMode to auto, overriding the fixed-fallback default (memiavl_only).
	expectedSC := config.DefaultStateCommitConfig()
	expectedSC.WriteMode = sctypes.Auto
	// FlatKV snapshot cadence mirrors the memIAVL (SC) snapshot settings rather
	// than using FlatKV's own defaults.
	expectedSC.FlatKVConfig.SnapshotInterval = expectedSC.MemIAVLConfig.SnapshotInterval
	expectedSC.FlatKVConfig.SnapshotKeepRecent = expectedSC.MemIAVLConfig.SnapshotKeepRecent
	assert.Equal(t, expectedSC, scConfig)
	assert.Equal(t, ssConfig, config.DefaultStateStoreConfig())
	assert.Equal(t, receiptConfig, config.DefaultReceiptStoreConfig())
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

func TestParseSCConfigs_FlatKVReadWriteMetrics(t *testing.T) {
	scConfig := parseSCConfigs(mapAppOpts{
		FlagSCEnable:                 true,
		FlagSCFlatKVReadWriteMetrics: true,
	})

	assert.True(t, scConfig.FlatKVConfig.EnableReadWriteMetrics)
}

func TestParseSCConfigs_FlatKVSnapshotMirrorsMemIAVL(t *testing.T) {
	scConfig := parseSCConfigs(mapAppOpts{
		FlagSCEnable:             true,
		FlagSCSnapshotInterval:   uint32(5000),
		FlagSCSnapshotKeepRecent: uint32(3),
	})

	assert.Equal(t, uint32(5000), scConfig.MemIAVLConfig.SnapshotInterval)
	assert.Equal(t, uint32(3), scConfig.MemIAVLConfig.SnapshotKeepRecent)
	assert.Equal(t, scConfig.MemIAVLConfig.SnapshotInterval, scConfig.FlatKVConfig.SnapshotInterval)
	assert.Equal(t, scConfig.MemIAVLConfig.SnapshotKeepRecent, scConfig.FlatKVConfig.SnapshotKeepRecent)
}

func TestParseSCConfigs_SnapshotKeepRecentClampedToMin(t *testing.T) {
	scConfig := parseSCConfigs(mapAppOpts{
		FlagSCEnable:             true,
		FlagSCSnapshotKeepRecent: uint32(0),
	})

	// A configured 0 (keep only the current snapshot) is floored to 1 here in
	// app config parsing, and FlatKV mirrors the clamped value.
	assert.Equal(t, uint32(1), scConfig.MemIAVLConfig.SnapshotKeepRecent)
	assert.Equal(t, uint32(1), scConfig.FlatKVConfig.SnapshotKeepRecent)
}

func TestParseSCConfigs_LegacyCosmosOnlyWriteMode(t *testing.T) {
	scConfig := parseSCConfigs(mapAppOpts{
		FlagSCEnable:    true,
		FlagSCWriteMode: "cosmos_only",
	})
	assert.Equal(t, sctypes.Auto, scConfig.WriteMode)

	scConfig = parseSCConfigs(mapAppOpts{
		FlagSCEnable:              true,
		FlagSCWriteMode:           "cosmos_only",
		FlagSCWriteModeEnableAuto: false,
	})
	assert.Equal(t, sctypes.MemiavlOnly, scConfig.WriteMode)
}

func TestParseSCConfigs_InvalidWriteModePanicMentionsSC(t *testing.T) {
	assert.PanicsWithValue(t, `invalid SC write mode "bogus": invalid write mode: bogus`, func() {
		parseSCConfigs(mapAppOpts{
			FlagSCEnable:    true,
			FlagSCWriteMode: "bogus",
		})
	})
}

func TestParseSSConfigs_EVMFlags(t *testing.T) {
	appOpts := mapAppOpts{
		FlagSSEnable:            true,
		FlagEVMSSDirectory:      "/tmp/evm-ss",
		FlagEVMSSSplit:          true,
		FlagEVMSSSeparateDBs:    true,
		FlagSSAsyncWriterBuffer: 0,
	}

	ssConfig := parseSSConfigs(appOpts)
	assert.True(t, ssConfig.Enable)
	assert.Equal(t, "/tmp/evm-ss", ssConfig.EVMDBDirectory)
	assert.True(t, ssConfig.EVMSplit)
	assert.True(t, ssConfig.SeparateEVMSubDBs)
}

func TestParseSSConfigs_ReadWriteMetrics(t *testing.T) {
	ssConfig := parseSSConfigs(mapAppOpts{
		FlagSSEnable:           true,
		FlagSSReadWriteMetrics: true,
	})

	assert.True(t, ssConfig.EnableReadWriteMetrics)
}

func TestParseReceiptConfigs_DefaultsToPebbleWhenUnset(t *testing.T) {
	receiptConfig, err := config.ReadReceiptConfig(mapAppOpts{})
	assert.NoError(t, err)
	assert.Equal(t, config.DefaultReceiptStoreConfig(), receiptConfig)
}

func TestParseReceiptConfigs_UsesConfiguredBackend(t *testing.T) {
	receiptConfig, err := config.ReadReceiptConfig(mapAppOpts{
		receiptStoreBackendKey: "pebbledb",
	})
	assert.NoError(t, err)
	assert.Equal(t, "pebbledb", receiptConfig.Backend)
	assert.Equal(t, config.DefaultReceiptStoreConfig().AsyncWriteBuffer, receiptConfig.AsyncWriteBuffer)
	assert.Equal(t, 0, receiptConfig.KeepRecent)
}

func TestParseReceiptConfigs_UsesConfiguredValues(t *testing.T) {
	receiptConfig, err := config.ReadReceiptConfig(mapAppOpts{
		receiptStoreDBDirectoryKey:          "/tmp/custom-receipt-db",
		receiptStoreBackendKey:              "pebbledb",
		receiptStoreAsyncWriteBufferKey:     7,
		receiptStorePruneIntervalSecondsKey: 9,
		receiptStoreReadWriteMetricsKey:     true,
	})
	assert.NoError(t, err)
	assert.Equal(t, "/tmp/custom-receipt-db", receiptConfig.DBDirectory)
	assert.Equal(t, "pebbledb", receiptConfig.Backend)
	assert.Equal(t, 7, receiptConfig.AsyncWriteBuffer)
	assert.Equal(t, 0, receiptConfig.KeepRecent)
	assert.Equal(t, 9, receiptConfig.PruneIntervalSeconds)
	assert.True(t, receiptConfig.EnableReadWriteMetrics)
}

func TestParseReceiptConfigs_RejectsInvalidBackend(t *testing.T) {
	_, err := config.ReadReceiptConfig(mapAppOpts{
		receiptStoreBackendKey: "rocksdb",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported receipt-store backend")
	assert.Contains(t, err.Error(), "rocksdb")
}

func TestReadReceiptStoreConfigUsesMinRetainBlocks(t *testing.T) {
	homePath := t.TempDir()
	receiptConfig, err := readReceiptStoreConfig(homePath, mapAppOpts{
		receiptStoreDBDirectoryKey: "/tmp/custom-receipt-db",
		server.FlagMinRetainBlocks: 200000,
	})
	require.NoError(t, err)
	assert.Equal(t, "/tmp/custom-receipt-db", receiptConfig.DBDirectory)
	assert.Equal(t, 200000, receiptConfig.KeepRecent)
}

func TestReadReceiptStoreConfigUsesDefaultDirectoryWhenUnset(t *testing.T) {
	homePath := t.TempDir()
	receiptConfig, err := readReceiptStoreConfig(homePath, mapAppOpts{})
	require.NoError(t, err)
	// New nodes (no legacy data/receipt.db) get the new ledger/ layout with backend
	assert.Equal(t, filepath.Join(homePath, "data", "ledger", "receipt", "pebbledb"), receiptConfig.DBDirectory)
}

func TestReadReceiptStoreConfigFallsBackToMinRetainBlocks(t *testing.T) {
	homePath := t.TempDir()
	receiptConfig, err := readReceiptStoreConfig(homePath, mapAppOpts{
		server.FlagMinRetainBlocks: 500000,
	})
	require.NoError(t, err)
	assert.Equal(t, 500000, receiptConfig.KeepRecent)
}

func TestReadReceiptStoreConfigFallsBackToZeroWhenNeitherSet(t *testing.T) {
	homePath := t.TempDir()
	receiptConfig, err := readReceiptStoreConfig(homePath, mapAppOpts{})
	require.NoError(t, err)
	assert.Equal(t, 0, receiptConfig.KeepRecent)
}

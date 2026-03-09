package app

import (
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	case "receipt-store.rs-backend":
		return config.DefaultReceiptStoreConfig().Backend
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
	receiptConfig, err := config.ReadReceiptConfig(appOpts)
	assert.NoError(t, err)
	assert.Equal(t, scConfig, config.DefaultStateCommitConfig())
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

func TestParseReceiptConfigs_DefaultsToPebbleWhenUnset(t *testing.T) {
	receiptConfig, err := config.ReadReceiptConfig(mapAppOpts{})
	assert.NoError(t, err)
	assert.Equal(t, config.DefaultReceiptStoreConfig(), receiptConfig)
}

func TestParseReceiptConfigs_UsesConfiguredBackend(t *testing.T) {
	receiptConfig, err := config.ReadReceiptConfig(mapAppOpts{
		"receipt-store.rs-backend": "parquet",
	})
	assert.NoError(t, err)
	assert.Equal(t, "parquet", receiptConfig.Backend)
	assert.Equal(t, config.DefaultReceiptStoreConfig().AsyncWriteBuffer, receiptConfig.AsyncWriteBuffer)
	assert.Equal(t, config.DefaultReceiptStoreConfig().KeepRecent, receiptConfig.KeepRecent)
}

func TestParseReceiptConfigs_UsesConfiguredValues(t *testing.T) {
	receiptConfig, err := config.ReadReceiptConfig(mapAppOpts{
		"receipt-store.db-directory":           "/tmp/custom-receipt-db",
		"receipt-store.rs-backend":             "parquet",
		"receipt-store.async-write-buffer":     7,
		"receipt-store.keep-recent":            42,
		"receipt-store.prune-interval-seconds": 9,
	})
	assert.NoError(t, err)
	assert.Equal(t, "/tmp/custom-receipt-db", receiptConfig.DBDirectory)
	assert.Equal(t, "parquet", receiptConfig.Backend)
	assert.Equal(t, 7, receiptConfig.AsyncWriteBuffer)
	assert.Equal(t, 42, receiptConfig.KeepRecent)
	assert.Equal(t, 9, receiptConfig.PruneIntervalSeconds)
}

func TestParseReceiptConfigs_RejectsInvalidBackend(t *testing.T) {
	_, err := config.ReadReceiptConfig(mapAppOpts{
		"receipt-store.rs-backend": "rocksdb",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported receipt-store backend")
	assert.Contains(t, err.Error(), "rocksdb")
}

func TestReadReceiptStoreConfigUsesConfiguredValues(t *testing.T) {
	homePath := t.TempDir()
	receiptConfig, err := readReceiptStoreConfig(homePath, mapAppOpts{
		"receipt-store.db-directory": "/tmp/custom-receipt-db",
		"receipt-store.keep-recent":  5,
		server.FlagMinRetainBlocks:   100,
	})
	require.NoError(t, err)
	assert.Equal(t, "/tmp/custom-receipt-db", receiptConfig.DBDirectory)
	assert.Equal(t, 5, receiptConfig.KeepRecent)
}

func TestReadReceiptStoreConfigUsesDefaultDirectoryWhenUnset(t *testing.T) {
	homePath := t.TempDir()
	receiptConfig, err := readReceiptStoreConfig(homePath, mapAppOpts{})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(homePath, "data", "receipt.db"), receiptConfig.DBDirectory)
}

// TestFullAppPathWithParquetReceiptStore exercises the full app.New path with rs-backend = "parquet"
// and asserts the parquet receipt store is actually instantiated (not pebble).
func TestFullAppPathWithParquetReceiptStore(t *testing.T) {
	app := SetupWithScReceiptFromOpts(t, false, false, TestAppOpts{
		UseSc:          true,
		ReceiptBackend: "parquet",
	})
	require.NotNil(t, app.receiptStore, "receipt store should be created")
	assert.Equal(t, "parquet", receipt.BackendTypeName(app.receiptStore), "receipt store backend should be parquet")
}

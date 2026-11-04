package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
)

// validBaseConfig returns a Config that passes Validate() so tests can
// mutate a single field and check that specific validation error.
func validBaseConfig() *Config {
	cfg := DefaultConfig()
	cfg.DataDir = "/tmp/test"
	cfg.AccountDBConfig.DataDir = "/tmp/test/account"
	cfg.CodeDBConfig.DataDir = "/tmp/test/code"
	cfg.StorageDBConfig.DataDir = "/tmp/test/storage"
	cfg.MiscDBConfig.DataDir = "/tmp/test/misc"
	return cfg
}

func TestValidateEmptyDataDir(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DataDir = ""
	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "data dir is required")
}

func TestValidateNegativeReaderThreadsPerCore(t *testing.T) {
	cfg := validBaseConfig()
	cfg.ReaderThreadsPerCore = -1.0
	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "reader threads per core")
}

func TestValidateZeroReaderThreadsPerCore(t *testing.T) {
	cfg := validBaseConfig()
	cfg.ReaderThreadsPerCore = 0
	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "reader threads per core")
}

func TestValidateNegativeReaderConstantThreadCount(t *testing.T) {
	cfg := validBaseConfig()
	cfg.ReaderConstantThreadCount = -1
	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "reader constant thread count")
}

func TestValidateZeroReaderPoolQueueSizePasses(t *testing.T) {
	cfg := validBaseConfig()
	cfg.ReaderPoolQueueSize = 0
	err := cfg.Validate()
	require.NoError(t, err)
}

func TestValidateNegativeReaderPoolQueueSize(t *testing.T) {
	cfg := validBaseConfig()
	cfg.ReaderPoolQueueSize = -1
	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "reader pool queue size")
}

func TestValidateNegativeMiscPoolThreadsPerCore(t *testing.T) {
	cfg := validBaseConfig()
	cfg.MiscPoolThreadsPerCore = -1.0
	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "misc threads per core")
}

func TestValidateNegativeMiscConstantThreadCount(t *testing.T) {
	cfg := validBaseConfig()
	cfg.MiscConstantThreadCount = -1
	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "misc constant thread count")
}

func TestValidateNegativeSortThreadsPerCore(t *testing.T) {
	cfg := validBaseConfig()
	cfg.SortThreadsPerCore = -1.0
	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "sort threads per core")
}

func TestDefaultConfigValidExceptDataDir(t *testing.T) {
	cfg := DefaultConfig()
	err := cfg.Validate()
	require.Error(t, err)

	cfg.DataDir = "/tmp/test"
	cfg.AccountDBConfig.DataDir = "/tmp/test/account"
	cfg.CodeDBConfig.DataDir = "/tmp/test/code"
	cfg.StorageDBConfig.DataDir = "/tmp/test/storage"
	cfg.MiscDBConfig.DataDir = "/tmp/test/misc"
	require.NoError(t, cfg.Validate())
}

func TestConfigCopyDeep(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DataDir = "/original"
	cfg.SnapshotInterval = 100

	cp := cfg.Copy()
	cp.DataDir = "/mutated"
	cp.SnapshotInterval = 999

	require.Equal(t, "/original", cfg.DataDir, "original should be unchanged")
	require.Equal(t, uint32(100), cfg.SnapshotInterval, "original should be unchanged")
	require.Equal(t, "/mutated", cp.DataDir)
	require.Equal(t, uint32(999), cp.SnapshotInterval)
}

func TestValidateNestedPebbleDBConfigError(t *testing.T) {
	cfg := validBaseConfig()
	cfg.AccountDBConfig.EnableMetrics = true
	cfg.AccountDBConfig.MetricsScrapeInterval = 0

	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "account db config is invalid")
}

func TestValidateNestedEngineConfigError(t *testing.T) {
	cfg := validBaseConfig()
	cfg.StorageStoreConfig.MaxSize = 1024
	cfg.StorageStoreConfig.ShardCount = 3 // not a power of two

	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "storage store config is invalid")
	require.Contains(t, err.Error(), "ShardCount must be a power of two and greater than 0")
}

func TestApplyLowMemoryProfile(t *testing.T) {
	cfg := validBaseConfig()
	cfg.ApplyLowMemoryProfile()
	require.NoError(t, cfg.Validate())

	require.Equal(t, uint64(256*unit.MB), cfg.AccountStoreConfig.MaxSize)
	require.Equal(t, uint64(64*unit.MB), cfg.CodeStoreConfig.MaxSize)
	require.Equal(t, uint64(512*unit.MB), cfg.StorageStoreConfig.MaxSize)
	require.Equal(t, uint64(128*unit.MB), cfg.MiscStoreConfig.MaxSize)
	require.Equal(t, uint32(8), cfg.MaxSnapshotLagBlocks)
	require.Equal(t, 1.0, cfg.MiscPoolThreadsPerCore)

	stores := map[string]view.ViewManagerConfig{
		"account": cfg.AccountStoreConfig,
		"code":    cfg.CodeStoreConfig,
		"storage": cfg.StorageStoreConfig,
		"misc":    cfg.MiscStoreConfig,
	}
	dbs := map[string]pebbledb.PebbleDBConfig{
		"account": cfg.AccountDBConfig,
		"code":    cfg.CodeDBConfig,
		"storage": cfg.StorageDBConfig,
		"misc":    cfg.MiscDBConfig,
	}
	for name, store := range stores {
		db := dbs[name]
		require.Equal(t, uint64(8), store.MaxUnflushedVersions, name)
		require.Equal(t, int64(64*unit.MB), db.BlockCacheSize, name)
		require.Equal(t, uint64(32*unit.MB), db.MemTableSize, name)
		require.Equal(t, 4, db.MemTableStopWritesThreshold, name)
		// Pebble moves a batch larger than half a memtable onto its flushable-batch slow path.
		require.Greater(t, db.MemTableSize, 2*store.TargetBytesPerFlush, name)
	}
}

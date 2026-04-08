package flatkv

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// validBaseConfig returns a Config that passes Validate() so tests can
// mutate a single field and check that specific validation error.
func validBaseConfig() *Config {
	cfg := DefaultConfig()
	cfg.DataDir = "/tmp/test"
	cfg.InitializeDataDirectories()
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

func TestDefaultConfigValidExceptDataDir(t *testing.T) {
	cfg := DefaultConfig()
	err := cfg.Validate()
	require.Error(t, err)

	cfg.DataDir = "/tmp/test"
	cfg.InitializeDataDirectories()
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

func TestInitializeDataDirectories(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DataDir = "/base/flatkv"
	cfg.AccountDBConfig.DataDir = ""
	cfg.CodeDBConfig.DataDir = ""
	cfg.StorageDBConfig.DataDir = ""
	cfg.LegacyDBConfig.DataDir = ""
	cfg.MetadataDBConfig.DataDir = ""

	cfg.InitializeDataDirectories()

	require.Equal(t, "/base/flatkv/working/account", cfg.AccountDBConfig.DataDir)
	require.Equal(t, "/base/flatkv/working/code", cfg.CodeDBConfig.DataDir)
	require.Equal(t, "/base/flatkv/working/storage", cfg.StorageDBConfig.DataDir)
	require.Equal(t, "/base/flatkv/working/legacy", cfg.LegacyDBConfig.DataDir)
	require.Equal(t, "/base/flatkv/working/metadata", cfg.MetadataDBConfig.DataDir)
}

func TestInitializeDataDirectoriesPreservesExisting(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DataDir = "/base/flatkv"
	cfg.AccountDBConfig.DataDir = "/custom/account"

	cfg.InitializeDataDirectories()

	require.Equal(t, "/custom/account", cfg.AccountDBConfig.DataDir,
		"existing DataDir should not be overwritten")
	require.Equal(t, "/base/flatkv/working/code", cfg.CodeDBConfig.DataDir,
		"empty DataDir should be populated")
}

func TestValidateNestedPebbleDBConfigError(t *testing.T) {
	cfg := validBaseConfig()
	cfg.AccountDBConfig.EnableMetrics = true
	cfg.AccountDBConfig.MetricsScrapeInterval = 0

	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "account db config is invalid")
}

func TestValidateNestedCacheConfigError(t *testing.T) {
	cfg := validBaseConfig()
	cfg.StorageCacheConfig.MaxSize = 1024
	cfg.StorageCacheConfig.ShardCount = 3 // not a power of two

	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "storage cache config is invalid")
	require.Contains(t, err.Error(), "shard count must be a non-zero power of two")
}

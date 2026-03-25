package flatkv

import (
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/dbcache"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
)

func smallTestPebbleConfig() pebbledb.PebbleDBConfig {
	return pebbledb.PebbleDBConfig{
		EnableMetrics: false,
	}
}

func smallTestCacheConfig() dbcache.CacheConfig {
	return dbcache.CacheConfig{
		ShardCount: 8,
		MaxSize:    16 * unit.MB,
	}
}

// DefaultTestConfig returns a Config suitable for unit tests. It uses
// t.TempDir() as the DataDir root, small cache sizes, and disables metrics.
func DefaultTestConfig(t *testing.T) *Config {
	t.Helper()
	return &Config{
		DataDir:                filepath.Join(t.TempDir(), flatkvRootDir),
		SnapshotInterval:       DefaultSnapshotInterval,
		SnapshotKeepRecent:     DefaultSnapshotKeepRecent,
		AccountDBConfig:        smallTestPebbleConfig(),
		AccountCacheConfig:     smallTestCacheConfig(),
		CodeDBConfig:           smallTestPebbleConfig(),
		CodeCacheConfig:        smallTestCacheConfig(),
		StorageDBConfig:        smallTestPebbleConfig(),
		StorageCacheConfig:     smallTestCacheConfig(),
		LegacyDBConfig:         smallTestPebbleConfig(),
		LegacyCacheConfig:      smallTestCacheConfig(),
		MetadataDBConfig:       smallTestPebbleConfig(),
		MetadataCacheConfig:    smallTestCacheConfig(),
		ReaderThreadsPerCore:   2.0,
		ReaderPoolQueueSize:    1024,
		MiscPoolThreadsPerCore: 4.0,
	}
}

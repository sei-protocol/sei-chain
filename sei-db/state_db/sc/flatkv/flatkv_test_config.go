package flatkv

import (
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
)

func smallTestPebbleConfig() pebbledb.PebbleDBConfig {
	return pebbledb.PebbleDBConfig{
		CacheSize:       16 * unit.MB,
		CacheShardCount: 8,
		BlockCacheSize:  16 * unit.MB,
		EnableMetrics:   false,
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
		CodeDBConfig:           smallTestPebbleConfig(),
		StorageDBConfig:        smallTestPebbleConfig(),
		LegacyDBConfig:         smallTestPebbleConfig(),
		MetadataDBConfig:       smallTestPebbleConfig(),
		ReaderThreadsPerCore:   2.0,
		ReaderPoolQueueSize:    1024,
		MiscPoolThreadsPerCore: 4.0,
	}
}

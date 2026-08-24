package config

import (
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/snapshot"
)

func smallTestPebbleConfig() pebbledb.PebbleDBConfig {
	// Built from the default rather than as a literal, so a field added there does not silently arrive
	// here as a zero value.
	cfg := pebbledb.DefaultConfig()
	cfg.EnableMetrics = false
	cfg.BlockCacheSize = int64(8 * unit.MB)
	return cfg
}

func smallTestEngineConfig(name string) snapshot.SnapshotEngineConfig {
	cfg := defaultStoreConfig(name)
	cfg.MaxSize = 16 * unit.MB
	cfg.MetricsEnabled = false
	return cfg
}

// DefaultTestConfig returns a Config suitable for unit tests. It uses
// t.TempDir() as the DataDir root, small cache sizes, and disables metrics.
func DefaultTestConfig(t testing.TB) *Config {
	t.Helper()
	return &Config{
		DataDir:                filepath.Join(t.TempDir(), "flatkv"),
		SnapshotInterval:       DefaultSnapshotInterval,
		SnapshotKeepRecent:     DefaultSnapshotKeepRecent,
		HashQueueSize:          64,
		HashChanSize:           1024,
		AccountDBConfig:        smallTestPebbleConfig(),
		AccountStoreConfig:     smallTestEngineConfig("account"),
		CodeDBConfig:           smallTestPebbleConfig(),
		CodeStoreConfig:        smallTestEngineConfig("code"),
		StorageDBConfig:        smallTestPebbleConfig(),
		StorageStoreConfig:     smallTestEngineConfig("storage"),
		MiscDBConfig:           smallTestPebbleConfig(),
		MiscStoreConfig:        smallTestEngineConfig("misc"),
		MetadataDBConfig:       smallTestPebbleConfig(),
		MetadataStoreConfig:    smallTestEngineConfig("metadata"),
		ReaderThreadsPerCore:   2.0,
		ReaderPoolQueueSize:    1024,
		MiscPoolThreadsPerCore: 4.0,
		LtHashThreadsPerCore:   1.0,
	}
}

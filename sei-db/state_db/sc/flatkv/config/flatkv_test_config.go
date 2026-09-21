package config

import (
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

func smallTestPebbleConfig() pebbledb.PebbleDBConfig {
	// Built from the default rather than as a literal, so a field added there does not silently arrive
	// here as a zero value.
	cfg := pebbledb.DefaultConfig()
	cfg.EnableMetrics = false
	cfg.BlockCacheSize = int64(8 * unit.MB)
	return cfg
}

func smallTestViewManagerConfig(name string) view.ViewManagerConfig {
	cfg := defaultStoreConfig(name)
	cfg.MaxSize = 16 * unit.MB
	cfg.MetricsEnabled = false
	return cfg
}

// DefaultTestConfig returns a Config suitable for unit tests. It uses
// t.TempDir() as the DataDir root, small cache sizes, and disables metrics.
func DefaultTestConfig(t *testing.T) *Config {
	t.Helper()
	return &Config{
		DataDir:                filepath.Join(t.TempDir(), "flatkv"),
		SnapshotInterval:       10000,
		SnapshotKeepRecent:     1,
		AccountDBConfig:        smallTestPebbleConfig(),
		AccountStoreConfig:     smallTestViewManagerConfig("account"),
		CodeDBConfig:           smallTestPebbleConfig(),
		CodeStoreConfig:        smallTestViewManagerConfig("code"),
		StorageDBConfig:        smallTestPebbleConfig(),
		StorageStoreConfig:     smallTestViewManagerConfig("storage"),
		MiscDBConfig:           smallTestPebbleConfig(),
		MiscStoreConfig:        smallTestViewManagerConfig("misc"),
		ReaderThreadsPerCore:   2.0,
		ReaderPoolQueueSize:    1024,
		MiscPoolThreadsPerCore: 4.0,
		LtHashThreadsPerCore:   1.0,
		HashEngineConfig:       *lthash.DefaultConfig(),
		FinalizationQueueSize:  64,
	}
}

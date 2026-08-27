package config

import (
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
)

func smallTestPebbleConfig() pebbledb.PebbleDBConfig {
	return pebbledb.PebbleDBConfig{
		EnableMetrics: false,
	}
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
		SnapshotInterval:       DefaultSnapshotInterval,
		SnapshotKeepRecent:     DefaultSnapshotKeepRecent,
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
	}
}

package flatkv

import (
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
)

// DefaultTestConfig returns a Config suitable for unit tests. It uses
// t.TempDir() as the DataDir root, small cache sizes, and disables metrics.
func DefaultTestConfig(t *testing.T) *Config {
	t.Helper()
	return &Config{
		DataDir:                filepath.Join(t.TempDir(), flatkvRootDir),
		SnapshotInterval:       DefaultSnapshotInterval,
		SnapshotKeepRecent:     DefaultSnapshotKeepRecent,
		AccountDBConfig:        pebbledb.DefaultTestPebbleDBConfig(t),
		CodeDBConfig:           pebbledb.DefaultTestPebbleDBConfig(t),
		StorageDBConfig:        pebbledb.DefaultTestPebbleDBConfig(t),
		LegacyDBConfig:         pebbledb.DefaultTestPebbleDBConfig(t),
		MetadataDBConfig:       pebbledb.DefaultTestPebbleDBConfig(t),
		ReaderThreadsPerCore:   2.0,
		ReaderPoolQueueSize:    1024,
		MiscPoolThreadsPerCore: 4.0,
	}
}

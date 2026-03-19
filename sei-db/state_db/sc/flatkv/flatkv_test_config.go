package flatkv

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
)

func smallTestPebbleConfig() *pebbledb.PebbleDBConfig {
	return nil // TODO is this still needed?
}

// DefaultTestConfig returns a Config suitable for unit tests. It uses
// t.TempDir() as the DataDir root, small cache sizes, and disables metrics.
func DefaultTestConfig(t *testing.T) *Config {
	cfg := DefaultConfig()

	cfg.AccountDBConfig = pebbledb.DefaultTestPebbleDBConfig(t)
	cfg.CodeDBConfig = pebbledb.DefaultTestPebbleDBConfig(t)
	cfg.StorageDBConfig = pebbledb.DefaultTestPebbleDBConfig(t)
	cfg.LegacyDBConfig = pebbledb.DefaultTestPebbleDBConfig(t)
	cfg.MetadataDBConfig = pebbledb.DefaultTestPebbleDBConfig(t)

	return cfg
}

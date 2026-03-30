package pebbledb

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/dbcache"
)

// DefaultTestConfig returns a PebbleDBConfig suitable for testing.
// Allocates a smaller block cache and disables metrics.
func DefaultTestConfig(t *testing.T) PebbleDBConfig {
	cfg := DefaultConfig()
	cfg.DataDir = t.TempDir()
	cfg.EnableMetrics = false
	return cfg
}

// DefaultTestCacheConfig returns a CacheConfig suitable for testing.
func DefaultTestCacheConfig() dbcache.CacheConfig {
	return dbcache.CacheConfig{
		ShardCount: 8,
		MaxSize:    16 * unit.MB,
	}
}

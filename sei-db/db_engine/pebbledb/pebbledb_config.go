package pebbledb

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
)

// Configuration for the PebbleDB database.
type PebbleDBConfig struct {
	// The directory to store the database files. This has no default value and must be provided.
	DataDir string
	// The size of key-value cache, in bytes.
	CacheSize int
	// The number of shards in the key-value cache. Must be a power of two and greater than 0.
	CacheShardCount int
	// The size of pebbleDB's internal page cache, in bytes.
	PageCacheSize int
	// Whether to enable metrics.
	EnableMetrics bool
}

// Default configuration for the PebbleDB database.
func DefaultConfig() PebbleDBConfig {
	return PebbleDBConfig{
		CacheSize:       512 * unit.MB,
		CacheShardCount: 8,
		PageCacheSize:   512 * unit.MB,
		EnableMetrics:   true,
	}
}

// Validates the configuration (basic sanity checks).
func (c *PebbleDBConfig) Validate() error {
	if c.DataDir == "" {
		return fmt.Errorf("data dir is required")
	}
	if c.CacheShardCount <= 0 || (c.CacheShardCount&(c.CacheShardCount-1)) != 0 {
		return fmt.Errorf("cache shard count must be a power of two and greater than 0")
	}
	if c.CacheSize <= 0 {
		return fmt.Errorf("cache size must be greater than 0")
	}
	if c.PageCacheSize <= 0 {
		return fmt.Errorf("page cache size must be greater than 0")
	}
	return nil
}

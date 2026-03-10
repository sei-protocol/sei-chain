package pebbledb

import (
	"fmt"
	"time"

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
	// The size of pebbleDB's internal block cache, in bytes.
	BlockCacheSize int
	// Whether to enable metrics.
	EnableMetrics bool
	// How often to scrape metrics (pebble internals + cache size).
	MetricsScrapeInterval time.Duration
}

// Default configuration for the PebbleDB database.
func DefaultConfig() PebbleDBConfig {
	return PebbleDBConfig{
		CacheSize:             512 * unit.MB,
		CacheShardCount:       8,
		BlockCacheSize:        512 * unit.MB,
		EnableMetrics:         true,
		MetricsScrapeInterval: 10 * time.Second,
	}
}

// Validates the configuration (basic sanity checks).
func (c *PebbleDBConfig) Validate() error {
	if c.DataDir == "" {
		return fmt.Errorf("data dir is required")
	}
	if c.CacheSize < 0 {
		return fmt.Errorf("cache size must not be negative")
	}
	if c.CacheSize > 0 {
		if (c.CacheShardCount&(c.CacheShardCount-1)) != 0 {
			return fmt.Errorf("cache shard count must be a power of two or 0")
		}
	}
	if c.BlockCacheSize <= 0 {
		return fmt.Errorf("block cache size must be greater than 0")
	}
	if c.EnableMetrics && c.MetricsScrapeInterval <= 0 {
		return fmt.Errorf("metrics scrape interval must be positive when metrics are enabled")
	}
	return nil
}

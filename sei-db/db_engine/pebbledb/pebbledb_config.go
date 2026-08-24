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
	// Whether to enable pebble-internal metrics.
	EnableMetrics bool
	// Whether to emit simple estimated logical read/write counters.
	EnableReadWriteMetrics bool
	// How often to scrape pebble-internal metrics.
	MetricsScrapeInterval time.Duration

	// Size, in bytes, of pebble's block cache for this database.
	//
	// The block cache holds decompressed sstable blocks, so it absorbs the reads that miss the layers
	// above it. It is allocated outside the Go heap, which makes it the cheapest place to spend spare
	// memory on a dedicated machine: unlike an in-heap cache it adds no work for the garbage collector.
	//
	// Default: 512 MB
	BlockCacheSize int64 `mapstructure:"block-cache-size"`
}

// Default configuration for the PebbleDB database.
func DefaultConfig() PebbleDBConfig {
	return PebbleDBConfig{
		EnableMetrics:         true,
		MetricsScrapeInterval: 10 * time.Second,
		BlockCacheSize:        int64(512 * unit.MB),
	}
}

// Validates the configuration (basic sanity checks).
func (c *PebbleDBConfig) Validate() error {
	if c.DataDir == "" {
		return fmt.Errorf("data dir is required")
	}
	if c.EnableMetrics && c.MetricsScrapeInterval <= 0 {
		return fmt.Errorf("metrics scrape interval must be positive when metrics are enabled")
	}
	if c.BlockCacheSize <= 0 {
		return fmt.Errorf("block cache size must be positive, got %d", c.BlockCacheSize)
	}
	return nil
}

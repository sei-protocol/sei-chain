package pebbledb

import (
	"fmt"
	"runtime"
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

	// Upper bound on how many compactions pebble may run concurrently.
	//
	// Pebble's own default is 1, which cannot keep up with a sustained write load: L0 accumulates
	// sublevels faster than a single compaction drains them, and every point lookup then pays to
	// search all of them. This bound also gates pebble's debt-based escalation, which grants extra
	// compaction slots as compaction debt builds but never exceeds this value, so a bound of 1
	// disables that mechanism entirely.
	//
	// Default: a quarter of the machine's cores, at least 4.
	MaxConcurrentCompactions int `mapstructure:"max-concurrent-compactions"`
}

// Default configuration for the PebbleDB database.
func DefaultConfig() PebbleDBConfig {
	return PebbleDBConfig{
		EnableMetrics:            true,
		MetricsScrapeInterval:    10 * time.Second,
		BlockCacheSize:           int64(512 * unit.MB),
		MaxConcurrentCompactions: max(4, runtime.NumCPU()/4),
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
	if c.MaxConcurrentCompactions < 1 {
		return fmt.Errorf("max concurrent compactions must be at least 1, got %d", c.MaxConcurrentCompactions)
	}
	return nil
}

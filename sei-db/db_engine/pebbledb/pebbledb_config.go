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
	// CacheSize is the block cache capacity in bytes.
	CacheSize int64
	// MemTableSize is the size of each Pebble memtable in bytes.
	MemTableSize uint64
	// MemTableStopWritesThreshold is the number of memtables that trigger write backpressure.
	MemTableStopWritesThreshold int
	// Whether to enable pebble-internal metrics.
	EnableMetrics bool
	// Whether to emit simple estimated logical read/write counters.
	EnableReadWriteMetrics bool
	// How often to scrape pebble-internal metrics.
	MetricsScrapeInterval time.Duration
}

// Default configuration for the PebbleDB database.
func DefaultConfig() PebbleDBConfig {
	return PebbleDBConfig{
		CacheSize:                   int64(512 * unit.MB),
		MemTableSize:                64 * unit.MB,
		MemTableStopWritesThreshold: 4,
		EnableMetrics:               true,
		MetricsScrapeInterval:       10 * time.Second,
	}
}

func (c PebbleDBConfig) withDefaults() PebbleDBConfig {
	defaults := DefaultConfig()
	if c.CacheSize == 0 {
		c.CacheSize = defaults.CacheSize
	}
	if c.MemTableSize == 0 {
		c.MemTableSize = defaults.MemTableSize
	}
	if c.MemTableStopWritesThreshold == 0 {
		c.MemTableStopWritesThreshold = defaults.MemTableStopWritesThreshold
	}
	return c
}

// Validates the configuration (basic sanity checks).
func (c *PebbleDBConfig) Validate() error {
	if c.DataDir == "" {
		return fmt.Errorf("data dir is required")
	}
	if c.CacheSize < 0 {
		return fmt.Errorf("cache size must not be negative")
	}
	if c.MemTableStopWritesThreshold < 0 {
		return fmt.Errorf("memtable stop-writes threshold must not be negative")
	}
	if c.EnableMetrics && c.MetricsScrapeInterval <= 0 {
		return fmt.Errorf("metrics scrape interval must be positive when metrics are enabled")
	}
	return nil
}

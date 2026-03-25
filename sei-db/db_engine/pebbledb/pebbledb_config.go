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
	// The size of pebbleDB's internal block cache, in bytes.
	BlockCacheSize int
	// Whether to enable pebble-internal metrics.
	EnableMetrics bool
	// How often to scrape pebble-internal metrics.
	MetricsScrapeInterval time.Duration
}

// Default configuration for the PebbleDB database.
func DefaultConfig() PebbleDBConfig {
	return PebbleDBConfig{
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
	if c.BlockCacheSize <= 0 {
		return fmt.Errorf("block cache size must be greater than 0")
	}
	if c.EnableMetrics && c.MetricsScrapeInterval <= 0 {
		return fmt.Errorf("metrics scrape interval must be positive when metrics are enabled")
	}
	return nil
}

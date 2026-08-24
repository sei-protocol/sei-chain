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

	// Size, in bytes, of a memtable for this database.
	//
	// Every write is a skiplist insert into the memtable, and the cost of that insert grows with how
	// many entries the memtable already holds: a deeper structure to descend, over a working set less
	// likely to be cached. Smaller memtables make writes cheaper and are paid for with more frequent
	// flushes, and so more L0 files for compaction to absorb.
	//
	// Keep this above twice the snapshot engine's TargetBytesPerFlush. Pebble diverts a batch larger
	// than half a memtable onto its flushable-batch slow path, which hurts read amplification and
	// compaction shape.
	//
	// Default: 16 MB
	MemTableSize uint64 `mapstructure:"mem-table-size"`

	// How many memtables may exist before writes block waiting for one to be flushed.
	//
	// Multiplied by MemTableSize this is the memory a database's memtables may occupy, and it is the
	// slack that lets a burst of writes proceed while earlier memtables are still being flushed. Set it
	// too low and writers stall on flush latency rather than on any real limit, which is charged to the
	// memtable_write_stall phase of pebble_commit_phase_duration.
	//
	// Default: 16, which with the default MemTableSize allows 256 MB.
	MemTableStopWritesThreshold int `mapstructure:"mem-table-stop-writes-threshold"`
}

// Default configuration for the PebbleDB database.
func DefaultConfig() PebbleDBConfig {
	return PebbleDBConfig{
		EnableMetrics:               true,
		MetricsScrapeInterval:       10 * time.Second,
		BlockCacheSize:              int64(512 * unit.MB),
		MaxConcurrentCompactions:    max(4, runtime.NumCPU()/4),
		MemTableSize:                uint64(16 * unit.MB),
		MemTableStopWritesThreshold: 16,
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
	if c.MemTableSize == 0 {
		return fmt.Errorf("mem table size must be positive")
	}
	if c.MemTableStopWritesThreshold < 2 {
		return fmt.Errorf("mem table stop writes threshold must be at least 2, got %d",
			c.MemTableStopWritesThreshold)
	}
	return nil
}

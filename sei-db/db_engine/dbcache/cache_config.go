package dbcache

import (
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
)

type CacheConfig struct {
	// The number of shards in the cache. Must be a power of two and greater than 0.
	ShardCount uint64

	// The maximum size of the cache, in bytes. This only constrans the DB cache, not the cache snapshots,
	// since cache snapshots cannot be freed without compromising consistency semantics.
	MaxSize uint64

	// The estimated overhead per entry, in bytes. This is used to calculate the maximum size of the cache.
	// This value should be derived experimentally, and may differ between different builds and architectures.
	EstimatedOverheadPerEntry uint64

	// Name used as the "cache" attribute on OTel metrics. Must be non-empty.
	MetricsName string

	// Whether to enable OTel metrics collection.
	MetricsEnabled bool

	// How often to scrape cache size for metrics, in seconds.
	MetricsScrapeIntervalSeconds float64

	// How often to run garbage collection, in seconds.
	GCIntervalSeconds float64

	// The maximum number of unreleased snapshots that can be pending GC before Snapshot() blocks.
	MaxUnGCdVersions uint64

	// Target number of keys per batch when flushing GC'd data to the underlying DB.
	TargetKeysPerFlush int
}

func DefaultCacheConfig() *CacheConfig {
	return &CacheConfig{
		ShardCount:                   8,
		MaxSize:                      unit.GB / 2,
		EstimatedOverheadPerEntry:    256,
		MetricsScrapeIntervalSeconds: 10,
		GCIntervalSeconds:            1.0,
		MaxUnGCdVersions:             4,
		TargetKeysPerFlush:           1024 * 10,
	}
}

func (c *CacheConfig) MetricsScrapeInterval() time.Duration {
	return time.Duration(c.MetricsScrapeIntervalSeconds * float64(time.Second))
}

func (c *CacheConfig) GCInterval() time.Duration {
	return time.Duration(c.GCIntervalSeconds * float64(time.Second))
}

func (c *CacheConfig) Validate() error {
	if c.ShardCount == 0 || (c.ShardCount&(c.ShardCount-1)) != 0 {
		return fmt.Errorf("ShardCount must be a power of two and greater than 0, got %d", c.ShardCount)
	}
	if c.MaxSize == 0 {
		return fmt.Errorf("MaxSize must be greater than 0")
	}
	if c.MaxSize < c.ShardCount {
		return fmt.Errorf("MaxSize (%d) must be >= ShardCount (%d)", c.MaxSize, c.ShardCount)
	}
	if c.EstimatedOverheadPerEntry == 0 {
		return fmt.Errorf("EstimatedOverheadPerEntry must be greater than 0")
	}
	if c.MetricsName == "" {
		return fmt.Errorf("MetricsName must be non-empty")
	}
	if c.MetricsEnabled && c.MetricsScrapeIntervalSeconds <= 0 {
		return fmt.Errorf("MetricsScrapeIntervalSeconds must be positive when MetricsEnabled is true")
	}
	if c.GCIntervalSeconds <= 0 {
		return fmt.Errorf("GCIntervalSeconds must be positive")
	}
	if c.MaxUnGCdVersions == 0 {
		return fmt.Errorf("MaxUnGCdVersions must be greater than 0")
	}
	if c.TargetKeysPerFlush <= 0 {
		return fmt.Errorf("TargetKeysPerFlush must be positive")
	}
	return nil
}

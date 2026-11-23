package lakesim

import (
	"errors"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/cdkdatalake"
)

var _ utils.Config = (*LakesimConfig)(nil)

// LakesimConfig configures a lakesim run.
type LakesimConfig struct {
	// Free text describing the configuration. Ignored by the benchmark.
	Comment string

	// The lanes, one data lake each. Lane i is the store named lane<i>.
	Lanes []LaneConfig

	// The maximum number of blocks produced per second. 0 produces blocks as fast as backpressure allows.
	BlocksPerSecond float64

	// The number of blocks to produce before stopping. 0 runs until interrupted.
	MaxBlocks uint64

	// The number of most recent blocks each lane keeps. Older blocks are deleted. 0 keeps every block.
	RetainedBlocks uint64

	// The number of blocks beyond RetainedBlocks a lane may hold while their deletes are pending. A lane that
	// reaches it stops accepting blocks until deletes catch up. Must cover the blocks produced in one
	// MetadataWriteIntervalMs, since a lake starts deleting a key only once metadata recording its deletion is
	// durable.
	MaxDeletionBacklog uint64

	// The maximum number of writes each lane's data lake performs at once.
	ParallelWrites int

	// The maximum number of accepted writes each lane's data lake holds waiting for one of its ParallelWrites. A
	// put that finds it full blocks.
	QueuedWrites int

	// The Go CDK URL of the bucket holding every lane, such as s3://my-bucket?region=us-east-2 or
	// file:///tmp/lakesim?create_dir=true.
	BucketURL string

	// The minimum time between each data lake's periodic metadata writes, in milliseconds.
	MetadataWriteIntervalMs int

	// The maximum number of deletes each data lake starts each time its control loop wakes.
	DeletesPerCycle int

	// How long a data lake waits before retrying an operation that failed with a transient error, in
	// milliseconds.
	RetryBackoffMs int

	// If true, a data lake retries an operation that failed with a transient error until it succeeds.
	InfiniteRetries bool

	// The number of times a data lake retries an operation before failing, when InfiniteRetries is false.
	MaxRetries int

	// How often each lane reads its data lake's watermarks, in milliseconds. Bounds the resolution of the
	// durable latency measurement and of deletion backpressure.
	WatermarkPollIntervalMs int

	// The size of the buffer of pre-generated random bytes values are cut from. Must be at least the largest
	// ValueSize.
	CannedRandomSize int

	// The seed of the random bytes.
	Seed int64

	// How often a progress line is printed, in seconds. 0 prints none.
	ConsoleUpdateIntervalSeconds int

	// Address for the Prometheus metrics HTTP server (e.g. ":9697"). Empty disables metrics serving.
	MetricsAddr string

	// Address for the pprof HTTP server (e.g. ":6060"). Empty disables profiling.
	PprofAddr string

	// The mutex profile's sampling rate: 1 records every contention event, N one in N on average, and 0 leaves
	// the profile off. Requires PprofAddr.
	MutexProfileFraction int

	// The block profile's sampling rate, in nanoseconds of blocked time per sample. 0 leaves the profile off.
	// Requires PprofAddr.
	BlockProfileRate int

	// How often to scrape background process metrics, in seconds. 0 disables them.
	BackgroundMetricsScrapeInterval int
}

// LaneConfig configures one lane.
type LaneConfig struct {
	// The size in bytes of the value the lane writes for each block.
	ValueSize int
}

// DefaultLakesimConfig returns the default lakesim configuration. It names no bucket.
func DefaultLakesimConfig() *LakesimConfig {
	lakeDefaults := cdkdatalake.DefaultDataLakeConfig()
	return &LakesimConfig{
		Lanes:                           []LaneConfig{{ValueSize: 1024 * 1024}},
		BlocksPerSecond:                 0,
		MaxBlocks:                       0,
		RetainedBlocks:                  10_000,
		MaxDeletionBacklog:              1_000,
		ParallelWrites:                  lakeDefaults.WriterCount,
		QueuedWrites:                    lakeDefaults.WorkBufferSize,
		BucketURL:                       "",
		MetadataWriteIntervalMs:         int(lakeDefaults.MetadataWriteInterval / time.Millisecond),
		DeletesPerCycle:                 lakeDefaults.DeletesPerCycle,
		RetryBackoffMs:                  int(lakeDefaults.RetryBackoff / time.Millisecond),
		InfiniteRetries:                 lakeDefaults.InfiniteRetries,
		MaxRetries:                      lakeDefaults.MaxRetries,
		WatermarkPollIntervalMs:         10,
		CannedRandomSize:                64 * 1024 * 1024,
		Seed:                            1337,
		ConsoleUpdateIntervalSeconds:    5,
		MetricsAddr:                     ":9697",
		PprofAddr:                       "",
		MutexProfileFraction:            0,
		BlockProfileRate:                0,
		BackgroundMetricsScrapeInterval: 10,
	}
}

// Validate returns an error if the configuration is unusable.
func (c *LakesimConfig) Validate() error {
	if len(c.Lanes) == 0 {
		return errors.New("at least one lane is required")
	}
	for i, lane := range c.Lanes {
		if lane.ValueSize < 0 {
			return fmt.Errorf("lane %d: value size must not be negative, got %d", i, lane.ValueSize)
		}
		if lane.ValueSize > c.CannedRandomSize {
			return fmt.Errorf("lane %d: value size %d exceeds the canned random size %d",
				i, lane.ValueSize, c.CannedRandomSize)
		}
	}
	if c.BlocksPerSecond < 0 {
		return fmt.Errorf("blocks per second must not be negative, got %f", c.BlocksPerSecond)
	}
	if c.WatermarkPollIntervalMs <= 0 {
		return fmt.Errorf("watermark poll interval must be positive, got %d", c.WatermarkPollIntervalMs)
	}
	if c.CannedRandomSize < 8 {
		return fmt.Errorf("canned random size must be at least 8, got %d", c.CannedRandomSize)
	}
	if c.ConsoleUpdateIntervalSeconds < 0 {
		return fmt.Errorf("console update interval must not be negative, got %d",
			c.ConsoleUpdateIntervalSeconds)
	}
	if c.PprofAddr == "" && (c.MutexProfileFraction != 0 || c.BlockProfileRate != 0) {
		return errors.New("mutex and block profiling require a pprof address")
	}
	if err := c.DataLakeConfig().Validate(); err != nil {
		return fmt.Errorf("invalid data lake settings: %w", err)
	}
	return nil
}

// DataLakeConfig returns the configuration of each lane's data lake.
func (c *LakesimConfig) DataLakeConfig() cdkdatalake.DataLakeConfig {
	config := cdkdatalake.DefaultDataLakeConfig()
	config.BucketURL = c.BucketURL
	config.WriterCount = c.ParallelWrites
	config.WorkBufferSize = c.QueuedWrites
	config.MetadataWriteInterval = time.Duration(c.MetadataWriteIntervalMs) * time.Millisecond
	config.DeletesPerCycle = c.DeletesPerCycle
	config.RetryBackoff = time.Duration(c.RetryBackoffMs) * time.Millisecond
	config.InfiniteRetries = c.InfiniteRetries
	config.MaxRetries = c.MaxRetries
	return config
}

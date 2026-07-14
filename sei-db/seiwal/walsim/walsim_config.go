package walsim

import (
	"fmt"
	"strings"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
)

var _ utils.Config = (*WalsimConfig)(nil)

// WalsimConfig is the configuration for the walsim benchmark.
type WalsimConfig struct {

	// The WAL implementation to benchmark. One of "seiwal" (the new WAL) or "legacy" (the old
	// sei-db/wal WAL, driven through a throwaway adapter).
	Backend string

	// The size in bytes of each record appended to the WAL. Every append writes exactly this many
	// bytes of opaque, canned-random data.
	RecordSizeBytes uint64

	// Size in bytes of the pre-generated random buffer used to synthesize record payloads. The buffer
	// is filled once at startup and sliced (zero-copy) thereafter, so the generator never runs
	// math/rand or allocates payload bytes on the hot path. Must be at least RecordSizeBytes.
	RandomDataBufferSizeBytes uint64

	// The capacity of the queue that holds generated records before they are consumed by the
	// benchmark. A larger queue lets the generator run further ahead of the consumer.
	StagedRecordQueueSize uint64

	// How often (in records) to call Flush() on the WAL. 0 means never explicitly flush.
	FlushIntervalRecords uint64

	// The target size in bytes of the on-disk data. Once the retained data would exceed this, the
	// benchmark issues prune requests to hold the on-disk size as close to this target as possible.
	// 0 disables pruning entirely.
	TargetDiskSizeBytes uint64

	// For the "legacy" backend only: coalesce prune requests, forwarding only every Nth PruneBefore
	// to the underlying (expensive) TruncateBefore. Ignored by the "seiwal" backend. Must be at
	// least 1.
	PruneRelaxationFactor uint64

	// The configuration for the "seiwal" backend, used only when Backend is "seiwal". Path and Name
	// are owned by walsim (Path is taken from DataDir); any values supplied for them here are
	// overwritten.
	Seiwal seiwal.Config

	// The configuration for the "legacy" backend, used only when Backend is "legacy". The storage
	// directory is taken from DataDir, and KeepRecent/PruneInterval are forced off so that walsim
	// drives pruning through PruneBefore; any values supplied for them here are overwritten.
	Legacy wal.Config

	// The seed to use for the random number generator. Altering this seed for a pre-existing data
	// directory is harmless (records are opaque), but keeping it stable makes runs reproducible.
	Seed int64

	// The directory to store the benchmark data.
	DataDir string

	// If this many seconds go by without a console update, the benchmark will print a report.
	ConsoleUpdateIntervalSeconds float64

	// If this many records are written without a console update, the benchmark will print a report.
	// Prevents console spam when throughput is very high.
	ConsoleUpdateIntervalRecords uint64

	// The amount of time to run the benchmark for, in seconds. If 0, runs until interrupted.
	MaxRuntimeSeconds int

	// Address for the Prometheus metrics HTTP server (e.g. ":9090"). If empty, metrics are disabled.
	MetricsAddr string

	// If true, pressing Enter in the terminal will toggle suspend/resume of the benchmark.
	EnableSuspension bool

	// How often (in seconds) to scrape background metrics (data dir size, uptime). If 0, background
	// metrics are disabled.
	BackgroundMetricsScrapeInterval int

	// Directory for seilog output files. Supports ~ expansion and relative paths.
	LogDir string

	// Log level for seilog output. Valid values: debug, info, warn, error.
	LogLevel string

	// If true, delete the contents of DataDir before opening the WAL.
	CleanDataOnStart bool

	// If true, delete the contents of LogDir before starting.
	CleanLogsOnStart bool

	// If true, delete the contents of DataDir after the benchmark finishes.
	CleanDataOnExit bool

	// If true, delete the contents of LogDir after the benchmark finishes.
	CleanLogsOnExit bool

	// Throttle record write rate to this many records per second. 0 = disabled (unlimited
	// throughput). Useful for testing steady-state performance rather than max-throughput thrashing.
	MaxRecordsPerSecond float64

	// This field is ignored, but allows for a comment to be added to the config file.
	Comment string
}

// DefaultWalsimConfig returns the default configuration for the walsim benchmark.
func DefaultWalsimConfig() *WalsimConfig {
	return &WalsimConfig{
		Backend:                   "seiwal",
		RecordSizeBytes:           1 * unit.MB,
		RandomDataBufferSizeBytes: unit.GB,
		StagedRecordQueueSize:     16,
		FlushIntervalRecords:      1,
		TargetDiskSizeBytes:       1 * unit.GB,
		PruneRelaxationFactor:     100,
		// Path is set by walsim from DataDir at open time.
		Seiwal: *seiwal.DefaultConfig("", "walsim"),
		Legacy: wal.Config{
			WriteBufferSize: 0,
			WriteBatchSize:  64,
			FsyncEnabled:    false,
		},
		Seed:                            1337,
		DataDir:                         "data",
		ConsoleUpdateIntervalSeconds:    1,
		ConsoleUpdateIntervalRecords:    10_000,
		MaxRuntimeSeconds:               0,
		MetricsAddr:                     ":9090",
		EnableSuspension:                true,
		BackgroundMetricsScrapeInterval: 60,
		LogDir:                          "logs",
		LogLevel:                        "info",
		CleanDataOnStart:                false,
		CleanLogsOnStart:                false,
		CleanDataOnExit:                 false,
		CleanLogsOnExit:                 false,
		MaxRecordsPerSecond:             0,
	}
}

// Validate checks that the configuration is sane and returns an error if not.
func (c *WalsimConfig) Validate() error {
	switch c.Backend {
	case "seiwal", "legacy":
	default:
		return fmt.Errorf("invalid Backend %q, must be one of seiwal, legacy", c.Backend)
	}
	if c.RecordSizeBytes < 1 {
		return fmt.Errorf("RecordSizeBytes must be at least 1 (got %d)", c.RecordSizeBytes)
	}
	if c.RandomDataBufferSizeBytes < c.RecordSizeBytes {
		return fmt.Errorf("RandomDataBufferSizeBytes must be at least RecordSizeBytes (%d) (got %d)",
			c.RecordSizeBytes, c.RandomDataBufferSizeBytes)
	}
	if c.StagedRecordQueueSize < 1 {
		return fmt.Errorf("StagedRecordQueueSize must be at least 1 (got %d)", c.StagedRecordQueueSize)
	}
	if c.TargetDiskSizeBytes > 0 && c.TargetDiskSizeBytes < c.RecordSizeBytes {
		return fmt.Errorf("TargetDiskSizeBytes, when non-zero, must be at least RecordSizeBytes (%d) (got %d)",
			c.RecordSizeBytes, c.TargetDiskSizeBytes)
	}
	if c.PruneRelaxationFactor < 1 {
		return fmt.Errorf("PruneRelaxationFactor must be at least 1 (got %d)", c.PruneRelaxationFactor)
	}
	if c.DataDir == "" {
		return fmt.Errorf("DataDir is required")
	}
	if c.LogDir == "" {
		return fmt.Errorf("LogDir is required")
	}
	if c.ConsoleUpdateIntervalSeconds < 0 {
		return fmt.Errorf("ConsoleUpdateIntervalSeconds must be non-negative (got %f)", c.ConsoleUpdateIntervalSeconds)
	}
	if c.MaxRuntimeSeconds < 0 {
		return fmt.Errorf("MaxRuntimeSeconds must be non-negative (got %d)", c.MaxRuntimeSeconds)
	}
	if c.BackgroundMetricsScrapeInterval < 0 {
		return fmt.Errorf("BackgroundMetricsScrapeInterval must be non-negative (got %d)", c.BackgroundMetricsScrapeInterval)
	}
	if c.MaxRecordsPerSecond < 0 {
		return fmt.Errorf("MaxRecordsPerSecond must be non-negative (got %f)", c.MaxRecordsPerSecond)
	}
	switch strings.ToLower(c.LogLevel) {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("LogLevel must be one of debug, info, warn, error (got %q)", c.LogLevel)
	}
	return nil
}

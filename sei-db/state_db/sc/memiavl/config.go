package memiavl

const (
	DefaultSnapshotInterval = 10000
	// DefaultSnapshotKeepRecent is how many old snapshots (besides the latest) to
	// keep by default.
	DefaultSnapshotKeepRecent = 2
	// MinSnapshotKeepRecent is the smallest number of old snapshots memIAVL will
	// retain when the value comes from operator config. A configured value of 0
	// (keep only the current snapshot) is clamped up to this floor.
	MinSnapshotKeepRecent            uint32 = 1
	DefaultSnapshotMinTimeInterval          = 60 * 60 // 1 hour in seconds
	DefaultAsyncCommitBuffer                = 100
	DefaultSnapshotPrefetchThreshold        = 0.8 // prefetch if <80% pages in cache
	DefaultSnapshotWriteRateMBps            = 100 // 100 MB/s default
	DefaultSnapshotWriterLimit              = 4   // controls tree concurrency but not I/O rate (use SnapshotWriteRateMBps for that)
)

type Config struct {
	// AsyncCommitBuffer defines the size of asynchronous commit queue
	// this greatly improve block catching-up performance, <= 0 means synchronous commit.
	// defaults to 100
	AsyncCommitBuffer int `mapstructure:"async-commit-buffer"`

	// SnapshotKeepRecent defines how many old snapshots (excluding the latest one) to keep.
	// Defaults to 2; a configured value of 0 is clamped up to MinSnapshotKeepRecent (1).
	SnapshotKeepRecent uint32 `mapstructure:"snapshot-keep-recent"`

	// SnapshotInterval defines the block interval the memiavl snapshot is taken, default to 10000.
	SnapshotInterval uint32 `mapstructure:"snapshot-interval"`

	// SnapshotMinTimeInterval defines the minimum time interval (in seconds) between snapshots.
	// This prevents excessive snapshot creation during catch-up. Default to 3600 seconds (1 hour).
	SnapshotMinTimeInterval uint32 `mapstructure:"snapshot-min-time-interval"`

	// SnapshotWriterLimit defines the concurrency for taking commit store snapshot
	SnapshotWriterLimit int `mapstructure:"snapshot-writer-limit"`

	// SnapshotPrefetchThreshold defines the page cache residency threshold (0.0-1.0)
	// to trigger snapshot prefetch during cold-start.
	// Prefetch sequentially reads nodes/leaves files into page cache for faster replay.
	// Only active trees (evm/bank/acc/wasm) are prefetched, skipping sparse kv files.
	// Skips prefetch if >threshold of pages already resident (e.g., 0.8 = 80%).
	// Defaults to 0.8
	SnapshotPrefetchThreshold float64 `mapstructure:"snapshot-prefetch-threshold"`

	// SnapshotWriteRateMBps is the global snapshot write rate limit in MB/s. 0 = unlimited. Default 100.
	SnapshotWriteRateMBps int `mapstructure:"snapshot-write-rate-mbps"`
}

func DefaultConfig() Config {
	return Config{
		AsyncCommitBuffer:         DefaultAsyncCommitBuffer,
		SnapshotInterval:          DefaultSnapshotInterval,
		SnapshotKeepRecent:        DefaultSnapshotKeepRecent,
		SnapshotMinTimeInterval:   DefaultSnapshotMinTimeInterval,
		SnapshotPrefetchThreshold: DefaultSnapshotPrefetchThreshold,
		SnapshotWriteRateMBps:     DefaultSnapshotWriteRateMBps,
		SnapshotWriterLimit:       DefaultSnapshotWriterLimit,
	}
}

// NormalizeSnapshotKeepRecent clamps a configured snapshot-keep-recent value to
// MinSnapshotKeepRecent, so a value of 0 (keep only the current snapshot) is
// bumped up to keep at least one old snapshot.
func NormalizeSnapshotKeepRecent(keepRecent uint32) uint32 {
	if keepRecent < MinSnapshotKeepRecent {
		return MinSnapshotKeepRecent
	}
	return keepRecent
}

// NormalizeSnapshotInterval clamps a configured snapshot-interval of 0 up to
// DefaultSnapshotInterval, mirroring the effective cadence Options.FillDefaults
// applies at runtime. memIAVL never actually disables snapshots (FillDefaults
// bumps a 0/<=0 interval to DefaultSnapshotInterval), so callers that mirror
// this interval onto another backend must normalize it first; otherwise a
// configured 0 would disable that backend's snapshots while memIAVL keeps
// checkpointing every DefaultSnapshotInterval blocks.
func NormalizeSnapshotInterval(interval uint32) uint32 {
	if interval == 0 {
		return DefaultSnapshotInterval
	}
	return interval
}

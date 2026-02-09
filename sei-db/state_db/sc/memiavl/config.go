package memiavl

const (
	DefaultSnapshotInterval          = 10000
	DefaultSnapshotKeepRecent        = 0       // set to 0 to only keep one current snapshot
	DefaultSnapshotMinTimeInterval   = 60 * 60 // 1 hour in seconds
	DefaultAsyncCommitBuffer         = 100
	DefaultSnapshotPrefetchThreshold = 0.8 // prefetch if <80% pages in cache
	DefaultSnapshotWriteRateMBps     = 100 // 100 MB/s default
	DefaultSnapshotWriterLimit       = 4   // controls tree concurrency but not I/O rate (use SnapshotWriteRateMBps for that)
)

type Config struct {
	// AsyncCommitBuffer defines the size of asynchronous commit queue
	// this greatly improve block catching-up performance, <= 0 means synchronous commit.
	// defaults to 100
	AsyncCommitBuffer int `mapstructure:"async-commit-buffer"`

	// SnapshotKeepRecent defines what many old snapshots (excluding the latest one) to keep
	// defaults to 0 to only keep one current snapshot
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
	// Only active trees (evm/bank/acc) are prefetched, skipping sparse kv files.
	// Skips prefetch if >threshold of pages already resident (e.g., 0.8 = 80%).
	// Setting to 0 disables prefetching. Defaults to 0.8
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

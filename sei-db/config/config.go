package config

const (
	DefaultSnapshotInterval          = 10000
	DefaultSnapshotKeepRecent        = 0       // set to 0 to only keep one current snapshot
	DefaultSnapshotMinTimeInterval   = 60 * 60 // 1 hour in seconds
	DefaultAsyncCommitBuffer         = 100
	DefaultSnapshotPrefetchThreshold = 0.8 // prefetch if <80% pages in cache
	DefaultSSKeepRecent              = 100000
	DefaultSSPruneInterval           = 600
	DefaultSSImportWorkers           = 1
	DefaultSSAsyncBuffer             = 100
)

type StateCommitConfig struct {
	// Enable defines if the state-commit should be enabled.
	// If true, it will replace the existing IAVL db backend with memIAVL.
	// defaults to false.
	Enable bool `mapstructure:"enable"`

	// Directory defines the state-commit store directory
	// If not explicitly set, default to application home directory
	Directory string `mapstructure:"directory"`

	// ZeroCopy defines if the memiavl should return slices pointing to mmap-ed buffers directly (zero-copy),
	// the zero-copied slices must not be retained beyond current block's execution.
	// the sdk address cache will be disabled if zero-copy is enabled.
	// defaults to false.
	ZeroCopy bool `mapstructure:"zero-copy"`

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

	// CacheSize defines the size of the cache for each memiavl store.
	// Deprecated: this is removed, we will just rely on mmap page cache
	CacheSize int `mapstructure:"cache-size"`

	// OnlyAllowExportOnSnapshotVersion defines whether we only allow state sync
	// snapshot creation happens after the memiavl snapshot is created
	OnlyAllowExportOnSnapshotVersion bool `mapstructure:"only-allow-export-on-snapshot-version"`
}

type StateStoreConfig struct {

	// Enable defines if the state-store should be enabled for historical queries.
	Enable bool `mapstructure:"enable"`

	// DBDirectory defines the directory to store the state store db files
	// If not explicitly set, default to application home directory
	// default to empty
	DBDirectory string `mapstructure:"db-directory"`

	// Backend defines the backend database used for state-store
	// Supported backends: pebbledb, rocksdb
	// defaults to pebbledb
	Backend string `mapstructure:"backend"`

	// AsyncWriteBuffer defines the async queue length for commits to be applied to State Store
	// Set <= 0 for synchronous writes, which means commits also need to wait for data to be persisted in State Store.
	// defaults to 100
	AsyncWriteBuffer int `mapstructure:"async-write-buffer"`

	// KeepRecent defines the number of versions to keep in state store
	// Setting it to 0 means keep everything.
	// Default to keep the last 100,000 blocks
	KeepRecent int `mapstructure:"keep-recent"`

	// PruneIntervalSeconds defines the interval in seconds to trigger pruning
	// default to every 600 seconds
	PruneIntervalSeconds int `mapstructure:"prune-interval-seconds"`

	// ImportNumWorkers defines the number of goroutines used during import
	// defaults to 1
	ImportNumWorkers int `mapstructure:"import-num-workers"`

	// Whether to keep last version of a key during pruning or delete
	// defaults to true
	KeepLastVersion bool `mapstructure:"keep-last-version"`

	// UseDefaultComparer uses Pebble's default lexicographic byte comparer instead of
	// the custom MVCCComparer. This is NOT backwards compatible with existing databases
	// that were created with MVCCComparer - only use this for NEW databases.
	// The MVCC key encoding uses big-endian version bytes, so ordering is compatible,
	// but existing databases will fail to open due to comparer name mismatch.
	// defaults to false (use MVCCComparer for backwards compatibility)
	UseDefaultComparer bool `mapstructure:"use-default-comparer"`
}

// ReceiptStoreConfig defines configuration for the receipt store database.
type ReceiptStoreConfig struct {
	// DBDirectory defines the directory to store the receipt db files.
	// If not explicitly set, default to application home directory.
	DBDirectory string `mapstructure:"db-directory"`

	// Backend defines the backend used for receipt storage.
	// Supported backends: pebble (aka pebbledb), parquet.
	// defaults to pebble.
	Backend string `mapstructure:"backend"`

	// KeepRecent defines the number of versions to keep in receipt store.
	// Setting it to 0 means keep everything.
	KeepRecent int `mapstructure:"keep-recent"`

	// PruneIntervalSeconds defines the interval in seconds to trigger pruning.
	PruneIntervalSeconds int `mapstructure:"prune-interval-seconds"`
}

func DefaultStateCommitConfig() StateCommitConfig {
	return StateCommitConfig{
		Enable:                    true,
		AsyncCommitBuffer:         DefaultAsyncCommitBuffer,
		SnapshotInterval:          DefaultSnapshotInterval,
		SnapshotKeepRecent:        DefaultSnapshotKeepRecent,
		SnapshotMinTimeInterval:   DefaultSnapshotMinTimeInterval,
		SnapshotPrefetchThreshold: DefaultSnapshotPrefetchThreshold,
	}
}

func DefaultStateStoreConfig() StateStoreConfig {
	return StateStoreConfig{
		Enable:               true,
		Backend:              "pebbledb",
		AsyncWriteBuffer:     DefaultSSAsyncBuffer,
		KeepRecent:           DefaultSSKeepRecent,
		PruneIntervalSeconds: DefaultSSPruneInterval,
		ImportNumWorkers:     DefaultSSImportWorkers,
		KeepLastVersion:      true,
		UseDefaultComparer:   false, // TODO: flip to true once MVCCComparer is deprecated
	}
}

func DefaultReceiptStoreConfig() ReceiptStoreConfig {
	return ReceiptStoreConfig{
		Backend:              "pebbledb",
		KeepRecent:           DefaultSSKeepRecent,
		PruneIntervalSeconds: DefaultSSPruneInterval,
	}
}

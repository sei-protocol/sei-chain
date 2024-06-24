package config

const (
	DefaultSnapshotInterval    = 10000
	DefaultSnapshotKeepRecent  = 1
	DefaultSnapshotWriterLimit = 1
	DefaultAsyncCommitBuffer   = 100
	DefaultCacheSize           = 100000
	DefaultSSKeepRecent        = 100000
	DefaultSSPruneInterval     = 600
	DefaultSSImportWorkers     = 1
	DefaultSSAsyncBuffer       = 100
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
	// defaults to 1 to make sure ibc relayers work.
	SnapshotKeepRecent uint32 `mapstructure:"snapshot-keep-recent"`

	// SnapshotInterval defines the block interval the memiavl snapshot is taken, default to 10000.
	SnapshotInterval uint32 `mapstructure:"snapshot-interval"`

	// SnapshotWriterLimit defines the concurrency for taking commit store snapshot
	SnapshotWriterLimit int `mapstructure:"snapshot-writer-limit"`

	// CacheSize defines the size of the cache for each memiavl store.
	// Deprecated: this is removed, we will just rely on mmap page cache
	CacheSize int `mapstructure:"cache-size"`
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
}

func DefaultStateCommitConfig() StateCommitConfig {
	return StateCommitConfig{
		AsyncCommitBuffer:  DefaultAsyncCommitBuffer,
		CacheSize:          DefaultCacheSize,
		SnapshotInterval:   DefaultSnapshotInterval,
		SnapshotKeepRecent: DefaultSnapshotKeepRecent,
	}
}

func DefaultStateStoreConfig() StateStoreConfig {
	return StateStoreConfig{
		Backend:              "pebbledb",
		AsyncWriteBuffer:     DefaultSSAsyncBuffer,
		KeepRecent:           DefaultSSKeepRecent,
		PruneIntervalSeconds: DefaultSSPruneInterval,
		ImportNumWorkers:     DefaultSSImportWorkers,
		KeepLastVersion:      true,
	}
}

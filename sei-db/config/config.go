package config

const (
	DefaultCacheSize        = 100000
	DefaultSnapshotInterval = 10000
)

type StateCommitConfig struct {
	// Enable defines if the state-commit should be enabled.
	// If true, it will replace the existing IAVL db backend with memIAVL.
	// defaults to false.
	Enable bool `mapstructure:"enable"`

	// ZeroCopy defines if the memiavl should return slices pointing to mmap-ed buffers directly (zero-copy),
	// the zero-copied slices must not be retained beyond current block's execution.
	// the sdk address cache will be disabled if zero-copy is enabled.
	// defaults to false.
	ZeroCopy bool `mapstructure:"zero-copy"`

	// AsyncCommitBuffer defines the size of asynchronous commit queue, this greatly improve block catching-up
	// performance, -1 or 0 means synchronous commit.
	AsyncCommitBuffer int `mapstructure:"async-commit-buffer"`

	// SnapshotKeepRecent defines what many old snapshots (excluding the latest one) to keep
	// defaults to 1 to make sure ibc relayers work.
	SnapshotKeepRecent uint32 `mapstructure:"snapshot-keep-recent"`

	// SnapshotInterval defines the block interval the memiavl snapshot is taken, default to 10000.
	SnapshotInterval uint32 `mapstructure:"snapshot-interval"`

	// CacheSize defines the size of the cache for each memiavl store.
	// defaults to 100000.
	CacheSize int `mapstructure:"cache-size"`
}

type StateStoreConfig struct {

	// Enable defines if the state-store should be enabled for historical queries.
	Enable bool `mapstructure:"enable"`

	// Backend defines the backend database used for state-store
	// Supported backends: pebbledb, rocksdb
	// defaults to pebbledb
	Backend string `mapstructure:"backend"`

	// AsyncFlush defines if committing the block should also wait for the data to be persisted in the StateStore.
	// If true, data will be written to StateStore in async manner to reduce latency.
	// defaults to true
	AsyncFlush bool `mapstructure:"async-flush"`
}

func DefaultStateCommitConfig() StateCommitConfig {
	return StateCommitConfig{
		AsyncCommitBuffer:  100,
		CacheSize:          DefaultCacheSize,
		SnapshotInterval:   DefaultSnapshotInterval,
		SnapshotKeepRecent: 1,
	}
}

func DefaultStateStoreConfig() StateStoreConfig {
	return StateStoreConfig{
		Backend:    "pebbledb",
		AsyncFlush: true,
	}
}

package config

// DBBackend defines the SS DB backend.
type DBBackend string

const (
	DefaultSSKeepRecent    = 100000
	DefaultSSPruneInterval = 600
	DefaultSSImportWorkers = 1
	DefaultSSAsyncBuffer   = 100
	PebbleDBBackend        = "pebbledb"
	RocksDBBackend         = "rocksdb"
	DefaultSSBackend       = PebbleDBBackend
)

// StateStoreConfig defines configuration for the state store (SS) layer.
// EVM optimization is controlled via WriteMode/ReadMode (no separate Enable flag):
//   - WriteMode == CosmosOnlyWrite && ReadMode == CosmosOnlyRead → EVM stores not opened
//   - Any other mode → EVM stores opened and used per the mode semantics
type StateStoreConfig struct {
	// Enable defines if the state-store should be enabled for historical queries.
	Enable bool `mapstructure:"enable"`

	// DBDirectory overrides the directory to store the state store db files
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

	// KeepRecent defines the number of versions to keep in state store (shared by Cosmos and EVM).
	// Setting it to 0 means keep everything.
	// Default to keep the last 100,000 blocks
	KeepRecent int `mapstructure:"keep-recent"`

	// PruneIntervalSeconds defines the interval in seconds to trigger pruning
	// default to every 600 seconds
	PruneIntervalSeconds int `mapstructure:"prune-interval-seconds"`

	// ImportNumWorkers defines the number of goroutines used during import
	// defaults to 1
	ImportNumWorkers int `mapstructure:"import-num-workers"`

	// KeepLastVersion defines whether to keep last version of a key during pruning or delete
	// defaults to true
	KeepLastVersion bool `mapstructure:"keep-last-version"`

	// UseDefaultComparer uses Pebble's default lexicographic byte comparer instead of
	// the custom MVCCComparer. This is NOT backwards compatible with existing databases
	// that were created with MVCCComparer - only use this for NEW databases.
	// defaults to false (use MVCCComparer for backwards compatibility)
	UseDefaultComparer bool `mapstructure:"use-default-comparer"`

	// --- EVM optimization fields (embedded, matching SC pattern) ---

	// WriteMode controls how EVM data writes are routed between backends.
	// cosmos_only: all writes to Cosmos only (default, no EVM stores opened)
	// dual_write: EVM data written to both Cosmos and EVM stores
	// split_write: EVM data only to EVM stores, non-EVM to Cosmos
	WriteMode WriteMode `mapstructure:"write-mode"`

	// ReadMode controls how EVM data reads are routed.
	// cosmos_only: all reads from Cosmos only (default)
	// evm_first: try EVM store first, fall back to Cosmos
	// split_read: EVM data exclusively from EVM store
	ReadMode ReadMode `mapstructure:"read-mode"`

	// EVMDBDirectory defines the directory for EVM state store db files.
	// If not set, defaults to <home>/data/evm_ss
	EVMDBDirectory string `mapstructure:"evm-db-directory"`
}

// EVMEnabled returns true if EVM state stores should be opened.
// Derived from WriteMode/ReadMode — no separate Enable flag needed.
// Treats zero-value (empty string) as CosmosOnly (the default).
func (c StateStoreConfig) EVMEnabled() bool {
	writeMode := c.WriteMode
	if writeMode == "" {
		writeMode = CosmosOnlyWrite
	}
	readMode := c.ReadMode
	if readMode == "" {
		readMode = CosmosOnlyRead
	}
	return writeMode != CosmosOnlyWrite || readMode != CosmosOnlyRead
}

// DefaultStateStoreConfig returns the default StateStoreConfig
func DefaultStateStoreConfig() StateStoreConfig {
	return StateStoreConfig{
		Enable:               true,
		Backend:              DefaultSSBackend,
		AsyncWriteBuffer:     DefaultSSAsyncBuffer,
		KeepRecent:           DefaultSSKeepRecent,
		PruneIntervalSeconds: DefaultSSPruneInterval,
		ImportNumWorkers:     DefaultSSImportWorkers,
		KeepLastVersion:      true,
		UseDefaultComparer:   false,
		WriteMode:            CosmosOnlyWrite,
		ReadMode:             CosmosOnlyRead,
	}
}

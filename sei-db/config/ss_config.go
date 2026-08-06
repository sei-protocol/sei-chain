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
// EVM optimization is controlled via a single EVMSplit bool. When false,
// all state (including EVM) lives in the Cosmos SS backend. When true, EVM
// data is routed exclusively to a dedicated EVM SS backend with no fallback.
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

	// EnableReadWriteMetrics emits simple estimated read/write counters for the state-store backend.
	// defaults to false
	EnableReadWriteMetrics bool `mapstructure:"enable-read-write-metrics"`

	// KeepLastVersion defines whether to keep last version of a key during pruning or delete
	// defaults to true
	KeepLastVersion bool `mapstructure:"keep-last-version"`

	// UseDefaultComparer uses Pebble's default lexicographic byte comparer instead of
	// the custom MVCCComparer. This is NOT backwards compatible with existing databases
	// that were created with MVCCComparer - only use this for NEW databases.
	// defaults to false (use MVCCComparer for backwards compatibility)
	UseDefaultComparer bool `mapstructure:"use-default-comparer"`

	// SnapshotEnable controls whether the state store takes periodic online
	// snapshots. Snapshots are Pebble checkpoints (hardlink trees), so each
	// retained snapshot pins the SSTs it references and prevents compaction
	// from reclaiming them. Steady-state disk overhead is therefore the
	// compaction churn accumulated over SnapshotInterval blocks, per retained
	// snapshot — significant on a multi-TB state store. Disable this on nodes
	// that do not serve archives or state sync from SS.
	// defaults to true
	SnapshotEnable bool `mapstructure:"snapshot-enable"`

	// SnapshotInterval and SnapshotKeepRecent are mirrored from the state-commit
	// snapshot settings at runtime by AlignSSSnapshotWithSC. They are
	// intentionally not exposed in app.toml so SS and SC cannot drift onto
	// different cadences; SnapshotEnable is the only SS-side knob.
	SnapshotInterval   int64
	SnapshotKeepRecent int

	// --- EVM optimization fields ---

	// EVMSplit controls whether EVM data is routed to a dedicated SS backend.
	// When false (default), all state — including EVM — lives in the Cosmos SS
	// backend. When true, EVM data goes only to the EVM SS backend; non-EVM
	// only to Cosmos. No fallback: a missing key returns empty.
	EVMSplit bool `mapstructure:"evm-split"`

	// EVMDBDirectory defines the directory for EVM state store db files.
	// If not set, defaults to <home>/data/state_store/evm/{backend}
	EVMDBDirectory string `mapstructure:"evm-db-directory"`

	// SeparateEVMSubDBs controls whether EVM data is physically split across
	// per-type databases. When false (default), all EVM data stays in one DB.
	// When true, data is routed to separate DBs by EVM key family while
	// preserving the same logical store key and full key encoding inside each DB.
	SeparateEVMSubDBs bool `mapstructure:"evm-separate-dbs"`
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
		SnapshotEnable:       true,
		EVMSplit:             false,
		SeparateEVMSubDBs:    false,
	}
}

// AlignSSSnapshotWithSC mirrors the state-commit snapshot cadence onto the
// state store, so a height retained by SC is also retained by SS. When SS
// snapshots are disabled the cadence is zeroed rather than mirrored, which is
// what the snapshot manager reads as "do not run".
func AlignSSSnapshotWithSC(scConfig StateCommitConfig, ssConfig *StateStoreConfig) {
	if !ssConfig.SnapshotEnable {
		ssConfig.SnapshotInterval = 0
		ssConfig.SnapshotKeepRecent = 0
		return
	}
	interval, keepRecent := EffectiveMemIAVLSnapshotCadence(scConfig.MemIAVLConfig)
	ssConfig.SnapshotInterval = int64(interval)
	ssConfig.SnapshotKeepRecent = int(keepRecent)
}

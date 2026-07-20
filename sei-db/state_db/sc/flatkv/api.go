package flatkv

import (
	"io"

	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// Options configures a FlatKV store.
type Options struct {
	// Dir is the base directory containing snapshot dirs, working/, and changelog/.
	Dir string
}

// Store provides EVM state storage with LtHash integrity.
//
// Lifecycle: NewCommitStore (create) → LoadVersion (open) → ApplyChangeSets/Commit → Close.
// Write path: ApplyChangeSets (buffer) → Commit (persist).
// Read path: Get/Has/Iterator read committed state only.
// Key format: x/evm memiavl keys (mapped internally to account/code/storage DBs).
type Store interface {
	// LoadVersion opens the database at the given version (0 = latest).
	// When readOnly is true an isolated, read-only store is returned;
	// the caller must Close it when done.
	LoadVersion(targetVersion int64, readOnly bool) (Store, error)

	// ApplyChangeSets buffers EVM changesets (x/evm memiavl keys) and updates LtHash.
	// Non-EVM modules are routed into legacy storage under their module prefix.
	// Call Commit to persist.
	ApplyChangeSets(cs []*proto.NamedChangeSet) error

	// Commit persists buffered writes and advances the version.
	Commit() (int64, error)

	// SetInitialVersion seeds the store so that the next Commit produces
	// initialVersion. Must be called after LoadVersion, on a truly fresh
	// store (no prior commits) and before any writes. Returns an error on
	// a read-only store, on a non-fresh store, or for initialVersion <= 0.
	SetInitialVersion(initialVersion int64) error

	// EarliestVersion returns the version this store's history begins at
	// (the seeded version recorded by SetInitialVersion), or 0 when
	// unknown (genesis stores, and stores created before the record
	// existed). A non-zero result means versions below it predate the
	// store entirely — the chain ran without flatkv — as opposed to
	// pruned or corrupt in-history versions, which still fail to load.
	EarliestVersion() int64

	// Get returns the value for a key within the given module.
	// For EVM keys (moduleName == "evm"), the key is a memiavl EVM key
	// routed to account/storage/code/legacy DBs internally.
	// For non-EVM modules, the key is read from legacy storage with the module prefix.
	// If not found, returns (nil, false).
	Get(moduleName string, key []byte) (value []byte, found bool)

	// GetBlockHeightModified returns the block height at which the key was last modified.
	// Only supported for EVM keys; non-EVM legacy data does not track block height.
	// If not found, returns (-1, false, nil).
	GetBlockHeightModified(moduleName string, key []byte) (int64, bool, error)

	// Has reports whether the key exists within the given module.
	Has(moduleName string, key []byte) bool

	// RawGlobalIterator returns a positioned forward iterator over all committed
	// keys across underlying data DBs, merged in global lexicographic order.
	// Keys are physical format: "evm/" + type_prefix_byte + stripped_key.
	// Pending writes are not visible. Keys and values are read-only; copy
	// before modifying.
	//
	// The returned iterator is a stable snapshot taken at construction: it may
	// be used concurrently with, and outlive, subsequent ApplyChangeSets/Commit
	// calls without observing their effects. The caller must Close it when done;
	// an open iterator pins Pebble sstables/memtables and holds back compaction,
	// so close promptly rather than relying on it being safe to keep open.
	RawGlobalIterator() (dbm.Iterator, error)

	// Create an iterator over a range of keys in a given store.
	//
	// The returned iterator is a stable snapshot taken at construction (pending
	// writes are cloned and the Pebble view is pinned): it may be used
	// concurrently with, and outlive, subsequent ApplyChangeSets/Commit calls
	// without observing their effects. The caller must Close it when done; an
	// open iterator pins Pebble resources and holds back compaction, so close
	// promptly rather than relying on it being safe to keep open.
	Iterator(
		// The store to iterate over.
		store string,
		// The start key of the range to iterate over, inclusive.
		// If nil, the iterator will start at the beginning of the store.
		start []byte,
		// The end key of the range to iterate over, exclusive.
		// If nil, the iterator will iterate until the end of the store.
		end []byte,
		// Whether to iterate in ascending order.
		ascending bool,
	) (dbm.Iterator, error)

	// RootHash returns the 32-byte checksum of the working LtHash.
	// Note: This is the Blake3-256 digest of the underlying 2048-byte
	// raw LtHash vector.
	RootHash() []byte

	// CommittedRootHash returns the 32-byte checksum of the last committed LtHash.
	CommittedRootHash() []byte

	// HashCategories returns the hash logger category names this store reports (the global root plus one
	// per data DB). The set is fixed. The caller registers these on the logger.
	HashCategories() []string

	// RecordHashes reports this store's hashes (root + per-DB) for blockNumber. Call right after Commit.
	RecordHashes(hl hashlog.HashLogger, blockNumber uint64) error

	// Version returns the latest committed version.
	Version() int64

	// GetLatestVersion returns the latest committed version persisted to
	// disk. Equivalent to Version() once LoadVersion has run; before
	// LoadVersion it answers from on-disk metadata so callers can
	// inspect the store's height without taking ownership of it.
	GetLatestVersion() (int64, error)

	// WriteSnapshot writes a complete snapshot to dir.
	WriteSnapshot(dir string) error

	// Rollback restores state to targetVersion by rewinding to the best
	// snapshot, replaying WAL, and pruning snapshots/WAL beyond target.
	Rollback(targetVersion int64) error

	// Exporter creates an exporter for the given version (0 = current).
	Exporter(version int64) (types.Exporter, error)

	// Importer load data from snapshot to the database
	Importer(version int64) (types.Importer, error)

	// Get the phase timer used to measure time spent in various phases of execution. Useful for metrics
	// integration with external phases of execution.
	GetPhaseTimer() *metrics.PhaseTimer

	// CleanupOrphanedReadOnlyDirs removes readonly-* working directories
	// left behind by a previous process crash. Must be called once at
	// process startup, before any read-only instances are created.
	CleanupOrphanedReadOnlyDirs() error

	io.Closer
}

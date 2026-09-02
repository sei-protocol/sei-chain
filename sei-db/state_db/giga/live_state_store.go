package giga

import (
	"io"

	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// LiveStateStore provides EVM state storage with LtHash integrity.
//
// Lifecycle: NewCommitStore (create) → LoadLatest (open) → ApplyChangeSets/Commit → Close.
// Write path: ApplyChangeSets (buffer) → Commit (persist).
// Read path: Get/Has/Iterator read committed state only; LoadVersionReadOnly serves past versions.
// Key format: x/evm memiavl keys (mapped internally to account/code/storage DBs).
//
// There are no recoverable errors. Any error returned by this store is fatal, and halting is the
// caller's responsibility: on the first error the caller must stop rather than proceed on state the
// store cannot vouch for. Behaviour after that first error is undefined — a later call may fail, or may
// answer plausibly — so continued operation is not evidence that the failure was benign.
//
// Byte slices passed to or received from any method — including the keys and values an iterator
// yields — must not be mutated. They are not defensively copied: a value out of an iterator can point
// straight into memory the store is still using, so writing to it corrupts state that other readers
// will see.
type LiveStateStore interface {
	// LoadLatest opens this store at the latest persisted version, ready to commit. It must be called
	// before any read or write, and is the only way to obtain a committable store. Use Rollback to move a
	// committable store backwards.
	LoadLatest() error

	// LoadVersionReadOnly returns an isolated read-only view of the store at targetVersion (0 = latest).
	// This store is left untouched and keeps committing; the caller owns the view and must Close it.
	//
	// The view is reconstructed from the newest snapshot at or below targetVersion plus this store's WAL,
	// so a version whose history has been pruned fails rather than being served approximately.
	LoadVersionReadOnly(targetVersion int64) (LiveStateStore, error)

	// ApplyChangeSets buffers changesets at the given version, to be
	// persisted by the next Commit.
	//
	// Exactly one block may be buffered per Commit: repeated calls at the
	// same version are allowed, any other version is rejected (see
	// PendingVersion), and Commit must be called with that version.
	// Batching several blocks into one Commit is not supported.
	ApplyChangeSets(version int64, cs []*proto.NamedChangeSet) error

	// Commit persists buffered writes at the given version (block height).
	// One Commit persists exactly one block. If ApplyChangeSets has buffered
	// writes, version must equal the height those rows were stamped with
	// (see PendingVersion).
	Commit(version int64) (int64, error)

	// CommitStateChanges is a Giga-only helper that applies changesets and commits
	// them at blockNum in one call.
	// Callers must use either ApplyChangeSets+Commit or CommitStateChanges
	// exclusively, never mixing the two on the same store.
	// ApplyChangeSets+Commit is expected to be deprecated once all callers
	// move to CommitStateChanges.
	CommitStateChanges(blockNum int64, changeset []*proto.NamedChangeSet) error

	// OpenView returns a read-only StateView of the current block. The caller must Close it when done.
	OpenView() StateView

	// SetInitialVersion seeds the store so that Commit(initialVersion) is
	// accepted as the first commit. Must be called after LoadLatest, on a
	// truly fresh store (no prior commits) and before any writes. Returns an
	// error on a read-only store, on a non-fresh store, or for
	// initialVersion <= 0.
	SetInitialVersion(initialVersion int64) error

	// Get returns the value for a key within the given module.
	// For EVM keys (moduleName == "evm"), the key is a memiavl EVM key
	// routed to account/storage/code/misc DBs internally.
	// For non-EVM modules, the key is read from misc storage with the module prefix.
	// If not found, returns (nil, false).
	Get(moduleName string, key []byte) (value []byte, found bool)

	// GetBlockHeightModified returns the block height at which the key was last modified.
	// Only supported for EVM keys; non-EVM misc data does not track block height.
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

	// PublishedHash returns the most recent block hash the store has published: its height, its
	// lattice hash root, and each database's root. Hashing is asynchronous, so on a committing store
	// this lags the committed version; use FlushHashes to make it describe the version just committed.
	// On a freshly loaded or read-only store it is the height that was loaded.
	PublishedHash() *lthash.BlockHash

	// HashChan returns a channel producing the hash of each block: exactly one per block committed, in
	// block order, with no gaps or duplicates, closed once the store stops hashing.
	//
	// The channel has finite depth, so failure to dequeue hashes for long enough blocks commit. Every
	// store that returns one therefore needs a consumer.
	//
	// A store that will never carry a stream reports why instead of handing back one that stays empty:
	// one that is not open, and one that hashes only in order to replay and so consumes its own.
	HashChan() (<-chan *lthash.BlockHash, error)

	// FlushHashes blocks until the store has published a hash for every block committed so far, and
	// recorded each one's metadata alongside the block it describes.
	FlushHashes() error

	// CommitPendingBlock commits the block currently being applied, if any, so that it has a hash. A
	// no-op on a store with no pending writes.
	//
	// A block that has not been committed has no hash, so a caller wanting one mid-block is asking for
	// the block to be committed. This is that request, made explicitly. Post-Cosmos nothing asks for a
	// hash mid-block and this goes away.
	CommitPendingBlock() error

	// HashCategories returns the hash logger category names this store reports (the global root plus one
	// per data DB). The set is fixed. The caller registers these on the logger.
	HashCategories() []string

	// Version returns the latest committed version.
	Version() int64

	// PendingVersion returns the height of the block currently buffered by
	// ApplyChangeSets, or 0 when there are no buffered writes. It is the
	// version the next Commit must be called with, and the only version
	// further ApplyChangeSets calls may use.
	PendingVersion() int64

	// GetLatestVersion returns the latest committed version persisted to
	// disk. Equivalent to Version() once LoadLatest has run; before
	// LoadLatest it answers from on-disk metadata so callers can
	// inspect the store's height without taking ownership of it.
	GetLatestVersion() (int64, error)

	// Rollback rewinds a store opened with LoadLatest to targetVersion and prunes everything above it:
	// snapshots, WAL blocks and committed state. It is the only way to move a committable store backwards,
	// and the result keeps committing from targetVersion+1. An unreachable target is rejected before
	// anything is modified.
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

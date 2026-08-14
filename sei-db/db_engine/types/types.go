package types

import (
	"io"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	dbm "github.com/tendermint/tm-db"
)

// WriteOptions controls durability for write operations.
// Sync=true forces an fsync on commit.
type WriteOptions struct {
	Sync bool
}

// IterOptions controls iterator bounds.
// - LowerBound is inclusive.
// - UpperBound is exclusive.
// - Reverse iterates in descending key order when true (default false).
type IterOptions struct {
	LowerBound []byte
	UpperBound []byte
	Reverse    bool
}

// BatchGetResult describes the result of a single key lookup within a BatchGet call.
type BatchGetResult struct {
	// The value for the given key. If nil, the key was not found (but no error occurred).
	Value []byte
	// The error, if any, that occurred during the read.
	Error error
}

// IsFound returns true if the key was found (i.e. Value is not nil).
func (b BatchGetResult) IsFound() bool {
	return b.Value != nil
}

// OpenOptions configures opening a DB.
//
// NOTE: This is intentionally minimal today. Most performance-critical knobs
// (cache size, memtable sizing, compaction settings, etc.) are currently owned by
// the backend implementations. If/when we need per-node tuning, we can extend
// this struct or add engine-specific options.
//
// Comparer is optional; when set it must be compatible with the underlying
// engine (e.g. *pebble.Comparer for PebbleDB).
type OpenOptions struct {
	Comparer any
}

// KeyValueDB is a low-level KV engine contract (business-agnostic).
//
// Get returns a value copy (safe to use after the call returns).
type KeyValueDB interface {

	// Get returns the value for the given key, returning an error if the key is not found.
	//
	// A found zero-length value must be returned as a non-nil empty slice; nil is never a found
	// value. Callers (e.g. the snapshot engine) rely on this distinction to tell empty values
	// from absent keys.
	Get(key []byte) (value []byte, err error)

	// Perform a batch read operation. Given a map of keys to read, performs the reads and updates the
	// map with the results.
	//
	// It is not thread safe to read or mutate the map while this method is running.
	BatchGet(keys map[string]BatchGetResult) error

	// Set sets the value for the given key.
	Set(key, value []byte, opts WriteOptions) error

	// Delete deletes the value for the given key.
	Delete(key []byte, opts WriteOptions) error

	// NewIter returns a positioned forward iterator over the key-value store.
	// Keys and values are read-only; copy before modifying.
	NewIter(opts *IterOptions) (dbm.Iterator, error)

	// NewBatch returns a new batch for atomic writes.
	NewBatch() Batch

	// Flush flushes the database to disk.
	Flush() error

	// Close closes the database.
	io.Closer
}

// Batch is a set of modifications to apply atomically (business-agnostic).
type Batch interface {
	Set(key, value []byte) error
	Delete(key []byte) error

	// Commit applies the batch atomically: after a crash it is either fully present or fully absent.
	// Sequential commits on the same DB become durable in commit order — a crash may lose a suffix of
	// commits, never an earlier commit while retaining a later one.
	Commit(opts WriteOptions) error

	// Len returns the current encoded size of the batch in bytes — NOT the number of buffered
	// operations. Callers (e.g. the snapshot engine's flush batching) size batches by bytes;
	// implementations must preserve this unit.
	Len() int
	Reset()
	io.Closer
}

// Checkpointable is an optional capability for DB engines that support
// efficient point-in-time snapshots via filesystem hardlinks.
//
// Concurrency: Checkpoint is safe to call concurrently with reads and writes
// on the same DB instance. The resulting snapshot reflects a consistent
// point-in-time view; concurrent writes may or may not be included.
//
// Durability: When Checkpoint returns nil, the destination directory is a
// complete, crash-safe copy of the database state. It survives host OS
// crashes because it consists of hardlinks to already-fsynced SST files
// plus a flushed manifest.
type Checkpointable interface {
	Checkpoint(destDir string) error
}

// CheckpointVersionSetter writes the logical latest version into a completed
// checkpoint without changing the live database.
//
// The latest marker has to be stamped because a checkpoint is a copy of a
// database whose marker may already have moved on. The earliest marker is
// inherited from the checkpoint; pruning advances it before deleting history, so
// every checkpoint boundary either sees the old marker with old data or the new
// marker with data that is at least as deep as advertised.
type CheckpointVersionSetter interface {
	SetCheckpointVersion(destDir string, version int64) error
}

// DrainBarrier is an optional capability for engines that apply changesets from
// an async queue. It lets a caller place work at an exact point in the write
// order without waiting for the queue to drain.
type DrainBarrier interface {
	ScheduleAtDrain(fn func())
}

// PendingWriteWaiter is an optional capability for engines that apply changesets
// from an async queue. It blocks until the queue is empty.
type PendingWriteWaiter interface {
	WaitForPendingWrites()
}

// The interfaces above are engine capabilities. Deciding when a checkpoint runs,
// and what version it is labeled with, is coordination rather than engine
// behavior and lives in sei-db/management: CheckpointScheduler,
// ScheduleCheckpoint, SetCheckpointVersion and ErrCheckpointCanceled.

// ---------------------------------------------------------------------------
// SS DB layer
// ---------------------------------------------------------------------------

// StateStore is the unified interface for versioned key-value storage.
// Implemented by pebbledb/mvcc.Database, rocksdb/mvcc.Database,
// CosmosStateStore, EVMStateStore, and CompositeStateStore.
type StateStore interface {
	Get(storeKey string, version int64, key []byte) ([]byte, error)
	Has(storeKey string, version int64, key []byte) (bool, error)
	Iterator(storeKey string, version int64, start, end []byte) (dbm.Iterator, error)
	ReverseIterator(storeKey string, version int64, start, end []byte) (dbm.Iterator, error)
	RawIterate(storeKey string, fn func([]byte, []byte, int64) bool) (bool, error)
	GetLatestVersion() int64
	SetLatestVersion(version int64) error
	GetEarliestVersion() int64
	SetEarliestVersion(version int64, ignoreVersion bool) error
	ApplyChangesetSync(version int64, changesets []*proto.NamedChangeSet) error
	ApplyChangesetAsync(version int64, changesets []*proto.NamedChangeSet) error
	Prune(version int64) error
	Import(version int64, ch <-chan SnapshotNode) error
	io.Closer
}

type SnapshotNode struct {
	StoreKey string
	Key      []byte
	Value    []byte
}

type RawSnapshotNode struct {
	StoreKey string
	Key      []byte
	Value    []byte
	Version  int64
}

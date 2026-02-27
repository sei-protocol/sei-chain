package db_engine

import "io"

// WriteOptions controls durability for write operations.
// Sync=true forces an fsync on commit.
type WriteOptions struct {
	Sync bool
}

// IterOptions controls iterator bounds.
// - LowerBound is inclusive.
// - UpperBound is exclusive.
type IterOptions struct {
	LowerBound []byte
	UpperBound []byte
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

// DB is a low-level KV engine contract (business-agnostic).
//
// Get returns a value copy (safe to use after the call returns).
type DB interface {
	Get(key []byte) (value []byte, err error)
	Set(key, value []byte, opts WriteOptions) error
	Delete(key []byte, opts WriteOptions) error

	NewIter(opts *IterOptions) (Iterator, error)
	NewBatch() Batch

	Flush() error
	io.Closer
}

// Batch is a set of modifications to apply atomically (business-agnostic).
type Batch interface {
	Set(key, value []byte) error
	Delete(key []byte) error
	Commit(opts WriteOptions) error

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

// Iterator provides ordered iteration over keyspace with seek primitives.
//
// Zero-copy contract:
//   - Key/Value may return views into internal buffers and are only valid until the
//     next iterator positioning call (Next/Prev/Seek*/First/Last) or Close.
type Iterator interface {
	First() bool
	Last() bool
	Valid() bool

	SeekGE(key []byte) bool
	SeekLT(key []byte) bool

	Next() bool
	NextPrefix() bool
	Prev() bool

	Key() []byte
	Value() []byte
	Error() error
	io.Closer
}

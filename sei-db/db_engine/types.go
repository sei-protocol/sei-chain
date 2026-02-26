package db_engine

import (
	"io"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

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

// KeyValueDB is a low-level KV engine contract (business-agnostic).
//
// Get returns a value copy (safe to use after the call returns).
type KeyValueDB interface {
	Get(key []byte) (value []byte, err error)
	Set(key, value []byte, opts WriteOptions) error
	Delete(key []byte, opts WriteOptions) error

	NewIter(opts *IterOptions) (KeyValueDBIterator, error)
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

// KeyValueDBIterator provides ordered iteration over keyspace with seek primitives.
//
// Zero-copy contract:
//   - Key/Value may return views into internal buffers and are only valid until the
//     next iterator positioning call (Next/Prev/Seek*/First/Last) or Close.
type KeyValueDBIterator interface {
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

// ---------------------------------------------------------------------------
// MVCC (versioned) DB layer
// ---------------------------------------------------------------------------

// MvccDB is the DB engine layer contract for versioned key-value storage.
// Implemented by pebbledb/mvcc.Database and rocksdb/mvcc.Database.
type MvccDB interface {
	Get(storeKey string, version int64, key []byte) ([]byte, error)
	Has(storeKey string, version int64, key []byte) (bool, error)
	Iterator(storeKey string, version int64, start, end []byte) (DBIterator, error)
	ReverseIterator(storeKey string, version int64, start, end []byte) (DBIterator, error)
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

// DBIterator iterates over versioned key-value pairs.
type DBIterator interface {
	Domain() (start []byte, end []byte)
	Valid() bool
	Next()
	Key() (key []byte)
	Value() (value []byte)
	Error() error
	Close() error
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

package types

import (
	"io"

	"github.com/sei-protocol/sei-db/proto"
)

// StateStore is a versioned, embedded Key-Value Store,
// which allows efficient reads, writes, iteration over a specific version
type StateStore interface {
	Get(storeKey string, version int64, key []byte) ([]byte, error)
	Has(storeKey string, version int64, key []byte) (bool, error)
	Iterator(storeKey string, version int64, start, end []byte) (DBIterator, error)
	ReverseIterator(storeKey string, version int64, start, end []byte) (DBIterator, error)
	RawIterate(storeKey string, fn func([]byte, []byte, int64) bool) (bool, error)
	GetLatestVersion() (int64, error)
	SetLatestVersion(version int64) error
	GetEarliestVersion() (int64, error)
	SetEarliestVersion(version int64, ignoreVersion bool) error
	GetLatestMigratedKey() ([]byte, error)
	SetLatestMigratedKey(key []byte) error
	GetLatestMigratedModule() (string, error)
	SetLatestMigratedModule(module string) error
	WriteBlockRangeHash(storeKey string, beginBlockRange, endBlockRange int64, hash []byte) error

	// ApplyChangeset Persist the change set of a block,
	// the `changeSet` should be ordered by (storeKey, key),
	// the version should be latest version plus one.
	ApplyChangeset(version int64, cs *proto.NamedChangeSet) error

	// ApplyChangesetAsync Write changesets into WAL file first and apply later for async writes
	ApplyChangesetAsync(version int64, changesets []*proto.NamedChangeSet) error

	// Import the initial state of the store
	Import(version int64, ch <-chan SnapshotNode) error

	// Import the kv entries into the store in any order of version
	RawImport(ch <-chan RawSnapshotNode) error

	// Prune attempts to prune all versions up to and including the provided
	// version argument. The operation should be idempotent. An error should be
	// returned upon failure.
	Prune(version int64) error

	// Closer releases associated resources. It should NOT be idempotent. It must
	// only be called once and any call after may panic.
	io.Closer
}

type DBIterator interface {
	// Domain returns the start (inclusive) and end (exclusive) limits of the iterator.
	// CONTRACT: start, end readonly []byte
	Domain() (start []byte, end []byte)

	// Valid returns whether the current iterator is valid. Once invalid, the Iterator remains
	// invalid forever.
	Valid() bool

	// Next moves the iterator to the next key in the database, as defined by order of iteration.
	// If Valid returns false, this method will panic.
	Next()

	// Key returns the key at the current position. Panics if the iterator is invalid.
	// CONTRACT: key readonly []byte
	Key() (key []byte)

	// Value returns the value at the current position. Panics if the iterator is invalid.
	// CONTRACT: value readonly []byte
	Value() (value []byte)

	// Error returns the last error encountered by the iterator, if any.
	Error() error

	// Close closes the iterator, relasing any allocated resources.
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

func GetRawSnapshotNode(node SnapshotNode, version int64) RawSnapshotNode {
	return RawSnapshotNode{
		StoreKey: node.StoreKey,
		Key:      node.Key,
		Value:    node.Value,
		Version:  version,
	}
}

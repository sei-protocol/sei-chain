package types

import (
	"io"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// LtHasher is an interface for LtHash operations to avoid import cycle.
type LtHasher interface {
	MixIn(other interface{})
	MixOut(other interface{})
	Checksum() [32]byte
	Bytes() []byte
	IsIdentity() bool
}

// LtHashTimings holds wall-clock timing information for LtHash computation breakdown.
type LtHashTimings struct {
	TotalNs     int64 // Total wall-clock time
	SerializeNs int64 // Serialization phase
	Blake3Ns    int64 // Blake3 XOF phase
	MixInOutNs  int64 // MixIn/MixOut phase
	MergeNs     int64 // Merging worker results
}

// StateHash represents the commit hash and version.
type StateHash struct {
	Hash    []byte
	Version int64
}

// KVPair represents a key-value pair with metadata for LtHash.
type KVPair struct {
	Key            []byte
	Value          []byte
	LastFlushValue []byte // Value at last flush, used for LtHash incremental updates (MixOut)
	Deleted        bool
}

// LastFlushValueGetter is a function type for retrieving the last flushed value during LtHash computation.
// The function takes a storeKey (module name) and a key, and returns the value at last flush.
// This allows callers to provide last flush values from a deterministic source (e.g., SC/MemIAVL)
// instead of reading from SS which may be asynchronously lagging.
type LastFlushValueGetter func(storeKey string, key []byte) []byte

// StateStore is a versioned, embedded Key-Value Store,
// which allows efficient reads, writes, iteration over a specific version
type StateStore interface {
	Get(storeKey string, version int64, key []byte) ([]byte, error)
	Has(storeKey string, version int64, key []byte) (bool, error)
	Iterator(storeKey string, version int64, start, end []byte) (DBIterator, error)
	ReverseIterator(storeKey string, version int64, start, end []byte) (DBIterator, error)
	RawIterate(storeKey string, fn func([]byte, []byte, int64) bool) (bool, error)
	GetLatestVersion() int64
	SetLatestVersion(version int64) error
	GetEarliestVersion() int64
	SetEarliestVersion(version int64, ignoreVersion bool) error
	GetLatestMigratedKey() ([]byte, error)
	GetLatestMigratedModule() (string, error)
	WriteBlockRangeHash(storeKey string, beginBlockRange, endBlockRange int64, hash []byte) error

	// ApplyCommitHash computes the DB state hash for these changesets and returns
	// the meta key-value pairs that should be persisted alongside the state KV pairs.
	// If lastFlushValueGetter is non-nil, it will be used to retrieve last flush values
	// for LtHash delta computation instead of reading from the DB. This ensures determinism
	// when SS writes are asynchronous and may not yet be visible.
	ApplyCommitHash(version int64, changesets []*proto.NamedChangeSet, lastFlushValueGetter LastFlushValueGetter) (StateHash, []KVPair)
	// ApplyCommitHashWithTimings is like ApplyCommitHash but also returns timing breakdown.
	ApplyCommitHashWithTimings(version int64, changesets []*proto.NamedChangeSet, lastFlushValueGetter LastFlushValueGetter) (StateHash, *LtHashTimings, []KVPair)
	// GetCommitHash return the latest DB commit hash.
	GetCommitHash() StateHash
	// GetLtHash returns a copy of the current LtHash vector.
	GetLtHash() LtHasher
	// GetLtHashChecksumAtHeight returns the 32-byte LtHash checksum at a specific block height.
	// It returns nil if the checksum is not available.
	GetLtHashChecksumAtHeight(height uint64) ([]byte, error)

	// ApplyChangesetSync Persist all changeset of a block and bump the latest version
	// the version should be latest version plus one.
	ApplyChangesetSync(version int64, changesets []*proto.NamedChangeSet) error

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

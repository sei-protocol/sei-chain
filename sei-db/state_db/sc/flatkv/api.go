package flatkv

import (
	"errors"
	"io"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// ErrReadOnlyNotSupported is returned when LoadVersion is called with readOnly=true.
// Callers should fall back to Cosmos-only mode when this error is returned.
var ErrReadOnlyNotSupported = errors.New("FlatKV read-only mode not yet supported")

// Exporter streams FlatKV state for snapshots.
// NOTE: Not yet implemented. Will be implemented with state-sync support.
type Exporter interface {
	// Next returns the next key/value pair. Returns (nil, nil, io.EOF) when done.
	Next() (key, value []byte, err error)

	io.Closer
}

// Options configures a FlatKV store.
type Options struct {
	// Dir is the base directory containing
	// account/,
	// code/,
	// storage/,
	// changelog/,
	// __metadata.
	Dir string
}

// Store provides EVM state storage with LtHash integrity.
//
// Lifecycle: NewCommitStore (create) → LoadVersion (open) → ApplyChangeSets/Commit → Close.
// Write path: ApplyChangeSets (buffer) → Commit (persist).
// Read path: Get/Has/Iterator read committed state only.
// Key format: x/evm memiavl keys (mapped internally to account/code/storage DBs).
type Store interface {
	// LoadVersion opens the database at the specified version.
	// Note: FlatKV only stores latest state, so targetVersion is for verification only.
	// readOnly=true is NOT YET SUPPORTED and returns an error (requires snapshot implementation).
	LoadVersion(targetVersion int64, readOnly bool) (Store, error)

	// ApplyChangeSets buffers EVM changesets (x/evm memiavl keys) and updates LtHash.
	// Non-EVM modules are ignored. Call Commit to persist.
	ApplyChangeSets(cs []*proto.NamedChangeSet) error

	// Commit persists buffered writes and advances the version.
	Commit() (int64, error)

	// Get returns the value for the x/evm memiavl key, or (nil, false) if not found.
	Get(key []byte) ([]byte, bool)

	// Has reports whether the x/evm memiavl key exists.
	Has(key []byte) bool

	// Iterator returns an iterator over [start, end) in memiavl key order.
	// Pass nil for unbounded.
	Iterator(start, end []byte) Iterator

	// IteratorByPrefix iterates all keys with the given prefix (more efficient than Iterator).
	// Currently only supports: StateKeyPrefix||addr (storage iteration).
	// Account/code iteration will be added with state-sync support.
	IteratorByPrefix(prefix []byte) Iterator

	// RootHash returns the 32-byte checksum of the working LtHash.
	// Note: This is the Blake3-256 digest of the underlying 2048-byte
	// raw LtHash vector.
	RootHash() []byte

	// Version returns the latest committed version.
	Version() int64

	// Exporter creates an exporter for the given version (0 = current).
	Exporter(version int64) (Exporter, error)

	// WriteSnapshot writes a complete snapshot to dir.
	WriteSnapshot(dir string) error

	// Rollback restores state to targetVersion. Not implemented.
	Rollback(targetVersion int64) error

	io.Closer
}

// Iterator provides ordered iteration over EVM keys.
// Follows PebbleDB semantics: not positioned on creation.
//
// Keys are returned in internal format (without memiavl prefix).
// Concrete implementations (e.g. dbIterator) expose Kind() for callers
// that need to distinguish key types.
type Iterator interface {
	Domain() (start, end []byte)
	Valid() bool
	Error() error
	Close() error

	First() bool
	Last() bool
	SeekGE(key []byte) bool
	SeekLT(key []byte) bool
	Next() bool
	Prev() bool

	// Key returns the current key in internal format (valid until next move).
	// Internal formats:
	//   - Storage: addr(20) || slot(32)
	//   - Nonce/Code/CodeHash: addr(20)
	Key() []byte

	// Value returns the current value (valid until next move).
	Value() []byte
}

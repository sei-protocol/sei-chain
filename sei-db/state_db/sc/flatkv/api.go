package flatkv

import (
	"io"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// Exporter streams FlatKV state (in x/evm memiavl key format) for snapshots.
type Exporter interface {
	// Next returns the next key/value pair. Returns (nil, nil, io.EOF) when done.
	Next() (key, value []byte, err error)

	io.Closer
}

// Options configures a FlatKV store.
type Options struct {
	// Dir is the base directory containing
	// accounts/,
	// code/,
	// storage/,
	// changelog/,
	// __metadata.
	Dir string
}

// Store provides EVM state storage with LtHash integrity.
//
// Write path: ApplyChangeSets (buffer) → Commit (persist).
// Read path: Get/Has/Iterator read committed state only.
// Key format: x/evm memiavl keys (mapped internally to account/code/storage DBs).
type Store interface {
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
	//
	// Multiplexes across internal DBs to return keys in standard memiavl prefix order:
	//   0x03 (Storage), 0x07 (Code), 0x08 (CodeHash), 0x09 (CodeSize), 0x0a (Nonce).
	Iterator(start, end []byte) Iterator

	// IteratorByPrefix iterates all keys with the given prefix (more efficient than Iterator).
	// Supported: StateKeyPrefix||addr, NonceKeyPrefix, CodeKeyPrefix.
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

// Iterator provides ordered iteration over EVM keys (memiavl format).
// Follows PebbleDB semantics: not positioned on creation.
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

	// Key returns the current key (valid until next move).
	Key() []byte

	// Value returns the current value (valid until next move).
	Value() []byte
}

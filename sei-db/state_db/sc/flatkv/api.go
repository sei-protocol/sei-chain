package flatkv

import (
	"io"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// Exporter streams FlatKV state for snapshot export.
type Exporter interface{}

// Options configures a FlatKV store.
type Options struct {
	// Dir is the base directory. Layout:
	//   data/       - PebbleDB data
	//   changelog/  - FlatKV changelog
	//   metadata    - commit point (version + LtHash)
	Dir string
}

// Store provides EVM state storage with LtHash integrity.
//
// Write path: ApplyChangeSets (buffer) → Commit (persist).
// Read path: Get/Has/Iterator read committed state only.
type Store interface {
	// ApplyChangeSets buffers EVM changesets and updates the working LtHash.
	// Non-EVM modules are ignored. Call Commit to persist.
	ApplyChangeSets(cs []*proto.NamedChangeSet) error

	// Commit persists buffered writes and advances the committed version.
	Commit(version int64) error

	// Get returns the value for key, or (nil, false) if not found.
	Get(key Key) ([]byte, bool)

	// Has reports whether key exists.
	Has(key Key) bool

	// Iterator returns an iterator over [start, end).
	// Pass Key{} for unbounded. Not positioned; call First/SeekGE to start.
	Iterator(start, end Key) Iterator

	// RootHash returns the working LtHash (2048 bytes, in-memory).
	RootHash() []byte

	// Version returns the latest committed version.
	Version() (int64, error)

	// Exporter creates an exporter for the given version (0 = current).
	Exporter(version int64) (Exporter, error)

	// WriteSnapshot writes a complete snapshot to dir.
	WriteSnapshot(dir string) error

	// Rollback restores state to targetVersion. Not implemented.
	Rollback(targetVersion int64) error

	io.Closer
}

// Iterator provides ordered iteration over FlatKV keys.
// Follows PebbleDB semantics: not positioned on creation.
type Iterator interface {
	// Domain returns [start, end) bounds.
	Domain() (start, end []byte)

	Valid() bool
	Error() error
	Close() error

	// Positioning
	First() bool
	Last() bool
	SeekGE(key Key) bool // first key >= key
	SeekLT(key Key) bool // last key < key

	// Movement
	Next() bool
	Prev() bool

	// Data (valid until next positioning/movement call)
	Key() []byte
	Value() []byte
}

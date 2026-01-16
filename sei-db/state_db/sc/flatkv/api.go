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
	//   accounts/   - Account DB (address -> account record)
	//   code/       - Code DB (codehash -> bytecode)
	//   storage/    - Storage DBs (sharded; key is address+slot)
	//   raw/        - Raw DB (fallback; optional)
	//   changelog/  - FlatKV changelog (best-effort in Phase 1)
	//   metadata    - commit point (version + LtHash)
	Dir string
}

// Store provides EVM state storage with LtHash integrity.
//
// Write path: ApplyChangeSets (buffer) → Commit (persist).
// Read path: Accounts/Code/Storage/Raw read committed state only.
type Store interface {
	// ApplyChangeSets buffers EVM changesets and updates the working LtHash.
	// Non-EVM modules are ignored. Call Commit to persist.
	ApplyChangeSets(cs []*proto.NamedChangeSet) error

	// Commit persists buffered writes and advances the committed version.
	Commit(version int64) error

	// Accounts exposes the committed account state (address -> record).
	Accounts() AccountStore

	// Code exposes committed contract bytecode (codehash -> bytecode).
	Code() CodeStore

	// Storage exposes committed contract storage (sharded; address+slot -> value).
	Storage() StorageStore

	// Raw is a fallback namespace for entries FlatKV does not model explicitly.
	Raw() RawStore

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

// AccountStore provides committed reads over the account DB.
type AccountStore interface {
	Get(addr Address) (AccountValue, bool)
	Has(addr Address) bool
	Iterator(start, end AccountKey) AccountIterator
}

// CodeStore provides committed reads over the code DB.
type CodeStore interface {
	Get(codeHash CodeHash) ([]byte, bool)
	Has(codeHash CodeHash) bool
	Iterator(start, end CodeKey) CodeIterator
}

// StorageStore provides committed reads over the sharded storage DB.
//
// Keys and values are fixed-size 32-byte words at the EVM level.
type StorageStore interface {
	Get(addr Address, slot Slot) (Word, bool)
	Has(addr Address, slot Slot) bool
	Iterator(start, end StorageKey) StorageIterator
}

// RawStore provides committed reads over the raw DB.
// Raw keys and values are opaque.
type RawStore interface {
	Get(key []byte) ([]byte, bool)
	Has(key []byte) bool
	Iterator(start, end []byte) RawIterator
}

// AccountIterator provides ordered iteration over account keys.
// Follows PebbleDB semantics: not positioned on creation.
type AccountIterator interface {
	Domain() (start, end []byte)
	Valid() bool
	Error() error
	Close() error

	First() bool
	Last() bool
	SeekGE(key AccountKey) bool
	SeekLT(key AccountKey) bool
	Next() bool
	Prev() bool

	// Key and raw value bytes are valid until the next movement call.
	Key() Address
	Value() []byte
}

// CodeIterator provides ordered iteration over code keys.
// Follows PebbleDB semantics: not positioned on creation.
type CodeIterator interface {
	Domain() (start, end []byte)
	Valid() bool
	Error() error
	Close() error

	First() bool
	Last() bool
	SeekGE(key CodeKey) bool
	SeekLT(key CodeKey) bool
	Next() bool
	Prev() bool

	Key() CodeHash
	Value() []byte
}

// StorageIterator provides ordered iteration over storage keys.
// Follows PebbleDB semantics: not positioned on creation.
type StorageIterator interface {
	Domain() (start, end []byte)
	Valid() bool
	Error() error
	Close() error

	First() bool
	Last() bool
	SeekGE(key StorageKey) bool
	SeekLT(key StorageKey) bool
	Next() bool
	Prev() bool

	Key() (addr Address, slot Slot)
	Value() []byte
}

// RawIterator provides ordered iteration over raw keys.
// Follows PebbleDB semantics: not positioned on creation.
type RawIterator interface {
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

	Key() []byte
	Value() []byte
}

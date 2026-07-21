package giga

import "github.com/sei-protocol/sei-chain/sei-db/proto"

const (
	AddressLen = 20
	HashLen    = 32
)

// Address is an EVM address (20 bytes). It intentionally avoids depending on
// go-ethereum/evmc types since sei-db is a generic storage layer; callers can
// convert to/from common.Address or evmc.Address, which share the same
// underlying [20]byte layout.
type Address [AddressLen]byte

// Hash is a 256-bit value (32 bytes), used here for storage slots, balances,
// code hashes, and block hashes. Like Address, it can be freely converted
// to/from common.Hash or evmc.Hash.
type Hash [HashLen]byte

// GigaStateStore is the top-level storage API used by the Giga EVM executor for read and write.
// Reads can be served either for the current (in-progress) block or for a past already committed block.
type GigaStateStore interface {

	// CommitStateChanges write to both SC and SS.
	// To be called after executing each block to commit the state changes.
	// The actual hash computation and disk write could happen asynchronously.
	// But the state changes should take effect immediately when this function returns.
	CommitStateChanges(blockNum int64, changeset []*proto.NamedChangeSet) error

	// GetCurrentStateSnapshot returns with a read-only StateSnapshot (backed by ephemeral snapshot from SC)
	// which represents the state for the current block.
	GetCurrentStateSnapshot() StateSnapshot

	// GetHistoricalStateSnapshot returns a read-only StateSnapshot (backed by SS store)
	// And a bool whether found or not.
	GetHistoricalStateSnapshot(blockNum int64) (StateSnapshot, bool)
}

// StateSnapshot is a read-only, point-in-time view over the store's raw
// key/value data, plus (via the embedded EVMStateSnapshot) EVM-specific
// accessors for account/storage/code/balance reads. Snapshots
// are reference-counted via Reserve/Release so that the underlying
// resources (e.g. an ephemeral SC snapshot or a pinned SS version) stay
// alive for as long as the snapshot is in use, even concurrently with
// later writes/commits against the store.
type StateSnapshot interface {
	EVMStateSnapshot

	// GetBlockHeight returns the block height of this snapshot.
	GetBlockHeight() int64

	// Get returns the raw value stored under key in this snapshot, and
	// whether it was found. It never observes writes made after the
	// snapshot was taken.
	Get(key []byte) ([]byte, bool)

	// Reserve pins the snapshot, incrementing its reference count so its
	// underlying resources are not reclaimed while still in use. Must be
	// paired with a corresponding call to Release.
	Reserve()

	// Release unpins the snapshot, decrementing its reference count.
	// Once the last reference is released, the underlying resources may
	// be reclaimed. Must only be called after a matching Reserve.
	Release()
}

// EVMStateSnapshot is the EVM-specific read surface embedded by
// StateSnapshot. Its method set mirrors the read-only subset of
// evmc.HostContext (see giga/executor/internal/host_context.go), so
// implementations can be adapted to it by simple type conversion between
// Address/Hash and evmc.Address/evmc.Hash.
type EVMStateSnapshot interface {

	// AccountExists reports whether addr has an account in state,
	// including accounts that have self-destructed in the current block.
	AccountExists(addr Address) bool

	// GetStorage returns the value stored at key in addr's storage.
	// Returns the zero Hash if the slot is unset.
	GetStorage(addr Address, key Hash) Hash

	// GetBalance returns addr's balance, as a 256-bit big-endian value.
	GetBalance(addr Address) Hash

	// GetCodeSize returns the length in bytes of addr's contract code.
	// Returns 0 for accounts with no code.
	GetCodeSize(addr Address) int

	// GetCodeHash returns the hash of addr's contract code. Returns the
	// zero Hash for accounts with no code.
	GetCodeHash(addr Address) Hash

	// GetCode returns addr's contract code. Returns nil/empty for
	// accounts with no code.
	GetCode(addr Address) []byte

	// GetBlockHash returns the hash of the block at the given height.
	// Returns the zero Hash if the height is out of the available range.
	GetBlockHash(number int64) Hash
}

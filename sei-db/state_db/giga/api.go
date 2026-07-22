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

// Store is the top-level API used by the Giga EVM executor for
// read and write. Writes commit into both SC and SS; reads can be served for
// the current (in-progress) block or for a past, already-committed block.
type Store interface {

	// CommitStateChanges writes the given changesets into both SC and SS.
	// Call after executing each block. Hash computation and disk I/O may
	// continue asynchronously, but the state changes must be visible to
	// subsequent reads as soon as this function returns.
	CommitStateChanges(blockNum int64, changeset []*proto.NamedChangeSet) error

	// OpenSnapshot returns a read-only StateSnapshot of the current block
	// (backed by an SC ephemeral snapshot). The caller must Close it when done.
	OpenSnapshot() StateSnapshot

	// OpenSnapshotAt returns a read-only StateSnapshot for the given
	// committed block height (backed by SS). The bool is false when no
	// snapshot exists at that height. When true, the caller must Close the
	// returned snapshot when done.
	OpenSnapshotAt(blockNum int64) (StateSnapshot, bool)
}

// StateSnapshot is a read-only, point-in-time view over the store's raw
// key/value data, plus (via the embedded EVMStateSnapshot) EVM-specific
// accessors for account/storage/code/balance/block-hash reads.
//
// Until Close, the underlying resources (e.g. an ephemeral SC snapshot or a pinned SS
// version) stay alive, even concurrently with later writes/commits.
type StateSnapshot interface {
	EVMStateSnapshot

	// GetBlockHeight returns the block height of this snapshot.
	GetBlockHeight() int64

	// Get returns the raw value stored under key in this snapshot, and
	// whether it was found. It never observes writes made after the
	// snapshot was taken.
	//
	// Get does not return an error: internal database failures are expected
	// to panic so the process crashes rather than continuing with corrupt or incomplete state.
	Get(key []byte) ([]byte, bool)

	// Close releases the snapshot's underlying ref counting.
	// Caller is required to Close the snapshot after using it.
	// Not closing snapshot properly could lead to memory leak.
	Close()
}

// EVMStateSnapshot is the EVM-specific read surface embedded by StateSnapshot.
//
// None of these methods return an error: any underlying database failure
// is expected to panic so the process crashes rather than continuing with
// corrupt or incomplete state.
type EVMStateSnapshot interface {

	// AccountExists reports whether addr has an account in state,
	// including accounts that have self-destructed in the current block.
	// Panics on underlying database errors.
	AccountExists(addr Address) bool

	// GetStorage returns the value stored at key in addr's storage.
	// Returns the zero Hash if the slot is unset.
	// Panics on underlying database errors.
	GetStorage(addr Address, key Hash) Hash

	// GetBalance returns addr's balance, as a 256-bit big-endian value.
	// Panics on underlying database errors.
	GetBalance(addr Address) Hash

	// GetCodeSize returns the length in bytes of addr's contract code.
	// Returns 0 for accounts with no code.
	// Panics on underlying database errors.
	GetCodeSize(addr Address) int

	// GetCodeHash returns the hash of addr's contract code. Returns the
	// zero Hash for accounts with no code.
	// Panics on underlying database errors.
	GetCodeHash(addr Address) Hash

	// GetCode returns addr's contract code. Returns nil/empty for
	// accounts with no code.
	// Panics on underlying database errors.
	GetCode(addr Address) []byte

	// GetBlockHash returns the hash of the block at the given height.
	// Returns the zero Hash if the height is out of the available range.
	// Panics on underlying database errors.
	GetBlockHash(number int64) Hash
}

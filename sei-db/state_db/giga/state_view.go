package giga

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
// and code hashes. Like Address, it can be freely converted to/from common.Hash or evmc.Hash.
type Hash [HashLen]byte

// StateView is a read-only, point-in-time view over the store's raw
// key/value data, plus (via the embedded EVMStateView) EVM-specific
// accessors for account/storage/code/balance/nonce reads.
//
// Until Close, the underlying resources (e.g. an ephemeral SC snapshot or a
// pinned SS version) stay alive, even concurrently with later writes/commits.
type StateView interface {
	EVMStateView

	// GetBlockHeight returns the block height of this view.
	GetBlockHeight() int64

	// Get returns the raw value stored under key in this view, and
	// whether it was found. It never observes writes made after the
	// view was opened.
	//
	// Get does not return an error: internal database failures are expected
	// to panic so the process crashes rather than continuing with corrupt or incomplete state.
	Get(module string, key []byte) ([]byte, bool)

	// Close releases the view's underlying ref counting.
	// Caller is required to Close the view after using it.
	// Not closing the view properly could lead to memory leak.
	Close()
}

// EVMStateView is the EVM-specific read surface embedded by StateView.
//
// None of these methods return an error: any underlying database failure
// is expected to panic so the process crashes rather than continuing with
// corrupt or incomplete state.
type EVMStateView interface {

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

	// GetNonce returns addr's account nonce. Returns 0 if unset / the
	// account does not exist.
	// Panics on underlying database errors.
	GetNonce(addr Address) uint64

	// GetCodeSize returns the length in bytes of addr's contract code.
	// Returns 0 for accounts with no code.
	// Panics on underlying database errors.
	GetCodeSize(addr Address) int

	// GetCodeHash returns the hash of addr's contract code.
	// Returns the empty-code hash (keccak256("")) for existing accounts
	// with no code (e.g. EOAs with balance/nonce), and the zero Hash for
	// accounts that do not exist. Matches EXTCODEHASH / keeper.GetCodeHash.
	// Panics on underlying database errors.
	GetCodeHash(addr Address) Hash

	// GetCode returns addr's contract code. Returns nil/empty for
	// accounts with no code.
	// Panics on underlying database errors.
	GetCode(addr Address) []byte
}

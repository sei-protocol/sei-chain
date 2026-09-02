package giga

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Address is an EVM account address.
type Address = common.Address

// Hash is a 256-bit value used for storage slots, balances, and code hashes.
type Hash = common.Hash

// EmptyCodeHash is keccak256 of the empty byte string, the code hash EVM semantics assign to an
// account that exists and holds no code.
var EmptyCodeHash = crypto.Keccak256Hash(nil)

// StateView is a read-only, point-in-time view over the store's raw key/value data, plus (via the
// embedded EVMStateView) EVM-specific accessors for account/storage/code/balance/nonce reads.
//
// Until Close, the view's underlying resources stay alive, even as later blocks are written. A view is
// thread safe with respect to updates to the store it came from, and with respect to other views.
// Close is the exception: it drops the view's reference, so a read racing it races the reclamation of
// the resource being read.
//
// There are no recoverable errors. Any error or panic from a view is fatal, and halting is the
// caller's responsibility: on the first one the caller must stop rather than proceed on state the view
// cannot vouch for.
//
// Every byte slice passed into or received from a view method must be treated as immutable: safe to
// read, never safe to modify in place.
type StateView interface {
	EVMStateView

	// GetBlockHeight returns the block height of this view.
	GetBlockHeight() int64

	// Get returns the value stored under key in this view, and whether it
	// was found. It never observes writes made after the view was opened.
	//
	// Get reports what is stored, not what EVM semantics substitute for it: a code-hash key for an
	// account that exists with no code is not found, where GetCodeHash reports EmptyCodeHash.
	Get(module string, key []byte) ([]byte, bool)

	// Close releases the view's underlying ref counting.
	// Caller is required to Close the view after using it.
	// Not closing the view properly could lead to memory leak.
	Close()
}

// EVMStateView is the EVM-specific read surface embedded by StateView, and carries the same contract.
type EVMStateView interface {

	// AccountExists reports whether addr has an account in state,
	// including accounts that have self-destructed in the current block.
	AccountExists(addr Address) bool

	// GetStorage returns the value stored at key in addr's storage.
	// Returns the zero Hash if the slot is unset.
	GetStorage(addr Address, key Hash) Hash

	// GetBalance returns addr's balance, as a 256-bit big-endian value.
	GetBalance(addr Address) Hash

	// GetNonce returns addr's account nonce. Returns 0 if unset / the
	// account does not exist.
	GetNonce(addr Address) uint64

	// GetCodeSize returns the length in bytes of addr's contract code.
	// Returns 0 for accounts with no code.
	GetCodeSize(addr Address) int

	// GetCodeHash returns the hash of addr's contract code.
	// Returns EmptyCodeHash for an account that exists with no code, and the zero
	// Hash for an account that does not exist or has been deleted.
	// Matches EXTCODEHASH / keeper.GetCodeHash.
	GetCodeHash(addr Address) Hash

	// GetCode returns addr's contract code. Returns nil/empty for
	// accounts with no code.
	GetCode(addr Address) []byte
}

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
	Get(module string, key []byte) ([]byte, bool)

	// Close releases the view's underlying ref counting.
	// Caller is required to Close the view after using it.
	// Not closing the view properly could lead to memory leak.
	Close()
}

// EVMStateView is the EVM-specific read surface embedded by StateView, and carries the same contract.
//
// Every getter reports whether the state entry it reads was found. The bool speaks to that entry's
// existence and not to its contents: an account that exists with a nonce of 0 reads as (0, true).
// A getter that reports false returns the zero value, so a caller with no use for the distinction can
// discard the bool and read a missing entry as zero.
type EVMStateView interface {

	// AccountExists reports whether addr has an account in state,
	// including accounts that have self-destructed in the current block.
	AccountExists(addr Address) bool

	// GetStorage returns the value stored at key in addr's storage, and whether that slot is set.
	GetStorage(addr Address, key Hash) (Hash, bool)

	// GetBalance returns addr's balance as a 256-bit big-endian value, and whether a balance is
	// stored for addr.
	GetBalance(addr Address) (Hash, bool)

	// GetNonce returns addr's account nonce, and whether addr has an account.
	GetNonce(addr Address) (uint64, bool)

	// GetCodeSize returns the length in bytes of addr's contract code, and whether addr has code.
	GetCodeSize(addr Address) (int, bool)

	// GetCodeHash returns the hash of addr's contract code, and whether a code hash is stored for
	// addr. An account that exists and holds no code stores no code hash; EVM semantics answer
	// EmptyCodeHash for that case, which the caller must substitute for itself.
	GetCodeHash(addr Address) (Hash, bool)

	// GetCode returns addr's contract code, and whether addr has code.
	GetCode(addr Address) ([]byte, bool)
}

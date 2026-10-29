package types

import (
	"bytes"
	"math/big"
)

// StateChangeSet is a block's state output: one entry per account the block changed and per slot it wrote,
// in address order and then slot order, plus one entry per non-EVM key it wrote. Values are post-block
// values, not deltas. A set is never modified once produced, so any holder may keep it.
type StateChangeSet struct {
	Accounts []AccountChange
	Storage  []StorageChange
	Misc     []MiscChange
}

// AccountState is an account's balance, nonce and code. A nil balance is zero. Code is immutable:
// it may be shared, and must never be modified in place.
type AccountState struct {
	Balance *big.Int
	Nonce   uint64
	Code    []byte
}

// AccountChange is an account whose balance, nonce or code the block changed: its whole state after
// the block beside its whole state before it.
type AccountChange struct {
	Address Address
	Account AccountState
	Prior   AccountState
}

type StorageChange struct {
	Address    Address
	Key        Hash
	Delete     bool
	Value      Hash
	PriorValue Hash
}

// MiscChange is a write to a key outside the EVM module. Each key appears at most once in a set.
//
// No prior value is stored because nothing consumes it yet. One can be added if a consumer needs it.
type MiscChange struct {
	// The module the key belongs to. Never the EVM module.
	Module string
	// The key within the module.
	Key []byte
	// The value written. Ignored when Delete is set.
	Value []byte
	// Whether the key is deleted.
	Delete bool
}

// BalanceChanged reports whether the block changed the account's balance.
func (c *AccountChange) BalanceChanged() bool {
	return orZero(c.Account.Balance).Cmp(orZero(c.Prior.Balance)) != 0
}

// NonceChanged reports whether the block changed the account's nonce.
func (c *AccountChange) NonceChanged() bool {
	return c.Account.Nonce != c.Prior.Nonce
}

// CodeChanged reports whether the block changed the account's code.
func (c *AccountChange) CodeChanged() bool {
	return !bytes.Equal(c.Account.Code, c.Prior.Code)
}

// IsEmpty reports whether the set would change nothing.
func (c *StateChangeSet) IsEmpty() bool {
	return len(c.Accounts) == 0 && len(c.Storage) == 0 && len(c.Misc) == 0
}

// Reset empties the set, keeping its slices' capacity for reuse.
func (c *StateChangeSet) Reset() {
	clear(c.Accounts)
	c.Accounts = c.Accounts[:0]
	clear(c.Storage)
	c.Storage = c.Storage[:0]
	clear(c.Misc)
	c.Misc = c.Misc[:0]
}

// zeroBalance is the balance orZero reads in place of nil. It must never be modified.
var zeroBalance = new(big.Int)

// orZero returns v, or zeroBalance when v is nil.
func orZero(v *big.Int) *big.Int {
	if v == nil {
		return zeroBalance
	}
	return v
}

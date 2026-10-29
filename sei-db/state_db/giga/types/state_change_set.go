package types

import (
	"bytes"
	"math/big"
)

// StateChangeSet is the deterministic EVM-native state output for a block: one entry per account the
// block changed and per slot it wrote, in address order and then slot order. Values are post-block
// values, not deltas, each beside the value it replaced.
type StateChangeSet struct {
	Accounts []AccountChange
	Storage  []StorageChange
}

// AccountState is an account's balance, nonce and code. A nil balance is zero. Code is immutable:
// it may be shared, and must never be modified in place.
type AccountState struct {
	Balance *big.Int
	Nonce   uint64
	Code    []byte
}

// IsEmpty reports whether the account has no balance, nonce or code, which is an account that does
// not exist.
func (s *AccountState) IsEmpty() bool {
	return orZero(s.Balance).Sign() == 0 && s.Nonce == 0 && len(s.Code) == 0
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
	return len(c.Accounts) == 0 && len(c.Storage) == 0
}

// Clone returns a copy of the set that shares nothing mutable with it, nil for a nil set.
func (c *StateChangeSet) Clone() *StateChangeSet {
	if c == nil {
		return nil
	}
	cp := &StateChangeSet{
		Accounts: make([]AccountChange, len(c.Accounts)),
		Storage:  append([]StorageChange(nil), c.Storage...),
	}
	for i, change := range c.Accounts {
		change.Account.Balance = cloneBig(change.Account.Balance)
		change.Prior.Balance = cloneBig(change.Prior.Balance)
		cp.Accounts[i] = change
	}
	return cp
}

// Reset empties the set, keeping its slices' capacity for reuse.
func (c *StateChangeSet) Reset() {
	clear(c.Accounts)
	c.Accounts = c.Accounts[:0]
	clear(c.Storage)
	c.Storage = c.Storage[:0]
}

// cloneBig copies v, mapping nil to zero.
func cloneBig(v *big.Int) *big.Int {
	if v == nil {
		return new(big.Int)
	}
	return new(big.Int).Set(v)
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

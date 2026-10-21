package types

import "math/big"

// StateChangeSet is the deterministic EVM-native state output for a block.
// Values are post-block values, not deltas, each beside the value it replaced.
// Store-specific encoders must retain StorageClears as prefix-clear operations
// and apply them before Storage.
type StateChangeSet struct {
	Balances      []BalanceChange
	Nonces        []NonceChange
	Code          []CodeChange
	StorageClears []Address
	Storage       []StorageChange
}

type BalanceChange struct {
	Address      Address
	Balance      *big.Int
	PriorBalance *big.Int
}

type NonceChange struct {
	Address    Address
	Nonce      uint64
	PriorNonce uint64
}

type CodeChange struct {
	Address   Address
	Delete    bool
	Code      []byte
	PriorCode []byte
}

type StorageChange struct {
	Address    Address
	Key        Hash
	Delete     bool
	Value      Hash
	PriorValue Hash
}

// IsEmpty reports whether the set would change nothing.
func (c *StateChangeSet) IsEmpty() bool {
	return len(c.Balances) == 0 && len(c.Nonces) == 0 && len(c.Code) == 0 &&
		len(c.StorageClears) == 0 && len(c.Storage) == 0
}

// Clone returns a deep copy of the set, nil for a nil set.
func (c *StateChangeSet) Clone() *StateChangeSet {
	if c == nil {
		return nil
	}
	cp := &StateChangeSet{
		Balances:      make([]BalanceChange, len(c.Balances)),
		Nonces:        append([]NonceChange(nil), c.Nonces...),
		Code:          make([]CodeChange, len(c.Code)),
		StorageClears: append([]Address(nil), c.StorageClears...),
		Storage:       append([]StorageChange(nil), c.Storage...),
	}
	for i, change := range c.Balances {
		change.Balance = cloneBig(change.Balance)
		change.PriorBalance = cloneBig(change.PriorBalance)
		cp.Balances[i] = change
	}
	for i, change := range c.Code {
		change.Code = append([]byte(nil), change.Code...)
		change.PriorCode = append([]byte(nil), change.PriorCode...)
		cp.Code[i] = change
	}
	return cp
}

// Reset empties the set, keeping its slices' capacity for reuse.
func (c *StateChangeSet) Reset() {
	clear(c.Balances)
	c.Balances = c.Balances[:0]
	clear(c.Nonces)
	c.Nonces = c.Nonces[:0]
	clear(c.Code)
	c.Code = c.Code[:0]
	clear(c.StorageClears)
	c.StorageClears = c.StorageClears[:0]
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

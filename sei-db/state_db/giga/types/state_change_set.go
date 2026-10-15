package types

import "math/big"

// TODO: Temporal copy of giga/evmonly/types.go
// Both should be merged into a single package.
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

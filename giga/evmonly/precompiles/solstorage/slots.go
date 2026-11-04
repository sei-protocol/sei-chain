// Package solstorage reads and writes a precompile's state using the storage
// layout solc assigns to the equivalent Solidity contract.
package solstorage

import (
	"encoding/binary"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

// Slot returns the slot numbered n.
func Slot(n uint64) common.Hash {
	var h common.Hash
	binary.BigEndian.PutUint64(h[24:], n)
	return h
}

// Offset returns slot+n, wrapping modulo 2^256 as the EVM does.
func Offset(slot common.Hash, n uint64) common.Hash {
	var v uint256.Int
	v.SetBytes32(slot[:])
	v.Add(&v, uint256.NewInt(n))
	return v.Bytes32()
}

// MapUint64 returns the slot of m[key] for a mapping declared at base.
func MapUint64(base common.Hash, key uint64) common.Hash {
	return MapWord(base, Slot(key))
}

// MapAddress returns the slot of m[key] for a mapping declared at base.
func MapAddress(base common.Hash, key common.Address) common.Hash {
	return MapWord(base, common.BytesToHash(key[:]))
}

// MapWord returns the slot of m[key] for a mapping declared at base whose key
// is a 32-byte value type.
func MapWord(base, key common.Hash) common.Hash {
	return crypto.Keccak256Hash(key[:], base[:])
}

// MapString returns the slot of m[key] for a mapping declared at base whose key
// is a string.
func MapString(base common.Hash, key string) common.Hash {
	return crypto.Keccak256Hash([]byte(key), base[:])
}

// ArrayData returns the slot of element 0 of a dynamic array whose length is
// stored at base.
func ArrayData(base common.Hash) common.Hash {
	return crypto.Keccak256Hash(base[:])
}

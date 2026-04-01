package vtype

import (
	"encoding/binary"
	"fmt"
)

const (
	AddressLen  = 20
	CodeHashLen = 32
	NonceLen    = 8
	SlotLen     = 32
	BalanceLen  = 32
)

// Address is an EVM address (20 bytes).
type Address [AddressLen]byte

// CodeHash is a contract code hash (32 bytes).
type CodeHash [CodeHashLen]byte

// Slot is a storage slot key (32 bytes).
type Slot [SlotLen]byte

// Balance is an EVM balance (32 bytes, big-endian uint256).
type Balance [BalanceLen]byte

// ParseNonce parses a nonce value from a byte slice.
func ParseNonce(b []byte) (uint64, error) {
	if len(b) != NonceLen {
		return 0, fmt.Errorf("invalid nonce value length: got %d, expected %d",
			len(b), NonceLen)
	}
	return binary.BigEndian.Uint64(b), nil
}

// ParseCodeHash parses a codehash value from a byte slice.
func ParseCodeHash(b []byte) (*CodeHash, error) {
	if len(b) != CodeHashLen {
		return nil, fmt.Errorf(
			"invalid codehash value length: got %d, expected %d",
			len(b), CodeHashLen,
		)
	}

	var result CodeHash
	copy(result[:], b)

	return &result, nil
}

// ParseBalance parses a balance value from a byte slice.
func ParseBalance(b []byte) (*Balance, error) {
	if len(b) != BalanceLen {
		return nil, fmt.Errorf("invalid balance value length: got %d, expected %d",
			len(b), BalanceLen,
		)
	}
	var result Balance
	copy(result[:], b)
	return &result, nil
}

// ParseStorageValue parses a storage value from a byte slice.
func ParseStorageValue(b []byte) (*[32]byte, error) {
	if len(b) != SlotLen {
		return nil, fmt.Errorf("invalid storage value length: got %d, expected %d",
			len(b), SlotLen,
		)
	}
	var result [32]byte
	copy(result[:], b)
	return &result, nil
}

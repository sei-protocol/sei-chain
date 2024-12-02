package utils

import (
	"errors"
	"math/big"
)

// SplitTransferBalance splits some amount (maximum of 48 bit) into two parts: the bottom 16 bits and the next 32 bits
func SplitTransferBalance(amount uint64) (uint16, uint32, error) {

	// The maximum transfer amount is 48 bits.
	maxAmount := uint64((1 << 48) - 1)

	if amount > maxAmount {
		return 0, 0, errors.New("amount is too large")
	}

	// Extract the bottom 16 bits (rightmost 16 bits)
	bottom16 := uint16(amount & 0xFFFF)

	// Extract the next 32 bits (from bit 16 to bit 47) (Everything else is ignored since the max is 48 bits)
	next32 := uint32((amount >> 16) & 0xFFFFFFFF)

	return bottom16, next32, nil
}

// CombineTransferAmount combines the bottom 32 bits and the next 48 bits into a single 64-bit number.
func CombineTransferAmount(bottom16 uint16, hi uint32) *big.Int {
	// Combine the bottom 32 bits and the next 48 bits
	combined := (uint64(hi) << 16) | uint64(bottom16)

	return new(big.Int).SetUint64(combined)
}

func CombinePendingBalances(loBits *big.Int, hiBits *big.Int) *big.Int {
	// Shift the hi bits by 16 bits to the left
	hiBits.Lsh(hiBits, 16) // Equivalent to hi << 16

	// Combine by adding hiBig with loBig
	combined := new(big.Int).Add(hiBits, loBits)
	return combined
}

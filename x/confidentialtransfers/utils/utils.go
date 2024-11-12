package utils

import "errors"

// Splits some amount (maximum of 48 bit) into two parts: the bottom 16 bits and the next 32 bits
func SplitTransferBalance(amount uint64) (uint16, uint32, error) {

	if amount > uint64((2<<48)-1) {
		return 0, 0, errors.New("amount is too large")
	}

	// Extract the bottom 16 bits (rightmost 16 bits)
	bottom16 := uint16(amount & 0xFFFF)

	// Extract the next 32 bits (from bit 16 to bit 47) (Everything else is ignored since the max is 48 bits)
	next32 := uint32((amount >> 16) & 0xFFFFFFFF)

	return bottom16, next32, nil
}

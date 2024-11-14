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

// Combines the bottom 32 bits and the next 48 bits into a single 64-bit number.
func CombineTransferAmount(bottom16 uint16, hi uint32) (uint64, error) {
	// Ensure that the inputs are within their respective bit limits
	if bottom16 > 0xFFFF || hi > 0xFFFFFFFF {
		return 0, errors.New("Input values exceed their respective bit limits")
	}

	// Combine the bottom 32 bits and the next 48 bits
	combined := (uint64(hi) << 16) | uint64(bottom16)

	return combined, nil
}

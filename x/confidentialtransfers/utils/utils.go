package utils

import (
	"errors"
	"github.com/armon/go-metrics"
	"math/big"
	"time"
)

// SplitTransferBalance splits some amount (maximum of 48 bit) into two parts: the bottom 16 bits and the next 32 bits
func SplitTransferBalance(amount uint64) (uint16, uint32, error) {
	defer metrics.MeasureSince(
		[]string{"ct", "split", "transfer", "balance", "milliseconds"},
		time.Now().UTC(),
	)
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
func CombineTransferAmount(bottom16 uint16, hi uint32) uint64 {
	defer metrics.MeasureSince(
		[]string{"ct", "combine", "transfer", "amount", "milliseconds"},
		time.Now().UTC(),
	)
	// Combine the bottom 32 bits and the next 48 bits
	combined := (uint64(hi) << 16) | uint64(bottom16)

	return combined
}

func CombinePendingBalances(loBits uint64, hiBits uint64) uint64 {
	defer metrics.MeasureSince(
		[]string{"ct", "combine", "pending", "balance", "milliseconds"},
		time.Now().UTC(),
	)
	loBig := new(big.Int).SetUint64(loBits)
	hiBig := new(big.Int).SetUint64(hiBits)

	// Shift the hi bits by 16 bits to the left
	hiBig.Lsh(hiBig, 16) // Equivalent to hi << 16

	// Combine by adding hiBig with loBig
	combined := new(big.Int).Add(hiBig, loBig)
	return combined.Uint64()
}

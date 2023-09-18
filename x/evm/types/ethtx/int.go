// Copyright Tharsis Labs Ltd.(Evmos)
// SPDX-License-Identifier:ENCL-1.0(https://github.com/evmos/evmos/blob/main/LICENSE)
package ethtx

import (
	"fmt"
	"math"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const maxBitLen = 256

// SafeInt64 checks for overflows while casting a uint64 to int64 value.
func SafeInt64(value uint64) (int64, error) {
	if value > uint64(math.MaxInt64) {
		return 0, fmt.Errorf("uint64 value %v cannot exceed %v", value, int64(math.MaxInt64))
	}

	return int64(value), nil // #nosec G701 -- checked for int overflow already
}

// SafeNewIntFromBigInt constructs Int from big.Int, return error if more than 256bits
func SafeNewIntFromBigInt(i *big.Int) (sdk.Int, error) {
	if !IsValidInt256(i) {
		return sdk.NewInt(0), fmt.Errorf("big int out of bound: %s", i)
	}
	return sdk.NewIntFromBigInt(i), nil
}

// IsValidInt256 check the bound of 256 bit number
func IsValidInt256(i *big.Int) bool {
	return i == nil || i.BitLen() <= maxBitLen
}

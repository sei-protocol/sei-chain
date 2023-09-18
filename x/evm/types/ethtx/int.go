package ethtx

import (
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const maxBitLen = 256

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

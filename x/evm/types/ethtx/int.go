package ethtx

import (
	"math/big"
)

const maxBitLen = 256

// IsValidInt256 check the bound of 256 bit number
func IsValidInt256(i *big.Int) bool {
	return i == nil || i.BitLen() <= maxBitLen
}

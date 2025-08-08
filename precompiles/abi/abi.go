package abi

import (
	"math/big"
)

// U256 is a stubbed version that returns a dummy value and nil error
func U256(input interface{}) (*big.Int, error) {
	return big.NewInt(0), nil
}


package ethtx

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common/math"
)

func EffectiveGasPrice(baseFee, feeCap, tipCap *big.Int) *big.Int {
	return math.BigMin(new(big.Int).Add(tipCap, baseFee), feeCap)
}

package ethtx

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common/math"
)

// Gas price is the smaller of base fee + tip limit vs total fee limit
func EffectiveGasPrice(baseFee, feeCap, tipCap *big.Int) *big.Int {
	return math.BigMin(new(big.Int).Add(tipCap, baseFee), feeCap)
}

func MustSetConvertIfPresent[U comparable, V any](orig U, converter func(U) (V, error), setter func(V)) {
	var nilU U
	if orig == nilU {
		return
	}

	if converted, err := converter(orig); err != nil {
		panic(err)
	} else {
		setter(converted)
	}
}

func SetConvertIfPresent[U comparable, V any](orig U, converter func(U) V, setter func(V)) {
	var nilU U
	if orig == nilU {
		return
	}

	setter(converter(orig))
}

package utils

import (
	"math"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

var Big0 = big.NewInt(0)
var Big1 = big.NewInt(1)
var Big2 = big.NewInt(2)
var Big8 = big.NewInt(8)
var Big27 = big.NewInt(27)
var Big35 = big.NewInt(35)
var BigMaxI64 = big.NewInt(math.MaxInt64)
var BigMaxU64 = new(big.Int).SetUint64(math.MaxUint64)

var Sdk0 = sdk.NewInt(0)

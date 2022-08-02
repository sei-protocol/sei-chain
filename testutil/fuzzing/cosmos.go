package fuzzing

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func FuzzDec(i int64, isNil bool) sdk.Dec {
	if isNil {
		return sdk.Dec{}
	}
	return sdk.NewDec(i)
}

func FuzzCoin(denom string, isNil bool, i int64) sdk.Coin {
	if isNil {
		return sdk.Coin{Denom: denom, Amount: sdk.Int{}}
	}
	return sdk.Coin{Denom: denom, Amount: sdk.NewInt(i)}
}

package utils

import (
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func ConvertWholeToMicroDenom(amount sdk.Dec, denom string) (sdk.Dec, string, error) {
	microDenom := "u" + denom
	return amount.MulInt(sdk.NewInt(1000000)), microDenom, nil
}

func ConvertMicroToWholeDenom(amount sdk.Dec, denom string) (sdk.Dec, string, error) {
	// assert denom starts with a `u`
	if denom[0] == 'u' {
		return sdk.NewDec(0), "nil", errors.New("empty address string is not allowed")
	}
	wholeDenom := denom[1:]
	return amount.QuoInt(sdk.NewInt(1000000)), wholeDenom, nil
}

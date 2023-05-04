package utils

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const ZeroUserProvidedGas = 0

func GetGasMeterForLimit(limit uint64) sdk.GasMeter {
	if limit == 0 {
		return sdk.NewInfiniteGasMeter()
	}
	return sdk.NewGasMeter(limit)
}

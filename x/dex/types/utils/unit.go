package utils

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

var UnitToMultiplier = map[types.Unit]sdk.Dec{
	types.Unit_STANDARD: sdk.OneDec(),
	types.Unit_MILLI:    sdk.NewDec(1_000),
	types.Unit_MICRO:    sdk.NewDec(1_000_000),
	types.Unit_NANO:     sdk.NewDec(1_000_000_000),
}

func ConvertDecToStandard(unit types.Unit, dec sdk.Dec) sdk.Dec {
	return dec.Quo(UnitToMultiplier[unit])
}

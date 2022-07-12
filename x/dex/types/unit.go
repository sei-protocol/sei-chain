package types

import sdk "github.com/cosmos/cosmos-sdk/types"

var UnitToMultiplier = map[Unit]sdk.Dec{
	Unit_STANDARD: sdk.OneDec(),
	Unit_MILLI:    sdk.NewDec(1_000),
	Unit_MICRO:    sdk.NewDec(1_000_000),
	Unit_NANO:     sdk.NewDec(1_000_000_000),
}

func ConvertDecToStandard(unit Unit, dec sdk.Dec) sdk.Dec {
	return dec.Quo(UnitToMultiplier[unit])
}

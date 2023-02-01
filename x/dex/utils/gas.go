package utils

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/tendermint/tendermint/libs/log"
)

const ZeroUserProvidedGas = 0

func GetGasMeterForLimit(limit uint64, logger log.Logger, meterID string) sdk.GasMeter {
	if limit == 0 {
		return sdk.NewInfiniteGasMeterWithLogger(logger, meterID)
	}
	return sdk.NewGasMeterWithLogger(limit, logger, meterID)
}

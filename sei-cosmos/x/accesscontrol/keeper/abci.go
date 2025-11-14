package keeper

import (
	"time"

	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/constants"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
)

func (k *Keeper) EndBlock(ctx sdk.Context) {
	defer telemetry.ModuleMeasureSince(types.ModuleName, time.Now(), telemetry.MetricKeyEndBlocker)
	badWasmDependencyAddresses := ctx.Context().Value(constants.BadWasmDependencyAddressesKey)
	if badWasmDependencyAddresses != nil {
		typedBadWasmDependencyAddresses, ok := badWasmDependencyAddresses.([]sdk.AccAddress)
		if ok && typedBadWasmDependencyAddresses != nil {
			for _, addr := range typedBadWasmDependencyAddresses {
				k.ResetWasmDependencyMapping(ctx, addr, constants.ResetReasonBadWasmDependency)
			}
		}
	}
}

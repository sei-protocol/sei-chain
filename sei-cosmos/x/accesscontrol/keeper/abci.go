package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/constants"
)

func (k *Keeper) EndBlock(ctx sdk.Context) {
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

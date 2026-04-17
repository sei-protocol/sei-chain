package keeper

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"

	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client/types"
)

// GetAllowedClients retrieves the allowed clients from the paramstore
func (k Keeper) GetAllowedClients(ctx sdk.Context) []string {
	var res []string
	k.paramSpace.Get(ctx, types.KeyAllowedClients, &res)
	return res
}

// GetParams returns the total set of ibc-client parameters.
func (k Keeper) GetParams(ctx sdk.Context) types.Params {
	return types.NewParams(k.GetAllowedClients(ctx)...)
}

// SetParams sets the total set of ibc-client parameters.
func (k Keeper) SetParams(ctx sdk.Context, params types.Params) {
	k.paramSpace.SetParamSet(ctx, &params)
}

package keeper

import (
	"github.com/sei-protocol/sei-chain/x/nitro/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetParams returns the total set params.
func (k Keeper) GetParams(ctx sdk.Context) (params types.Params) {
	k.paramSpace.GetParamSet(ctx, &params)
	return params
}

// SetParams sets the total set of params.
func (k Keeper) SetParams(ctx sdk.Context, params types.Params) {
	k.paramSpace.SetParamSet(ctx, &params)
}

func (k Keeper) IsTxSenderWhitelisted(ctx sdk.Context, addr string) bool {
	params := k.GetParams(ctx)
	if params.WhitelistedTxSenders == nil {
		return false
	}
	for _, whitelisted := range params.WhitelistedTxSenders {
		if whitelisted == addr {
			return true
		}
	}
	return false
}

func (k Keeper) IsFraudChallengeEnabled(ctx sdk.Context) bool {
	params := k.GetParams(ctx)
	return params.Enabled
}

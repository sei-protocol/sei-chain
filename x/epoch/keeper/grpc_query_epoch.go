package keeper

import (
	"context"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/epoch/types"
)

func (k Keeper) Epoch(c context.Context, _ *types.QueryEpochRequest) (*types.QueryEpochResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)

	epoch := k.GetEpoch(ctx)
	return &types.QueryEpochResponse{Epoch: epoch}, nil
}

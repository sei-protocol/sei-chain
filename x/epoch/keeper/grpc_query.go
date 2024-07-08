package keeper

import (
	"context"
	"github.com/sei-protocol/sei-chain/x/epoch/types"
)

var _ types.QueryServer = Keeper{}

func (k Keeper) Deprecated_Epoch(ctx context.Context, request *types.QueryEpochRequest) (*types.QueryEpochResponse, error) {
	return k.Epoch(ctx, request)
}

func (k Keeper) Deprecated_Params(ctx context.Context, request *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	return k.Params(ctx, request)
}

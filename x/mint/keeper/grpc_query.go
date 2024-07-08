package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/mint/types"
)

var _ types.QueryServer = Querier{}

// Querier defines a wrapper around the x/mint keeper providing gRPC method
// handlers.
type Querier struct {
	Keeper
}

func NewQuerier(k Keeper) Querier {
	return Querier{Keeper: k}
}

func (q Querier) Deprecated_Params(c context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	return q.Params(c, nil)
}

// Params returns params of the mint module.
func (q Querier) Params(c context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	params := q.Keeper.GetParams(ctx)

	return &types.QueryParamsResponse{Params: params}, nil
}

func (q Querier) Deprecated_Minter(c context.Context, _ *types.QueryMinterRequest) (*types.QueryMinterResponse, error) {
	return q.Minter(c, nil)
}

// Returns the most last mint state
func (q Querier) Minter(c context.Context, _ *types.QueryMinterRequest) (*types.QueryMinterResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	minter := q.Keeper.GetMinter(ctx)
	response := types.QueryMinterResponse(minter)
	return &response, nil
}

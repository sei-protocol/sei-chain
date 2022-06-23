package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/epoch/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (k Keeper) Epoch(c context.Context, req *types.QueryEpochRequest) (*types.QueryEpochResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx := sdk.UnwrapSDKContext(c)

	epoch := k.GetEpoch(ctx)
	return &types.QueryEpochResponse{Epoch: epoch}, nil
}

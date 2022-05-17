package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (k Keeper) GetTwap(goCtx context.Context, req *types.QueryGetTwapRequest) (*types.QueryGetTwapResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	twap := k.GetTwapState(ctx, req.ContractAddr, req.PriceDenom, req.AssetDenom)

	return &types.QueryGetTwapResponse{
		Twaps: &twap,
	}, nil
}

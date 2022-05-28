package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (k Keeper) GetPrice(goCtx context.Context, req *types.QueryGetPriceRequest) (*types.QueryGetPriceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	price, exist := k.GetPriceState(ctx, req.ContractAddr, req.Epoch, req.PriceDenom, req.AssetDenom)
	if !exist {
		return nil, status.Error(codes.NotFound, "not found")
	}

	return &types.QueryGetPriceResponse{
		Price: &price,
	}, nil
}

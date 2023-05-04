package query

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (k KeeperWrapper) GetPrices(goCtx context.Context, req *types.QueryGetPricesRequest) (*types.QueryGetPricesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	prices := k.GetAllPrices(ctx, req.ContractAddr, types.Pair{PriceDenom: req.PriceDenom, AssetDenom: req.AssetDenom})

	return &types.QueryGetPricesResponse{
		Prices: prices,
	}, nil
}

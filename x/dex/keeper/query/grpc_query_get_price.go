package query

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (k KeeperWrapper) GetPrice(goCtx context.Context, req *types.QueryGetPriceRequest) (*types.QueryGetPriceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	price, found := k.GetPriceState(ctx, req.ContractAddr, req.Timestamp, types.Pair{PriceDenom: req.PriceDenom, AssetDenom: req.AssetDenom})

	return &types.QueryGetPriceResponse{
		Price: &price,
		Found: found,
	}, nil
}

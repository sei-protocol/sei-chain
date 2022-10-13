package query

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (k KeeperWrapper) GetLatestPrice(goCtx context.Context, req *types.QueryGetLatestPriceRequest) (*types.QueryGetLatestPriceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)
	blockTimeStamp := uint64(ctx.BlockTime().Unix())

	price, found := k.GetPriceState(ctx, req.ContractAddr, blockTimeStamp, types.Pair{PriceDenom: req.PriceDenom, AssetDenom: req.AssetDenom})

	return &types.QueryGetLatestPriceResponse{
		Price: &price,
		Found: found,
	}, nil
}

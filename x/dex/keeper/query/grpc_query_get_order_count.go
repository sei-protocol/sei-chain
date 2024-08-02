package query

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (k KeeperWrapper) GetOrderCount(c context.Context, req *types.QueryGetOrderCountRequest) (*types.QueryGetOrderCountResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx := sdk.UnwrapSDKContext(c)

	return &types.QueryGetOrderCountResponse{Count: k.GetOrderCountState(ctx, req.ContractAddr, req.PriceDenom, req.AssetDenom, req.PositionDirection, *req.Price)}, nil
}

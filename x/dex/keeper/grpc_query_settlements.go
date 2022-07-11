package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (k Keeper) GetSettlements(c context.Context, req *types.QueryGetSettlementsRequest) (*types.QueryGetSettlementsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(c)

	settlements, found := k.GetSettlementsState(ctx, req.ContractAddr, req.PriceDenom, req.AssetDenom, req.OrderId)
	if !found {
		return &types.QueryGetSettlementsResponse{}, sdkerrors.ErrKeyNotFound
	}

	return &types.QueryGetSettlementsResponse{Settlements: settlements}, nil
}

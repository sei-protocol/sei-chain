package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const MaxSettlementsLimit = 100

func (k Keeper) GetSettlements(c context.Context, req *types.QueryGetSettlementsRequest) (*types.QueryGetSettlementsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(c)

	settlements, found := k.GetSettlementsState(ctx, req.ContractAddr, req.PriceDenom, req.AssetDenom, req.Account, req.OrderId)
	if !found {
		return &types.QueryGetSettlementsResponse{}, sdkerrors.ErrKeyNotFound
	}

	return &types.QueryGetSettlementsResponse{Settlements: settlements}, nil
}

func (k Keeper) GetSettlementsForAccount(c context.Context, req *types.QueryGetSettlementsForAccountRequest) (*types.QueryGetSettlementsForAccountResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(c)

	settlementsList := k.GetSettlementsStateForAccount(ctx, req.ContractAddr, req.PriceDenom, req.AssetDenom, req.Account)

	return &types.QueryGetSettlementsForAccountResponse{SettlementsList: settlementsList}, nil
}

func (k Keeper) GetAllSettlements(c context.Context, req *types.QueryGetAllSettlementsRequest) (*types.QueryGetAllSettlementsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.Limit == 0 {
		req.Limit = MaxSettlementsLimit
	}
	if req.Limit > MaxSettlementsLimit {
		return nil, status.Error(codes.InvalidArgument, "too many values requested")
	}

	ctx := sdk.UnwrapSDKContext(c)

	settlementsList := k.GetAllSettlementsState(ctx, req.ContractAddr, req.PriceDenom, req.AssetDenom, int(req.Limit))

	return &types.QueryGetAllSettlementsResponse{SettlementsList: settlementsList}, nil
}

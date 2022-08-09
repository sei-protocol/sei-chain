package query

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const MaxSettlementsLimit = 100

func (k KeeperWrapper) GetSettlements(c context.Context, req *types.QueryGetSettlementsRequest) (*types.QueryGetSettlementsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(c)

	settlements := k.GetSettlementsState(ctx, req.ContractAddr, req.PriceDenom, req.AssetDenom, req.Account, req.OrderId)

	return &types.QueryGetSettlementsResponse{Settlements: settlements}, nil
}

func (k KeeperWrapper) GetSettlementsForAccount(c context.Context, req *types.QueryGetSettlementsForAccountRequest) (*types.QueryGetSettlementsForAccountResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(c)

	settlementsList := k.GetSettlementsStateForAccount(ctx, req.ContractAddr, req.PriceDenom, req.AssetDenom, req.Account)

	return &types.QueryGetSettlementsForAccountResponse{SettlementsList: settlementsList}, nil
}

func (k KeeperWrapper) GetAllSettlements(c context.Context, req *types.QueryGetAllSettlementsRequest) (*types.QueryGetAllSettlementsResponse, error) {
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

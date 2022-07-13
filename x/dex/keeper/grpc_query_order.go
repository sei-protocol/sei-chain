package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (k Keeper) GetOrderByID(c context.Context, req *types.QueryGetOrderByIDRequest) (*types.QueryGetOrderByIDResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx := sdk.UnwrapSDKContext(c)
	orders := k.GetOrdersByIds(ctx, req.ContractAddr, []uint64{req.Id})
	order, ok := orders[req.Id]
	if !ok {
		return &types.QueryGetOrderByIDResponse{}, status.Error(codes.NotFound, "order not found")
	}
	return &types.QueryGetOrderByIDResponse{
		Order: &order,
	}, nil
}

func (k Keeper) GetOrders(c context.Context, req *types.QueryGetOrdersRequest) (*types.QueryGetOrdersResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx := sdk.UnwrapSDKContext(c)
	activeOrderIds := k.GetAccountActiveOrders(ctx, req.ContractAddr, req.Account)
	orders := k.GetOrdersByIds(ctx, req.ContractAddr, activeOrderIds.Ids)
	response := &types.QueryGetOrdersResponse{Orders: []*types.Order{}}
	for _, order := range orders {
		order := order
		response.Orders = append(response.Orders, &order)
	}
	return response, nil
}

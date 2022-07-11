package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (k Keeper) GetOrderById(c context.Context, req *types.QueryGetOrderByIdRequest) (*types.QueryGetOrderByIdResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx := sdk.UnwrapSDKContext(c)
	orders := k.GetOrdersByIds(ctx, req.ContractAddr, []uint64{req.Id})
	if order, ok := orders[req.Id]; !ok {
		return &types.QueryGetOrderByIdResponse{}, status.Error(codes.NotFound, "order not found")
	} else {
		k.decorateOrderStatus(ctx, &order)
		return &types.QueryGetOrderByIdResponse{
			Order: &order,
		}, nil
	}
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
		k.decorateOrderStatus(ctx, &order)
		response.Orders = append(response.Orders, &order)
	}
	return response, nil
}

func (k Keeper) decorateOrderStatus(c sdk.Context, order *types.Order) {
	cancels := k.GetCancelsByIds(c, order.ContractAddr, []uint64{order.Id})
	if _, ok := cancels[order.Id]; ok {
		order.Status = types.OrderStatus_CANCELLED
		return
	}
	settlements, found := k.GetSettlementsState(c, order.ContractAddr, order.PriceDenom, order.AssetDenom, order.Id)
	if found {
		totalSettledQuantity := sdk.ZeroDec()
		for _, settlement := range settlements.Entries {
			totalSettledQuantity = totalSettledQuantity.Add(settlement.Quantity)
		}
		if totalSettledQuantity.GTE(order.Quantity) {
			order.Status = types.OrderStatus_FULFILLED
		}
	}
}

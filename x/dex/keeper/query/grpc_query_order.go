package query

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

// To be deprecated once offchain query is built
func (k KeeperWrapper) GetOrder(c context.Context, req *types.QueryGetOrderByIDRequest) (*types.QueryGetOrderByIDResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx := sdk.UnwrapSDKContext(c)
	longBooks := k.GetAllLongBook(ctx, req.ContractAddr)
	for _, longBook := range longBooks {
		for _, allocation := range longBook.Entry.Allocations {
			if allocation.OrderId == req.Id {
				return &types.QueryGetOrderByIDResponse{
					Order: &types.Order{
						Id:                req.Id,
						Price:             longBook.Price,
						Quantity:          allocation.Quantity,
						PriceDenom:        longBook.Entry.PriceDenom,
						AssetDenom:        longBook.Entry.AssetDenom,
						OrderType:         types.OrderType_LIMIT,
						Status:            types.OrderStatus_PLACED,
						ContractAddr:      req.ContractAddr,
						PositionDirection: types.PositionDirection_LONG,
						Account:           allocation.Account,
					},
				}, nil
			}
		}
	}
	shortBooks := k.GetAllShortBook(ctx, req.ContractAddr)
	for _, shortBook := range shortBooks {
		for _, allocation := range shortBook.Entry.Allocations {
			if allocation.OrderId == req.Id {
				return &types.QueryGetOrderByIDResponse{
					Order: &types.Order{
						Id:                req.Id,
						Price:             shortBook.Price,
						Quantity:          allocation.Quantity,
						PriceDenom:        shortBook.Entry.PriceDenom,
						AssetDenom:        shortBook.Entry.AssetDenom,
						OrderType:         types.OrderType_LIMIT,
						Status:            types.OrderStatus_PLACED,
						ContractAddr:      req.ContractAddr,
						PositionDirection: types.PositionDirection_SHORT,
						Account:           allocation.Account,
					},
				}, nil
			}
		}
	}

	return &types.QueryGetOrderByIDResponse{}, types.ErrInvalidOrderID
}

// To be deprecated once offchain query is built
func (k KeeperWrapper) GetOrders(c context.Context, req *types.QueryGetOrdersRequest) (*types.QueryGetOrdersResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx := sdk.UnwrapSDKContext(c)
	orders := []*types.Order{}
	longBooks := k.GetAllLongBook(ctx, req.ContractAddr)
	for _, longBook := range longBooks {
		for _, allocation := range longBook.Entry.Allocations {
			if allocation.Account == req.Account {
				orders = append(orders, &types.Order{
					Id:                allocation.OrderId,
					Price:             longBook.Price,
					Quantity:          allocation.Quantity,
					PriceDenom:        longBook.Entry.PriceDenom,
					AssetDenom:        longBook.Entry.AssetDenom,
					OrderType:         types.OrderType_LIMIT,
					Status:            types.OrderStatus_PLACED,
					ContractAddr:      req.ContractAddr,
					PositionDirection: types.PositionDirection_LONG,
					Account:           allocation.Account,
				})
			}
		}
	}
	shortBooks := k.GetAllShortBook(ctx, req.ContractAddr)
	for _, shortBook := range shortBooks {
		for _, allocation := range shortBook.Entry.Allocations {
			if allocation.Account == req.Account {
				orders = append(orders, &types.Order{
					Id:                allocation.OrderId,
					Price:             shortBook.Price,
					Quantity:          allocation.Quantity,
					PriceDenom:        shortBook.Entry.PriceDenom,
					AssetDenom:        shortBook.Entry.AssetDenom,
					OrderType:         types.OrderType_LIMIT,
					Status:            types.OrderStatus_PLACED,
					ContractAddr:      req.ContractAddr,
					PositionDirection: types.PositionDirection_SHORT,
					Account:           allocation.Account,
				})
			}
		}
	}

	return &types.QueryGetOrdersResponse{Orders: orders}, nil
}

package keeper

import (
	"context"

	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k msgServer) CancelOrders(goCtx context.Context, msg *types.MsgCancelOrders) (*types.MsgCancelOrdersResponse, error) {
	_, span := (*k.tracingInfo.Tracer).Start(goCtx, "CancelOrders")
	defer span.End()

	pairToOrderCancellations := k.OrderCancellations[msg.GetContractAddr()]

	for _, orderCancellation := range msg.GetOrderCancellations() {
		pair := types.Pair{PriceDenom: orderCancellation.PriceDenom, AssetDenom: orderCancellation.AssetDenom}
		(*pairToOrderCancellations[pair.String()]).OrderCancellations = append(
			(*pairToOrderCancellations[pair.String()]).OrderCancellations,
			dexcache.OrderCancellation{
				Price:      orderCancellation.Price,
				Quantity:   orderCancellation.Quantity,
				Creator:    msg.Creator,
				PriceDenom: orderCancellation.PriceDenom,
				AssetDenom: orderCancellation.AssetDenom,
				Direction:  orderCancellation.PositionDirection,
				Effect:     orderCancellation.PositionEffect,
				Leverage:   orderCancellation.Leverage,
			},
		)
	}

	return &types.MsgCancelOrdersResponse{}, nil
}

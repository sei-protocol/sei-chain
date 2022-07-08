package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k msgServer) CancelOrders(goCtx context.Context, msg *types.MsgCancelOrders) (*types.MsgCancelOrdersResponse, error) {
	_, span := (*k.tracingInfo.Tracer).Start(goCtx, "CancelOrders")
	defer span.End()

	ctx := sdk.UnwrapSDKContext(goCtx)

	pairToOrderCancellations := k.OrderCancellations[msg.GetContractAddr()]
	
	for _, orderCancellation := range msg.GetOrderCancellations() {
		ticksize, found := k.Keeper.GetTickSizeForPair(ctx,msg.GetContractAddr(), types.Pair{PriceDenom: orderCancellation.PriceDenom, AssetDenom: orderCancellation.AssetDenom})
		if !found {
			return nil, sdkerrors.Wrapf(sdkerrors.ErrKeyNotFound, "the pair {price:%s,asset:%s} has no ticksize configured", orderCancellation.PriceDenom, orderCancellation.AssetDenom)
		}
		pair := types.Pair{PriceDenom: orderCancellation.PriceDenom, AssetDenom: orderCancellation.AssetDenom, Ticksize: &ticksize}
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

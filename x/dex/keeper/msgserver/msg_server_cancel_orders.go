package msgserver

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	typesutils "github.com/sei-protocol/sei-chain/x/dex/types/utils"
)

func (k msgServer) CancelOrders(goCtx context.Context, msg *types.MsgCancelOrders) (*types.MsgCancelOrdersResponse, error) {
	_, span := (*k.tracingInfo.Tracer).Start(goCtx, "CancelOrders")
	defer span.End()

	ctx := sdk.UnwrapSDKContext(goCtx)

	activeOrderIDSet := utils.NewUInt64Set(k.GetAccountActiveOrders(ctx, msg.ContractAddr, msg.Creator).Ids)
	orderMap := k.GetOrdersByIds(ctx, msg.ContractAddr, msg.GetOrderIds())
	for _, orderIDToCancel := range msg.GetOrderIds() {
		if !activeOrderIDSet.Contains(orderIDToCancel) {
			// cannot cancel an order that doesn't exist or is inactive
			continue
		}
		order := orderMap[orderIDToCancel]
		pair := types.Pair{PriceDenom: order.PriceDenom, AssetDenom: order.AssetDenom}
		pairStr := typesutils.GetPairString(&pair)
		pairBlockCancellations := k.MemState.GetBlockCancels(typesutils.ContractAddress(msg.GetContractAddr()), pairStr)
		cancelledInCurrentBlock := false
		for _, cancelInCurrentBlock := range *pairBlockCancellations {
			if cancelInCurrentBlock.Id == orderIDToCancel {
				cancelledInCurrentBlock = true
				break
			}
		}
		if order.Account != msg.Creator {
			// cannot cancel other's orders
			// TODO: add error message in response
			continue
		}
		if !cancelledInCurrentBlock {
			// only cancel if it's not cancelled in a previous tx in the same block
			cancel := types.Cancellation{
				Id:        orderIDToCancel,
				Initiator: types.CancellationInitiator_USER,
				Creator:   msg.Creator,
			}
			pairBlockCancellations.AddCancel(cancel)
		}
	}

	return &types.MsgCancelOrdersResponse{}, nil
}

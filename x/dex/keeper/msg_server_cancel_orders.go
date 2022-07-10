package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k msgServer) CancelOrders(goCtx context.Context, msg *types.MsgCancelOrders) (*types.MsgCancelOrdersResponse, error) {
	_, span := (*k.tracingInfo.Tracer).Start(goCtx, "CancelOrders")
	defer span.End()

	ctx := sdk.UnwrapSDKContext(goCtx)

	activeOrderIdSet := utils.NewUInt64Set(k.GetAccountActiveOrders(ctx, msg.ContractAddr, msg.Creator).Ids)
	orderMap := k.GetOrdersByIds(ctx, msg.ContractAddr, msg.GetOrderIds())
	for _, orderIdToCancel := range msg.GetOrderIds() {
		if !activeOrderIdSet.Contains(orderIdToCancel) {
			// cannot cancel an order that doesn't exist or is inactive
			continue
		}
		order := orderMap[orderIdToCancel]
		pair := types.Pair{PriceDenom: order.PriceDenom, AssetDenom: order.AssetDenom}
		pairStr := types.GetPairString(&pair)
		pairBlockCancellations := k.MemState.GetBlockCancels(types.ContractAddress(msg.GetContractAddr()), pairStr)
		cancelledInCurrentBlock := false
		for _, cancelInCurrentBlock := range *pairBlockCancellations {
			if cancelInCurrentBlock.Id == orderIdToCancel {
				cancelledInCurrentBlock = true
				break
			}
		}
		if !cancelledInCurrentBlock {
			// only cancel if it's not cancelled in a previous tx in the same block
			pairBlockCancellations.AddOrderIdToCancel(orderIdToCancel, types.CancellationInitiator_USER)
		}
	}

	return &types.MsgCancelOrdersResponse{}, nil
}

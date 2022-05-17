package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"go.opentelemetry.io/otel/attribute"
	otrace "go.opentelemetry.io/otel/trace"
)

func (k *Keeper) HandleEBCancelOrders(ctx context.Context, sdkCtx sdk.Context, tracer *otrace.Tracer, contractAddr string) {
	_, span := (*tracer).Start(ctx, "SudoCancelOrders")
	span.SetAttributes(attribute.String("contractAddr", contractAddr))

	msg := k.getCancelSudoMsg(contractAddr)
	_ = k.CallContractSudo(sdkCtx, contractAddr, msg)
	for pair, orderCancellations := range k.OrderCancellations[contractAddr] {
		for _, orderCancellation := range orderCancellations.OrderCancellations {
			k.Orders[contractAddr][pair].AddCancelOrder(dexcache.CancelOrder{
				Creator:  orderCancellation.Creator,
				Price:    orderCancellation.Price,
				Quantity: orderCancellation.Quantity,
				Long:     orderCancellation.Long,
				Open:     orderCancellation.Open,
				Leverage: orderCancellation.Leverage,
			})
		}
	}
	span.End()
}

func (k *Keeper) getCancelSudoMsg(contractAddr string) types.SudoOrderCancellationMsg {
	pairToOrderCancellations := k.OrderCancellations[contractAddr]
	contractOrderCancellations := []types.ContractOrderCancellation{}
	for _, orderCancellations := range pairToOrderCancellations {
		for _, orderCancellation := range orderCancellations.OrderCancellations {
			contractOrderCancellations = append(contractOrderCancellations, dexcache.ToContractOrderCancellation(orderCancellation))
		}
	}
	return types.SudoOrderCancellationMsg{
		OrderCancellations: types.OrderCancellationMsgDetails{
			OrderCancellations: contractOrderCancellations,
		},
	}
}

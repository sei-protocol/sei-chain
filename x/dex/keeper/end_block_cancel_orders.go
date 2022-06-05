package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"go.opentelemetry.io/otel/attribute"
	otrace "go.opentelemetry.io/otel/trace"
)

func (k *Keeper) HandleEBCancelOrders(ctx context.Context, sdkCtx sdk.Context, tracer *otrace.Tracer, contractAddr string, registeredPairs []types.Pair) {
	_, span := (*tracer).Start(ctx, "SudoCancelOrders")
	span.SetAttributes(attribute.String("contractAddr", contractAddr))

	msg := k.getCancelSudoMsg(contractAddr, registeredPairs)
	_ = k.CallContractSudo(sdkCtx, contractAddr, msg)
	for _, pair := range registeredPairs {
		pairStr := pair.String()
		if orderCancellations, ok := k.OrderCancellations[contractAddr][pairStr]; ok {
			for _, orderCancellation := range orderCancellations.OrderCancellations {
				k.Orders[contractAddr][pairStr].AddCancelOrder(dexcache.CancelOrder{
					Creator:   orderCancellation.Creator,
					Price:     orderCancellation.Price,
					Quantity:  orderCancellation.Quantity,
					Direction: orderCancellation.Direction,
					Effect:    orderCancellation.Effect,
					Leverage:  orderCancellation.Leverage,
				})
			}
		}
	}
	span.End()
}

func (k *Keeper) getCancelSudoMsg(contractAddr string, registeredPairs []types.Pair) types.SudoOrderCancellationMsg {
	contractOrderCancellations := []types.ContractOrderCancellation{}
	for _, pair := range registeredPairs {
		if orderCancellations, ok := k.OrderCancellations[contractAddr][pair.String()]; ok {
			for _, orderCancellation := range orderCancellations.OrderCancellations {
				contractOrderCancellations = append(contractOrderCancellations, dexcache.ToContractOrderCancellation(orderCancellation))
			}
		}
	}
	return types.SudoOrderCancellationMsg{
		OrderCancellations: types.OrderCancellationMsgDetails{
			OrderCancellations: contractOrderCancellations,
		},
	}
}

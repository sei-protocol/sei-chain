package contract

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	dexkeeperabci "github.com/sei-protocol/sei-chain/x/dex/keeper/abci"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
	"go.opentelemetry.io/otel/attribute"
	otrace "go.opentelemetry.io/otel/trace"
)

func PrepareCancelUnfulfilledMarketOrders(
	ctx sdk.Context,
	typedContractAddr types.ContractAddress,
	pair types.Pair,
	orderIDToSettledQuantities map[uint64]sdk.Dec,
) {
	dexutils.GetMemState(ctx.Context()).ClearCancellationForPair(ctx, typedContractAddr, pair)
	for _, marketOrderID := range getUnfulfilledPlacedMarketOrderIds(ctx, typedContractAddr, pair, orderIDToSettledQuantities) {
		dexutils.GetMemState(ctx.Context()).GetBlockCancels(ctx, typedContractAddr, pair).Add(&types.Cancellation{
			Id:        marketOrderID,
			Initiator: types.CancellationInitiator_USER,
		})
	}
}

func getUnfulfilledPlacedMarketOrderIds(
	ctx sdk.Context,
	typedContractAddr types.ContractAddress,
	pair types.Pair,
	orderIDToSettledQuantities map[uint64]sdk.Dec,
) []uint64 {
	res := []uint64{}
	for _, order := range dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, typedContractAddr, pair).Get() {
		if order.Status == types.OrderStatus_FAILED_TO_PLACE {
			continue
		}
		if order.OrderType == types.OrderType_MARKET || order.OrderType == types.OrderType_FOKMARKET {
			if settledQuantity, ok := orderIDToSettledQuantities[order.Id]; !ok || settledQuantity.LT(order.Quantity) {
				res = append(res, order.Id)
			}
		} else if order.OrderType == types.OrderType_FOKMARKETBYVALUE {
			// cancel market order by nominal if zero quantity is executed
			if _, ok := orderIDToSettledQuantities[order.Id]; !ok {
				res = append(res, order.Id)
			}
		}
	}
	return res
}

func CancelUnfulfilledMarketOrders(
	ctx context.Context,
	sdkCtx sdk.Context,
	contractAddr string,
	dexkeeper *keeper.Keeper,
	registeredPairs []types.Pair,
	tracer *otrace.Tracer,
) error {
	spanCtx, span := (*tracer).Start(ctx, "CancelUnfulfilledMarketOrders")
	span.SetAttributes(attribute.String("contract", contractAddr))
	defer span.End()
	abciWrapper := dexkeeperabci.KeeperWrapper{Keeper: dexkeeper}
	return abciWrapper.HandleEBCancelOrders(spanCtx, sdkCtx, tracer, contractAddr, registeredPairs)
}

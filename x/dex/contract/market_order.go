package contract

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	dexkeeperabci "github.com/sei-protocol/sei-chain/x/dex/keeper/abci"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dextypesutils "github.com/sei-protocol/sei-chain/x/dex/types/utils"
	"go.opentelemetry.io/otel/attribute"
	otrace "go.opentelemetry.io/otel/trace"
)

func PrepareCancelUnfulfilledMarketOrders(
	ctx sdk.Context,
	typedContractAddr dextypesutils.ContractAddress,
	typedPairStr dextypesutils.PairString,
	dexkeeper *keeper.Keeper,
	orderIDToSettledQuantities map[uint64]sdk.Dec,
) {
	dexkeeper.MemState.ClearCancellationForPair(ctx, typedContractAddr, typedPairStr)
	for _, marketOrderID := range getUnfulfilledPlacedMarketOrderIds(ctx, typedContractAddr, typedPairStr, dexkeeper, orderIDToSettledQuantities) {
		dexkeeper.MemState.GetBlockCancels(ctx, typedContractAddr, typedPairStr).Add(&types.Cancellation{
			Id:        marketOrderID,
			Initiator: types.CancellationInitiator_USER,
		})
	}
}

func getUnfulfilledPlacedMarketOrderIds(
	ctx sdk.Context,
	typedContractAddr dextypesutils.ContractAddress,
	typedPairStr dextypesutils.PairString,
	dexkeeper *keeper.Keeper,
	orderIDToSettledQuantities map[uint64]sdk.Dec,
) []uint64 {
	res := []uint64{}
	for _, order := range dexkeeper.MemState.GetBlockOrders(ctx, typedContractAddr, typedPairStr).Get() {
		if order.Status == types.OrderStatus_FAILED_TO_PLACE {
			continue
		}
		if order.OrderType == types.OrderType_MARKET || order.OrderType == types.OrderType_LIQUIDATION || 
		order.OrderType == types.OrderType_FOKMARKET || order.OrderType == types.OrderType_FOKMARKETBYVALUE {
			if settledQuantity, ok := orderIDToSettledQuantities[order.Id]; !ok || settledQuantity.LT(order.Quantity) {
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
	if err := abciWrapper.HandleEBCancelOrders(spanCtx, sdkCtx, tracer, contractAddr, registeredPairs); err != nil {
		return err
	}
	return nil
}

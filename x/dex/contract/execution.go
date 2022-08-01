package contract

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"go.opentelemetry.io/otel/attribute"
	otrace "go.opentelemetry.io/otel/trace"

	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/exchange"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	dexkeeperabci "github.com/sei-protocol/sei-chain/x/dex/keeper/abci"
	dexkeeperutils "github.com/sei-protocol/sei-chain/x/dex/keeper/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dextypesutils "github.com/sei-protocol/sei-chain/x/dex/types/utils"
	dextypeswasm "github.com/sei-protocol/sei-chain/x/dex/types/wasm"
)

func CallPreExecutionHooks(
	ctx sdk.Context,
	contractAddr string,
	dexkeeper *keeper.Keeper,
	tracer *otrace.Tracer,
) error {
	spanCtx, span := (*tracer).Start(ctx.Context(), "PreExecutionHooks")
	span.SetAttributes(attribute.String("contract", contractAddr))
	defer span.End()
	abciWrapper := dexkeeperabci.KeeperWrapper{Keeper: dexkeeper}
	registeredPairs := dexkeeper.GetAllRegisteredPairs(ctx, contractAddr)
	if err := abciWrapper.HandleEBLiquidation(spanCtx, ctx, tracer, contractAddr, registeredPairs); err != nil {
		return err
	}
	if err := abciWrapper.HandleEBCancelOrders(spanCtx, ctx, tracer, contractAddr, registeredPairs); err != nil {
		return err
	}
	if err := abciWrapper.HandleEBPlaceOrders(spanCtx, ctx, tracer, contractAddr, registeredPairs); err != nil {
		return err
	}
	return nil
}

func CancelUnfulfilledMarketOrders(
	ctx sdk.Context,
	contractAddr string,
	dexkeeper *keeper.Keeper,
	tracer *otrace.Tracer,
) error {
	spanCtx, span := (*tracer).Start(ctx.Context(), "CancelUnfulfilledMarketOrders")
	span.SetAttributes(attribute.String("contract", contractAddr))
	defer span.End()
	abciWrapper := dexkeeperabci.KeeperWrapper{Keeper: dexkeeper}
	registeredPairs := dexkeeper.GetAllRegisteredPairs(ctx, contractAddr)
	if err := abciWrapper.HandleEBCancelOrders(spanCtx, ctx, tracer, contractAddr, registeredPairs); err != nil {
		return err
	}
	return nil
}

func ExecutePair(
	ctx sdk.Context,
	contractAddr string,
	pair types.Pair,
	dexkeeper *keeper.Keeper,
) []*types.SettlementEntry {
	typedContractAddr := dextypesutils.ContractAddress(contractAddr)
	typedPairStr := dextypesutils.GetPairString(&pair)
	orderbook := dexkeeperutils.PopulateOrderbook(ctx, dexkeeper, typedContractAddr, pair)

	cancelForPair(ctx, typedContractAddr, typedPairStr, dexkeeper, orderbook)
	marketOrderOutcome := matchMarketOrderForPair(ctx, typedContractAddr, typedPairStr, dexkeeper, orderbook)
	limitOrderOutcome := matchLimitOrderForPair(ctx, typedContractAddr, typedPairStr, dexkeeper, orderbook)
	totalOutcome := marketOrderOutcome.Merge(&limitOrderOutcome)

	dexkeeperutils.SetPriceStateFromExecutionOutcome(ctx, dexkeeper, typedContractAddr, pair, totalOutcome)
	dexkeeperutils.FlushOrderbook(ctx, dexkeeper, typedContractAddr, orderbook)

	return totalOutcome.Settlements
}

func cancelForPair(
	ctx sdk.Context,
	typedContractAddr dextypesutils.ContractAddress,
	typedPairStr dextypesutils.PairString,
	dexkeeper *keeper.Keeper,
	orderbook *types.OrderBook,
) {
	cancels := dexkeeper.MemState.GetBlockCancels(typedContractAddr, typedPairStr)
	originalOrdersToCancel := dexkeeper.GetOrdersByIds(ctx, string(typedContractAddr), cancels.GetIdsToCancel())
	exchange.CancelOrders(*cancels, orderbook, originalOrdersToCancel)
}

func matchMarketOrderForPair(
	ctx sdk.Context,
	typedContractAddr dextypesutils.ContractAddress,
	typedPairStr dextypesutils.PairString,
	dexkeeper *keeper.Keeper,
	orderbook *types.OrderBook,
) exchange.ExecutionOutcome {
	orders := dexkeeper.MemState.GetBlockOrders(typedContractAddr, typedPairStr)
	marketBuys := orders.GetSortedMarketOrders(types.PositionDirection_LONG, true)
	marketSells := orders.GetSortedMarketOrders(types.PositionDirection_SHORT, true)
	marketBuyOutcome := exchange.MatchMarketOrders(
		ctx,
		marketBuys,
		orderbook.Shorts,
		types.PositionDirection_LONG,
	)
	marketSellOutcome := exchange.MatchMarketOrders(
		ctx,
		marketSells,
		orderbook.Longs,
		types.PositionDirection_SHORT,
	)
	return marketBuyOutcome.Merge(&marketSellOutcome)
}

func matchLimitOrderForPair(
	ctx sdk.Context,
	typedContractAddr dextypesutils.ContractAddress,
	typedPairStr dextypesutils.PairString,
	dexkeeper *keeper.Keeper,
	orderbook *types.OrderBook,
) exchange.ExecutionOutcome {
	orders := dexkeeper.MemState.GetBlockOrders(typedContractAddr, typedPairStr)
	limitBuys := orders.GetLimitOrders(types.PositionDirection_LONG)
	limitSells := orders.GetLimitOrders(types.PositionDirection_SHORT)
	return exchange.MatchLimitOrders(
		ctx,
		limitBuys,
		limitSells,
		orderbook,
	)
}

func UpdateOrderState(
	ctx sdk.Context,
	typedContractAddr dextypesutils.ContractAddress,
	typedPairStr dextypesutils.PairString,
	dexkeeper *keeper.Keeper,
	settlements []*types.SettlementEntry,
) {
	orders := dexkeeper.MemState.GetBlockOrders(typedContractAddr, typedPairStr)
	cancels := dexkeeper.MemState.GetBlockCancels(typedContractAddr, typedPairStr)
	// First add any new order, whether successfully placed or not, to the store
	for _, order := range *orders {
		if order.Quantity.IsZero() {
			order.Status = types.OrderStatus_FULFILLED
		}
		dexkeeper.AddNewOrder(ctx, *order)
	}
	// Then update order status and insert cancel record for any cancellation
	for _, cancel := range *cancels {
		dexkeeper.AddCancel(ctx, string(typedContractAddr), cancel)
		dexkeeper.UpdateOrderStatus(ctx, string(typedContractAddr), cancel.Id, types.OrderStatus_CANCELLED)
	}
	// Then deduct quantity from orders that have (partially) settled
	for _, settlementEntry := range settlements {
		// Market orders would have already had their quantities reduced during order matching
		if settlementEntry.OrderType == dextypeswasm.GetContractOrderType(types.OrderType_LIMIT) {
			dexkeeper.ReduceOrderQuantity(ctx, string(typedContractAddr), settlementEntry.OrderId, settlementEntry.Quantity)
		}
	}
	// Finally update market order status based on execution result
	for _, marketOrderID := range getUnfulfilledPlacedMarketOrderIds(typedContractAddr, typedPairStr, dexkeeper) {
		dexkeeper.UpdateOrderStatus(ctx, string(typedContractAddr), marketOrderID, types.OrderStatus_CANCELLED)
	}
}

func PrepareCancelUnfulfilledMarketOrders(
	typedContractAddr dextypesutils.ContractAddress,
	typedPairStr dextypesutils.PairString,
	dexkeeper *keeper.Keeper,
) {
	emptyBlockCancel := dexcache.BlockCancellations([]types.Cancellation{})
	dexkeeper.MemState.BlockCancels[typedContractAddr][typedPairStr] = &emptyBlockCancel
	for _, marketOrderID := range getUnfulfilledPlacedMarketOrderIds(typedContractAddr, typedPairStr, dexkeeper) {
		dexkeeper.MemState.GetBlockCancels(typedContractAddr, typedPairStr).AddCancel(types.Cancellation{
			Id:        marketOrderID,
			Initiator: types.CancellationInitiator_USER,
		})
	}
}

func getUnfulfilledPlacedMarketOrderIds(
	typedContractAddr dextypesutils.ContractAddress,
	typedPairStr dextypesutils.PairString,
	dexkeeper *keeper.Keeper,
) []uint64 {
	res := []uint64{}
	for _, order := range *dexkeeper.MemState.GetBlockOrders(typedContractAddr, typedPairStr) {
		if order.Status == types.OrderStatus_FAILED_TO_PLACE {
			continue
		}
		if order.OrderType == types.OrderType_MARKET || order.OrderType == types.OrderType_LIQUIDATION {
			if order.Quantity.IsPositive() {
				res = append(res, order.Id)
			}
		}
	}
	return res
}

func HandleExecutionForContract(
	ctx sdk.Context,
	contract types.ContractInfo,
	dexkeeper *keeper.Keeper,
	tracer *otrace.Tracer,
) (map[string]dextypeswasm.ContractOrderResult, []*types.SettlementEntry, error) {
	contractAddr := contract.ContractAddr
	typedContractAddr := dextypesutils.ContractAddress(contractAddr)
	registeredPairs := dexkeeper.GetAllRegisteredPairs(ctx, contractAddr)
	orderResults := map[string]dextypeswasm.ContractOrderResult{}
	settlements := []*types.SettlementEntry{}
	// Call contract hooks so that contracts can do internal bookkeeping
	if err := CallPreExecutionHooks(ctx, contractAddr, dexkeeper, tracer); err != nil {
		return orderResults, settlements, err
	}

	for _, pair := range registeredPairs {
		pairCopy := pair
		pairSettlements := ExecutePair(ctx, contractAddr, pair, dexkeeper)
		UpdateOrderState(ctx, typedContractAddr, dextypesutils.GetPairString(&pairCopy), dexkeeper, pairSettlements)
		PrepareCancelUnfulfilledMarketOrders(typedContractAddr, dextypesutils.GetPairString(&pairCopy), dexkeeper)

		settlements = append(settlements, pairSettlements...)
	}
	// Cancel unfilled market orders
	if err := CancelUnfulfilledMarketOrders(ctx, contractAddr, dexkeeper, tracer); err != nil {
		return orderResults, settlements, err
	}

	// populate order placement results for FinalizeBlock hook
	for _, orders := range dexkeeper.MemState.BlockOrders[typedContractAddr] {
		dextypeswasm.PopulateOrderPlacementResults(contractAddr, *orders, orderResults)
	}
	dextypeswasm.PopulateOrderExecutionResults(contractAddr, settlements, orderResults)
	return orderResults, settlements, nil
}

package contract

import (
	"context"
	"fmt"
	"sync"
	"time"

	otrace "go.opentelemetry.io/otel/trace"

	"github.com/cosmos/cosmos-sdk/telemetry"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/store/whitelist/multi"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/utils/datastructures"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/exchange"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	dexkeeperabci "github.com/sei-protocol/sei-chain/x/dex/keeper/abci"
	dexkeeperutils "github.com/sei-protocol/sei-chain/x/dex/keeper/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dextypesutils "github.com/sei-protocol/sei-chain/x/dex/types/utils"
	dextypeswasm "github.com/sei-protocol/sei-chain/x/dex/types/wasm"
	"go.opentelemetry.io/otel/attribute"
)

func CallPreExecutionHooks(
	ctx context.Context,
	sdkCtx sdk.Context,
	contractAddr string,
	dexkeeper *keeper.Keeper,
	registeredPairs []types.Pair,
	tracer *otrace.Tracer,
) error {
	spanCtx, span := (*tracer).Start(ctx, "PreExecutionHooks")
	defer span.End()
	span.SetAttributes(attribute.String("contract", contractAddr))
	abciWrapper := dexkeeperabci.KeeperWrapper{Keeper: dexkeeper}
	if err := abciWrapper.HandleEBLiquidation(spanCtx, sdkCtx, tracer, contractAddr, registeredPairs); err != nil {
		return err
	}
	if err := abciWrapper.HandleEBCancelOrders(spanCtx, sdkCtx, tracer, contractAddr, registeredPairs); err != nil {
		return err
	}
	if err := abciWrapper.HandleEBPlaceOrders(spanCtx, sdkCtx, tracer, contractAddr, registeredPairs); err != nil {
		return err
	}
	return nil
}

func ExecutePair(
	ctx sdk.Context,
	contractAddr string,
	pair types.Pair,
	dexkeeper *keeper.Keeper,
	orderbook *types.OrderBook,
) []*types.SettlementEntry {
	typedContractAddr := dextypesutils.ContractAddress(contractAddr)
	typedPairStr := dextypesutils.GetPairString(&pair)

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
	cancels := dexkeeper.MemState.GetBlockCancels(ctx, typedContractAddr, typedPairStr)
	originalOrdersToCancel := dexkeeper.GetOrdersByIds(ctx, string(typedContractAddr), cancels.GetIdsToCancel())
	exchange.CancelOrders(cancels.Get(), orderbook, originalOrdersToCancel)
}

func matchMarketOrderForPair(
	ctx sdk.Context,
	typedContractAddr dextypesutils.ContractAddress,
	typedPairStr dextypesutils.PairString,
	dexkeeper *keeper.Keeper,
	orderbook *types.OrderBook,
) exchange.ExecutionOutcome {
	orders := dexkeeper.MemState.GetBlockOrders(ctx, typedContractAddr, typedPairStr)
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
	orders := dexkeeper.MemState.GetBlockOrders(ctx, typedContractAddr, typedPairStr)
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
	orderIDToSettledQuantities map[uint64]sdk.Dec,
) {
	orders := dexkeeper.MemState.GetBlockOrders(ctx, typedContractAddr, typedPairStr)
	cancels := dexkeeper.MemState.GetBlockCancels(ctx, typedContractAddr, typedPairStr)
	// First add any new order, whether successfully placed or not, to the store
	for _, order := range orders.Get() {
		if order.Quantity.IsZero() {
			order.Status = types.OrderStatus_FULFILLED
		}
		dexkeeper.AddNewOrder(ctx, *order)
	}
	// Then update order status and insert cancel record for any cancellation
	for _, cancel := range cancels.Get() {
		dexkeeper.AddCancel(ctx, string(typedContractAddr), cancel)
		dexkeeper.UpdateOrderStatus(ctx, string(typedContractAddr), cancel.Id, types.OrderStatus_CANCELLED)
	}
	// Then deduct quantity from orders that have (partially) settled
	for orderID, quantity := range orderIDToSettledQuantities {
		dexkeeper.ReduceOrderQuantity(ctx, string(typedContractAddr), orderID, quantity)
	}
	// Finally update market order status based on execution result
	for _, marketOrderID := range getUnfulfilledPlacedMarketOrderIds(ctx, typedContractAddr, typedPairStr, dexkeeper, orderIDToSettledQuantities) {
		dexkeeper.UpdateOrderStatus(ctx, string(typedContractAddr), marketOrderID, types.OrderStatus_CANCELLED)
	}
}

func GetOrderIDToSettledQuantities(settlements []*types.SettlementEntry) map[uint64]sdk.Dec {
	res := map[uint64]sdk.Dec{}
	for _, settlement := range settlements {
		if _, ok := res[settlement.OrderId]; !ok {
			res[settlement.OrderId] = sdk.ZeroDec()
		}
		res[settlement.OrderId] = res[settlement.OrderId].Add(settlement.Quantity)
	}
	return res
}

func ExecutePairsInParallel(ctx sdk.Context, contractAddr string, dexkeeper *keeper.Keeper, registeredPairs []types.Pair, orderBooks *datastructures.TypedSyncMap[dextypesutils.PairString, *types.OrderBook]) ([]func(), []*types.SettlementEntry) {
	typedContractAddr := dextypesutils.ContractAddress(contractAddr)
	orderUpdaters := []func(){}
	settlements := []*types.SettlementEntry{}

	mu := sync.Mutex{}
	wg := sync.WaitGroup{}
	anyPanicked := false

	for _, pair := range registeredPairs {
		wg.Add(1)

		pair := pair
		pairCtx := ctx.WithMultiStore(multi.NewStore(ctx.MultiStore(), GetPerPairWhitelistMap(contractAddr, pair))).WithEventManager(sdk.NewEventManager())
		go func() {
			defer wg.Done()
			defer utils.PanicHandler(func(err any) {
				mu.Lock()
				defer mu.Unlock()
				anyPanicked = true
				utils.MetricsPanicCallback(err, ctx, fmt.Sprintf("%s-%s|%s", contractAddr, pair.PriceDenom, pair.AssetDenom))
			})()

			pairCopy := pair
			pairStr := dextypesutils.GetPairString(&pairCopy)
			orderbook, found := orderBooks.Load(pairStr)
			if !found {
				panic(fmt.Sprintf("Orderbook not found for %s", pairStr))
			}
			pairSettlements := ExecutePair(pairCtx, contractAddr, pair, dexkeeper, orderbook.DeepCopy())
			orderIDToSettledQuantities := GetOrderIDToSettledQuantities(pairSettlements)
			PrepareCancelUnfulfilledMarketOrders(pairCtx, typedContractAddr, pairStr, dexkeeper, orderIDToSettledQuantities)

			mu.Lock()
			defer mu.Unlock()
			orderUpdaters = append(orderUpdaters, func() {
				UpdateOrderState(ctx, typedContractAddr, dextypesutils.GetPairString(&pairCopy), dexkeeper, orderIDToSettledQuantities)
			})
			settlements = append(settlements, pairSettlements...)
			// ordering of events doesn't matter since events aren't part of consensus
			ctx.EventManager().EmitEvents(pairCtx.EventManager().Events())
		}()
	}
	wg.Wait()
	if anyPanicked {
		// need to re-throw panic to the top level goroutine
		panic("panicked during pair execution")
	}

	return orderUpdaters, settlements
}

func HandleExecutionForContract(
	ctx context.Context,
	sdkCtx sdk.Context,
	contract types.ContractInfo,
	dexkeeper *keeper.Keeper,
	registeredPairs []types.Pair,
	orderBooks *datastructures.TypedSyncMap[dextypesutils.PairString, *types.OrderBook],
	tracer *otrace.Tracer,
) (map[string]dextypeswasm.ContractOrderResult, []*types.SettlementEntry, error) {
	executionStart := time.Now()
	defer telemetry.ModuleSetGauge(types.ModuleName, float32(time.Since(executionStart).Milliseconds()), "handle_execution_for_contract_ms")
	contractAddr := contract.ContractAddr
	typedContractAddr := dextypesutils.ContractAddress(contractAddr)
	orderResults := map[string]dextypeswasm.ContractOrderResult{}

	// Call contract hooks so that contracts can do internal bookkeeping
	if err := CallPreExecutionHooks(ctx, sdkCtx, contractAddr, dexkeeper, registeredPairs, tracer); err != nil {
		return orderResults, []*types.SettlementEntry{}, err
	}

	orderUpdaters, settlements := ExecutePairsInParallel(sdkCtx, contractAddr, dexkeeper, registeredPairs, orderBooks)

	for _, orderUpdater := range orderUpdaters {
		orderUpdater()
	}

	// populate order placement results for FinalizeBlock hook
	dexkeeper.MemState.GetAllBlockOrders(sdkCtx, typedContractAddr).DeepApply(func(orders *dexcache.BlockOrders) {
		dextypeswasm.PopulateOrderPlacementResults(contractAddr, orders.Get(), orderResults)
	})
	dextypeswasm.PopulateOrderExecutionResults(contractAddr, settlements, orderResults)
	return orderResults, settlements, nil
}

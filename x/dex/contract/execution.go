package contract

import (
	"context"
	"fmt"
	"time"

	otrace "go.opentelemetry.io/otel/trace"

	"github.com/cosmos/cosmos-sdk/telemetry"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/store/whitelist/multi"
	"github.com/sei-protocol/sei-chain/utils/datastructures"
	"github.com/sei-protocol/sei-chain/x/dex/exchange"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	dexkeeperabci "github.com/sei-protocol/sei-chain/x/dex/keeper/abci"
	dexkeeperutils "github.com/sei-protocol/sei-chain/x/dex/keeper/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
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
	if err := abciWrapper.HandleEBCancelOrders(spanCtx, sdkCtx, tracer, contractAddr, registeredPairs); err != nil {
		return err
	}
	return abciWrapper.HandleEBPlaceOrders(spanCtx, sdkCtx, tracer, contractAddr, registeredPairs)
}

func ExecutePair(
	ctx sdk.Context,
	contractAddr string,
	pair types.Pair,
	dexkeeper *keeper.Keeper,
	orderbook *types.OrderBook,
) []*types.SettlementEntry {
	typedContractAddr := types.ContractAddress(contractAddr)

	// First cancel orders
	cancelForPair(ctx, dexkeeper, typedContractAddr, pair)
	// Add all limit orders to the orderbook
	orders := dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, typedContractAddr, pair)
	limitBuys := orders.GetLimitOrders(types.PositionDirection_LONG)
	limitSells := orders.GetLimitOrders(types.PositionDirection_SHORT)
	exchange.AddOutstandingLimitOrdersToOrderbook(ctx, dexkeeper, limitBuys, limitSells)
	// Fill market orders
	marketOrderOutcome := matchMarketOrderForPair(ctx, typedContractAddr, pair, orderbook)
	// Fill limit orders
	limitOrderOutcome := exchange.MatchLimitOrders(ctx, orderbook)
	totalOutcome := marketOrderOutcome.Merge(&limitOrderOutcome)

	dexkeeperutils.SetPriceStateFromExecutionOutcome(ctx, dexkeeper, typedContractAddr, pair, totalOutcome)

	return totalOutcome.Settlements
}

func cancelForPair(
	ctx sdk.Context,
	keeper *keeper.Keeper,
	contractAddress types.ContractAddress,
	pair types.Pair,
) {
	cancels := dexutils.GetMemState(ctx.Context()).GetBlockCancels(ctx, contractAddress, pair)
	exchange.CancelOrders(ctx, keeper, contractAddress, pair, cancels.Get())
}

func matchMarketOrderForPair(
	ctx sdk.Context,
	typedContractAddr types.ContractAddress,
	pair types.Pair,
	orderbook *types.OrderBook,
) exchange.ExecutionOutcome {
	orders := dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, typedContractAddr, pair)
	marketBuys := orders.GetSortedMarketOrders(types.PositionDirection_LONG)
	marketSells := orders.GetSortedMarketOrders(types.PositionDirection_SHORT)
	marketBuyOutcome := exchange.MatchMarketOrders(
		ctx,
		marketBuys,
		orderbook.Shorts,
		types.PositionDirection_LONG,
		orders,
	)
	marketSellOutcome := exchange.MatchMarketOrders(
		ctx,
		marketSells,
		orderbook.Longs,
		types.PositionDirection_SHORT,
		orders,
	)
	return marketBuyOutcome.Merge(&marketSellOutcome)
}

func GetMatchResults(
	ctx sdk.Context,
	typedContractAddr types.ContractAddress,
	pair types.Pair,
) ([]*types.Order, []*types.Cancellation) {
	orderResults := dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, typedContractAddr, pair).Get()
	cancelResults := dexutils.GetMemState(ctx.Context()).GetBlockCancels(ctx, typedContractAddr, pair).Get()
	return orderResults, cancelResults
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

func ExecutePairsInParallel(ctx sdk.Context, contractAddr string, dexkeeper *keeper.Keeper, registeredPairs []types.Pair, orderBooks *datastructures.TypedSyncMap[types.PairString, *types.OrderBook]) []*types.SettlementEntry {
	typedContractAddr := types.ContractAddress(contractAddr)
	orderResults := []*types.Order{}
	cancelResults := []*types.Cancellation{}
	settlements := []*types.SettlementEntry{}

	// mu := sync.Mutex{}
	// wg := sync.WaitGroup{}

	for _, pair := range registeredPairs {
		// wg.Add(1)

		pair := pair
		pairCtx := ctx.WithMultiStore(multi.NewStore(ctx.MultiStore(), GetPerPairWhitelistMap(contractAddr, pair))).WithEventManager(sdk.NewEventManager())
		// go func() {
		func() {
			// defer wg.Done()
			pairCopy := pair
			pairStr := types.GetPairString(&pairCopy)
			orderbook, found := orderBooks.Load(pairStr)
			if !found {
				panic(fmt.Sprintf("Orderbook not found for %s", pairCopy.String()))
			}
			pairSettlements := ExecutePair(pairCtx, contractAddr, pair, dexkeeper, orderbook)
			orderIDToSettledQuantities := GetOrderIDToSettledQuantities(pairSettlements)
			PrepareCancelUnfulfilledMarketOrders(pairCtx, typedContractAddr, pairCopy, orderIDToSettledQuantities)

			// mu.Lock()
			// defer mu.Unlock()
			orders, cancels := GetMatchResults(ctx, typedContractAddr, pairCopy)
			orderResults = append(orderResults, orders...)
			cancelResults = append(cancelResults, cancels...)
			settlements = append(settlements, pairSettlements...)
			// ordering of events doesn't matter since events aren't part of consensus
			ctx.EventManager().EmitEvents(pairCtx.EventManager().Events())
		}()
	}
	// wg.Wait()
	dexkeeper.SetMatchResult(ctx, contractAddr, types.NewMatchResult(orderResults, cancelResults, settlements))

	return settlements
}

func HandleExecutionForContract(
	ctx context.Context,
	sdkCtx sdk.Context,
	contract types.ContractInfoV2,
	dexkeeper *keeper.Keeper,
	registeredPairs []types.Pair,
	orderBooks *datastructures.TypedSyncMap[types.PairString, *types.OrderBook],
	tracer *otrace.Tracer,
) ([]*types.SettlementEntry, error) {
	executionStart := time.Now()
	defer telemetry.ModuleMeasureSince(types.ModuleName, executionStart, "handle_execution_for_contract_ms")
	contractAddr := contract.ContractAddr

	// Call contract hooks so that contracts can do internal bookkeeping
	if err := CallPreExecutionHooks(ctx, sdkCtx, contractAddr, dexkeeper, registeredPairs, tracer); err != nil {
		return []*types.SettlementEntry{}, err
	}
	settlements := ExecutePairsInParallel(sdkCtx, contractAddr, dexkeeper, registeredPairs, orderBooks)
	defer EmitSettlementMetrics(settlements)

	return settlements, nil
}

// Emit metrics for settlements
func EmitSettlementMetrics(settlements []*types.SettlementEntry) {
	if len(settlements) > 0 {
		telemetry.ModuleSetGauge(
			types.ModuleName,
			float32(len(settlements)),
			"num_settlements",
		)
		for _, s := range settlements {
			telemetry.IncrCounter(
				1,
				"num_settlements_order_type_"+s.GetOrderType(),
			)
			telemetry.IncrCounter(
				1,
				"num_settlements_position_direction"+s.GetPositionDirection(),
			)
			telemetry.IncrCounter(
				1,
				"num_settlements_asset_denom_"+s.GetAssetDenom(),
			)
			telemetry.IncrCounter(
				1,
				"num_settlements_price_denom_"+s.GetPriceDenom(),
			)
		}
	}
}

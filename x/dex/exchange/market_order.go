package exchange

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	cache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func MatchMarketOrders(
	ctx sdk.Context,
	marketOrders []*types.Order,
	orderBookEntries *types.CachedSortedOrderBookEntries,
	direction types.PositionDirection,
	blockOrders *cache.BlockOrders,
) ExecutionOutcome {
	totalExecuted, totalPrice := sdk.ZeroDec(), sdk.ZeroDec()
	minPrice, maxPrice := sdk.OneDec().Neg(), sdk.OneDec().Neg()
	settlements := []*types.SettlementEntry{}
	allTakerSettlements := []*types.SettlementEntry{}
	for _, marketOrder := range marketOrders {
		switch marketOrder.OrderType {
		case types.OrderType_FOKMARKETBYVALUE:
			settlements, allTakerSettlements = MatchByValueFOKMarketOrder(
				ctx, marketOrder, orderBookEntries, direction, &totalExecuted, &totalPrice, &minPrice, &maxPrice, settlements, allTakerSettlements, blockOrders)
		case types.OrderType_FOKMARKET:
			settlements, allTakerSettlements = MatchFOKMarketOrder(
				ctx, marketOrder, orderBookEntries, direction, &totalExecuted, &totalPrice, &minPrice, &maxPrice, settlements, allTakerSettlements, blockOrders)
		default:
			settlements, allTakerSettlements = MatchMarketOrder(
				ctx, marketOrder, orderBookEntries, direction, &totalExecuted, &totalPrice, &minPrice, &maxPrice, settlements, allTakerSettlements, blockOrders)
		}
	}

	if totalExecuted.IsPositive() {
		clearingPrice := totalPrice.Quo(totalExecuted)
		for _, settlement := range allTakerSettlements {
			settlement.ExecutionCostOrProceed = clearingPrice
		}
		minPrice, maxPrice = clearingPrice, clearingPrice
		settlements = append(settlements, allTakerSettlements...)
	}
	return ExecutionOutcome{
		TotalNotional: totalPrice,
		TotalQuantity: totalExecuted,
		Settlements:   settlements,
		MinPrice:      minPrice,
		MaxPrice:      maxPrice,
	}
}

func MatchMarketOrder(
	ctx sdk.Context,
	marketOrder *types.Order,
	orderBookEntries *types.CachedSortedOrderBookEntries,
	direction types.PositionDirection,
	totalExecuted *sdk.Dec,
	totalPrice *sdk.Dec,
	minPrice *sdk.Dec,
	maxPrice *sdk.Dec,
	settlements []*types.SettlementEntry,
	allTakerSettlements []*types.SettlementEntry,
	blockOrders *cache.BlockOrders,
) ([]*types.SettlementEntry, []*types.SettlementEntry) {
	remainingQuantity := marketOrder.Quantity
	for entry := orderBookEntries.Next(ctx); entry != nil; entry = orderBookEntries.Next(ctx) {
		// If price is zero, it means the order sender
		// doesn't want to specify a worst price, so
		// we don't need to perform price check for such orders
		if !marketOrder.Price.IsZero() {
			// Check if worst price can be matched against order book
			if (direction == types.PositionDirection_LONG && marketOrder.Price.LT(entry.GetPrice())) ||
				(direction == types.PositionDirection_SHORT && marketOrder.Price.GT(entry.GetPrice())) {
				break
			}
		}
		var executed sdk.Dec
		if remainingQuantity.LTE(entry.GetOrderEntry().Quantity) {
			executed = remainingQuantity
		} else {
			executed = entry.GetOrderEntry().Quantity
		}
		remainingQuantity = remainingQuantity.Sub(executed)
		*totalExecuted = totalExecuted.Add(executed)
		*totalPrice = totalPrice.Add(
			executed.Mul(entry.GetPrice()),
		)
		if minPrice.IsNegative() || minPrice.GT(entry.GetPrice()) {
			*minPrice = entry.GetPrice()
		}
		*maxPrice = sdk.MaxDec(*maxPrice, entry.GetPrice())

		takerSettlements, makerSettlements := Settle(
			ctx,
			marketOrder,
			executed,
			orderBookEntries,
			marketOrder.Price,
			entry.GetPrice(),
		)
		// update the status of order in the memState
		UpdateOrderData(marketOrder, executed, blockOrders)
		settlements = append(settlements, makerSettlements...)
		// taker settlements' clearing price will need to be adjusted after all market order executions finish
		allTakerSettlements = append(allTakerSettlements, takerSettlements...)
		if remainingQuantity.IsZero() {
			break
		}
	}

	orderBookEntries.Flush(ctx)

	return settlements, allTakerSettlements
}

func MatchFOKMarketOrder(
	ctx sdk.Context,
	marketOrder *types.Order,
	orderBookEntries *types.CachedSortedOrderBookEntries,
	direction types.PositionDirection,
	totalExecuted *sdk.Dec,
	totalPrice *sdk.Dec,
	minPrice *sdk.Dec,
	maxPrice *sdk.Dec,
	settlements []*types.SettlementEntry,
	allTakerSettlements []*types.SettlementEntry,
	blockOrders *cache.BlockOrders,
) ([]*types.SettlementEntry, []*types.SettlementEntry) {
	// check if there is enough liquidity for fill-or-kill market order, if not skip them
	remainingQuantity := marketOrder.Quantity
	newSettlements, newTakerSettlements := []*types.SettlementEntry{}, []*types.SettlementEntry{}
	orders, executedQuantities, entryPrices := []*types.Order{}, []sdk.Dec{}, []sdk.Dec{}
	for entry := orderBookEntries.Next(ctx); entry != nil; entry = orderBookEntries.Next(ctx) {
		if !marketOrder.Price.IsZero() {
			if (direction == types.PositionDirection_LONG && marketOrder.Price.LT(entry.GetPrice())) ||
				(direction == types.PositionDirection_SHORT && marketOrder.Price.GT(entry.GetPrice())) {
				break
			}
		}

		var executed sdk.Dec
		if remainingQuantity.LTE(entry.GetOrderEntry().Quantity) {
			executed = remainingQuantity
		} else {
			executed = entry.GetOrderEntry().Quantity
		}
		remainingQuantity = remainingQuantity.Sub(executed)

		takerSettlements, makerSettlements := Settle(
			ctx,
			marketOrder,
			executed,
			orderBookEntries,
			marketOrder.Price,
			entry.GetPrice(),
		)
		newSettlements = append(newSettlements, makerSettlements...)
		newTakerSettlements = append(newTakerSettlements, takerSettlements...)
		orders = append(orders, marketOrder)
		executedQuantities = append(executedQuantities, executed)
		entryPrices = append(entryPrices, entry.GetPrice())

		if remainingQuantity.IsZero() {
			break
		}
	}

	if remainingQuantity.IsZero() {
		orderBookEntries.Flush(ctx)
		settlements = append(settlements, newSettlements...)
		allTakerSettlements = append(allTakerSettlements, newTakerSettlements...)
		for i, order := range orders {
			UpdateOrderData(order, executedQuantities[i], blockOrders)
			*totalExecuted = totalExecuted.Add(executedQuantities[i])
			*totalPrice = totalPrice.Add(
				executedQuantities[i].Mul(entryPrices[i]),
			)
			if minPrice.IsNegative() || minPrice.GT(entryPrices[i]) {
				*minPrice = entryPrices[i]
			}
			*maxPrice = sdk.MaxDec(*maxPrice, entryPrices[i])
		}
	} else {
		orderBookEntries.Refresh(ctx)
	}

	return settlements, allTakerSettlements
}

func MatchByValueFOKMarketOrder(
	ctx sdk.Context,
	marketOrder *types.Order,
	orderBookEntries *types.CachedSortedOrderBookEntries,
	direction types.PositionDirection,
	totalExecuted *sdk.Dec,
	totalPrice *sdk.Dec,
	minPrice *sdk.Dec,
	maxPrice *sdk.Dec,
	settlements []*types.SettlementEntry,
	allTakerSettlements []*types.SettlementEntry,
	blockOrders *cache.BlockOrders,
) ([]*types.SettlementEntry, []*types.SettlementEntry) {
	remainingFund := marketOrder.Nominal
	remainingQuantity := marketOrder.Quantity
	newSettlements, newTakerSettlements := []*types.SettlementEntry{}, []*types.SettlementEntry{}
	orders, executedQuantities, entryPrices := []*types.Order{}, []sdk.Dec{}, []sdk.Dec{}
	for entry := orderBookEntries.Next(ctx); entry != nil; entry = orderBookEntries.Next(ctx) {
		if !marketOrder.Price.IsZero() {
			if (direction == types.PositionDirection_LONG && marketOrder.Price.LT(entry.GetPrice())) ||
				(direction == types.PositionDirection_SHORT && marketOrder.Price.GT(entry.GetPrice())) {
				break
			}
		}
		var executed sdk.Dec
		if remainingFund.LTE(entry.GetOrderEntry().Quantity.Mul(entry.GetPrice())) {
			executed = remainingFund.Quo(entry.GetPrice())
			remainingFund = sdk.ZeroDec()
		} else {
			executed = entry.GetOrderEntry().Quantity
			remainingFund = remainingFund.Sub(executed.Mul(entry.GetPrice()))
		}
		remainingQuantity = remainingQuantity.Sub(executed)

		takerSettlements, makerSettlements := Settle(
			ctx,
			marketOrder,
			executed,
			orderBookEntries,
			marketOrder.Price,
			entry.GetPrice(),
		)
		newSettlements = append(newSettlements, makerSettlements...)
		newTakerSettlements = MergeByNominalTakerSettlements(append(newTakerSettlements, takerSettlements...))
		orders = append(orders, marketOrder)
		executedQuantities = append(executedQuantities, executed)
		entryPrices = append(entryPrices, entry.GetPrice())
		if remainingFund.IsZero() || remainingQuantity.LTE(sdk.ZeroDec()) {
			break
		}
	}

	// settle orders only when all fund are used
	if remainingFund.IsZero() && remainingQuantity.GTE(sdk.ZeroDec()) {
		orderBookEntries.Flush(ctx)
		settlements = append(settlements, newSettlements...)
		allTakerSettlements = append(allTakerSettlements, newTakerSettlements...)
		for i, order := range orders {
			UpdateOrderData(order, executedQuantities[i], blockOrders)
			*totalExecuted = totalExecuted.Add(executedQuantities[i])
			*totalPrice = totalPrice.Add(
				executedQuantities[i].Mul(entryPrices[i]),
			)
			if minPrice.IsNegative() || minPrice.GT(entryPrices[i]) {
				*minPrice = entryPrices[i]
			}
			*maxPrice = sdk.MaxDec(*maxPrice, entryPrices[i])
		}
	} else {
		orderBookEntries.Refresh(ctx)
	}

	return settlements, allTakerSettlements
}

func MergeByNominalTakerSettlements(settlements []*types.SettlementEntry) []*types.SettlementEntry {
	aggregatedSettlement := types.SettlementEntry{Quantity: sdk.ZeroDec()}
	for _, settlement := range settlements {
		quantity := settlement.Quantity.Add(aggregatedSettlement.Quantity)
		aggregatedSettlement = *settlement
		aggregatedSettlement.Quantity = quantity
	}

	return []*types.SettlementEntry{&aggregatedSettlement}
}

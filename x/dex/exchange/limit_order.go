package exchange

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func MatchLimitOrders(
	ctx sdk.Context,
	orderbook *types.OrderBook,
) ExecutionOutcome {
	settlements := []*types.SettlementEntry{}
	totalExecuted, totalPrice := sdk.ZeroDec(), sdk.ZeroDec()
	minPrice, maxPrice := sdk.OneDec().Neg(), sdk.OneDec().Neg()

	for longEntry, shortEntry := orderbook.Longs.Next(ctx), orderbook.Shorts.Next(ctx); longEntry != nil && shortEntry != nil && longEntry.GetPrice().GTE(shortEntry.GetPrice()); longEntry, shortEntry = orderbook.Longs.Next(ctx), orderbook.Shorts.Next(ctx) {
		var executed sdk.Dec
		if longEntry.GetOrderEntry().Quantity.LT(shortEntry.GetOrderEntry().Quantity) {
			executed = longEntry.GetOrderEntry().Quantity
		} else {
			executed = shortEntry.GetOrderEntry().Quantity
		}
		totalExecuted = totalExecuted.Add(executed).Add(executed)
		totalPrice = totalPrice.Add(
			executed.Mul(
				longEntry.GetPrice().Add(shortEntry.GetPrice()),
			),
		)
		if minPrice.IsNegative() || minPrice.GT(shortEntry.GetPrice()) {
			minPrice = shortEntry.GetPrice()
		}
		maxPrice = sdk.MaxDec(maxPrice, longEntry.GetPrice())

		newSettlements := SettleFromBook(
			ctx,
			orderbook,
			executed,
			longEntry.GetPrice(),
			shortEntry.GetPrice(),
		)
		settlements = append(settlements, newSettlements...)
	}

	orderbook.Longs.Flush(ctx)
	orderbook.Shorts.Flush(ctx)
	return ExecutionOutcome{
		TotalNotional: totalPrice,
		TotalQuantity: totalExecuted,
		Settlements:   settlements,
		MinPrice:      minPrice,
		MaxPrice:      maxPrice,
	}
}

func addOrderToOrderBookEntry(
	ctx sdk.Context, keeper *keeper.Keeper,
	order *types.Order,
) {
	getter, setter := keeper.GetLongOrderBookEntryByPrice, keeper.SetLongOrderBookEntry
	if order.PositionDirection == types.PositionDirection_SHORT {
		getter, setter = keeper.GetShortOrderBookEntryByPrice, keeper.SetShortOrderBookEntry
	}
	entry, exist := getter(ctx, order.ContractAddr, order.Price, order.PriceDenom, order.AssetDenom)
	orderEntry := entry.GetOrderEntry()
	if !exist {
		orderEntry = &types.OrderEntry{
			Price:      order.Price,
			PriceDenom: order.PriceDenom,
			AssetDenom: order.AssetDenom,
			Quantity:   sdk.ZeroDec(),
		}
	}
	orderEntry.Quantity = orderEntry.Quantity.Add(order.Quantity)
	orderEntry.Allocations = append(orderEntry.Allocations, &types.Allocation{
		OrderId:  order.Id,
		Quantity: order.Quantity,
		Account:  order.Account,
	})
	entry.SetPrice(order.Price)
	entry.SetEntry(orderEntry)
	setter(ctx, order.ContractAddr, entry)

	err := keeper.IncreaseOrderCount(ctx, order.ContractAddr, order.PriceDenom, order.AssetDenom, order.PositionDirection, order.Price, 1)
	if err != nil {
		ctx.Logger().Error(fmt.Sprintf("error increasing order count: %s", err))
	}
}

func AddOutstandingLimitOrdersToOrderbook(
	ctx sdk.Context, keeper *keeper.Keeper,
	limitBuys []*types.Order,
	limitSells []*types.Order,
) {
	for _, order := range limitBuys {
		addOrderToOrderBookEntry(ctx, keeper, order)
	}
	for _, order := range limitSells {
		addOrderToOrderBookEntry(ctx, keeper, order)
	}
}

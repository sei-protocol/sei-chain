package dex

import (
	"sort"

	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/utils/datastructures"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/sei-protocol/sei-chain/x/dex/types/wasm"
)

type BlockOrders struct {
	memStateItems[*types.Order]
}

func NewOrders() *BlockOrders {
	return &BlockOrders{memStateItems: NewItems(utils.PtrCopier[types.Order])}
}

func (o *BlockOrders) Copy() *BlockOrders {
	return &BlockOrders{memStateItems: *o.memStateItems.Copy()}
}

func (o *BlockOrders) MarkFailedToPlaceByAccounts(accounts []string) {
	badAccountSet := datastructures.NewSyncSet(accounts)
	newOrders := []*types.Order{}
	for _, order := range o.internal {
		if badAccountSet.Contains(order.Account) {
			order.Status = types.OrderStatus_FAILED_TO_PLACE
			order.StatusDescription = "Failed liquidation"
		}
		newOrders = append(newOrders, order)
	}
	o.internal = newOrders
}

func (o *BlockOrders) MarkFailedToPlace(failedOrders []wasm.UnsuccessfulOrder) {
	failedOrdersMap := map[uint64]wasm.UnsuccessfulOrder{}
	for _, failedOrder := range failedOrders {
		failedOrdersMap[failedOrder.ID] = failedOrder
	}
	newOrders := []*types.Order{}
	for _, order := range o.internal {
		if failedOrder, ok := failedOrdersMap[order.Id]; ok {
			order.Status = types.OrderStatus_FAILED_TO_PLACE
			order.StatusDescription = failedOrder.Reason
		}
		newOrders = append(newOrders, order)
	}
	o.internal = newOrders
}

func (o *BlockOrders) GetSortedMarketOrders(direction types.PositionDirection, includeLiquidationOrders bool) []*types.Order {
	res := o.getOrdersByCriteria(types.OrderType_MARKET, direction)
	if includeLiquidationOrders {
		res = append(res, o.getOrdersByCriteria(types.OrderType_LIQUIDATION, direction)...)
	}
	sort.Slice(res, func(i, j int) bool {
		// a price of 0 indicates that there is no worst price for the order, so it should
		// always be ranked at the top.
		if res[i].Price.IsZero() {
			return true
		} else if res[j].Price.IsZero() {
			return false
		}
		switch direction {
		case types.PositionDirection_LONG:
			return res[i].Price.GT(res[j].Price)
		case types.PositionDirection_SHORT:
			return res[i].Price.LT(res[j].Price)
		default:
			panic("Unknown direction")
		}
	})
	return res
}

func (o *BlockOrders) GetLimitOrders(direction types.PositionDirection) []*types.Order {
	return o.getOrdersByCriteria(types.OrderType_LIMIT, direction)
}

func (o *BlockOrders) getOrdersByCriteria(orderType types.OrderType, direction types.PositionDirection) []*types.Order {
	res := []*types.Order{}
	for _, order := range o.internal {
		if order.OrderType != orderType || order.PositionDirection != direction {
			continue
		}
		if order.Status == types.OrderStatus_FAILED_TO_PLACE {
			continue
		}
		res = append(res, order)
	}
	return res
}

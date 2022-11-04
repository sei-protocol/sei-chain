package dex

import (
	"sort"

	"github.com/sei-protocol/sei-chain/utils"
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

func (o *BlockOrders) MarkFailedToPlace(failedOrders []wasm.UnsuccessfulOrder) {
	o.mu.Lock()
	defer o.mu.Unlock()
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
	o.mu.Lock()
	defer o.mu.Unlock()

	res := o.getOrdersByCriteria(types.OrderType_MARKET, direction)
	res = append(res, o.getOrdersByCriteria(types.OrderType_FOKMARKET, direction)...)
	if includeLiquidationOrders {
		res = append(res, o.getOrdersByCriteria(types.OrderType_LIQUIDATION, direction)...)
	}
	sort.SliceStable(res, func(i, j int) bool {
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
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.getOrdersByCriteria(types.OrderType_LIMIT, direction)
}

func (o *BlockOrders) GetTriggeredOrders() []*types.Order {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.getOrdersByCriteriaMap(
		map[types.OrderType]bool{
			types.OrderType_STOPLOSS:  true,
			types.OrderType_STOPLIMIT: true,
		},
		map[types.PositionDirection]bool{
			types.PositionDirection_LONG:  true,
			types.PositionDirection_SHORT: true,
		})
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

func (o *BlockOrders) getOrdersByCriteriaMap(orderType map[types.OrderType]bool, direction map[types.PositionDirection]bool) []*types.Order {
	res := []*types.Order{}
	for _, order := range o.internal {
		if _, ok := orderType[order.OrderType]; !ok {
			continue
		}
		if _, ok := direction[order.PositionDirection]; !ok {
			continue
		}
		if order.Status == types.OrderStatus_FAILED_TO_PLACE {
			continue
		}
		res = append(res, order)
	}
	return res
}

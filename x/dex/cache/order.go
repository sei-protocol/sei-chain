package dex

import (
	"encoding/binary"
	"sort"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/sei-protocol/sei-chain/x/dex/types/wasm"
)

type BlockOrders struct {
	orderStore *prefix.Store
}

func NewOrders(orderStore prefix.Store) *BlockOrders {
	return &BlockOrders{orderStore: &orderStore}
}

func (o *BlockOrders) Get() (list []*types.Order) {
	iterator := sdk.KVStorePrefixIterator(o.orderStore, []byte{})

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.Order
		if err := val.Unmarshal(iterator.Value()); err != nil {
			panic(err)
		}
		list = append(list, &val)
	}

	return
}

func (o *BlockOrders) MarkFailedToPlace(failedOrders []wasm.UnsuccessfulOrder) {
	failedOrdersMap := map[uint64]wasm.UnsuccessfulOrder{}
	for _, failedOrder := range failedOrders {
		failedOrdersMap[failedOrder.ID] = failedOrder
	}

	iterator := sdk.KVStorePrefixIterator(o.orderStore, []byte{})

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.Order
		if err := val.Unmarshal(iterator.Value()); err != nil {
			panic(err)
		}
		if failedOrder, ok := failedOrdersMap[val.Id]; ok {
			val.Status = types.OrderStatus_FAILED_TO_PLACE
			val.StatusDescription = failedOrder.Reason
		}
		if bz, err := val.Marshal(); err != nil {
			panic(err)
		} else {
			o.orderStore.Set(iterator.Key(), bz)
		}
	}
}

func (o *BlockOrders) GetSortedMarketOrders(direction types.PositionDirection, includeLiquidationOrders bool) []*types.Order {
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
	return o.getOrdersByCriteria(types.OrderType_LIMIT, direction)
}

func (o *BlockOrders) GetTriggeredOrders() []*types.Order {
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
	iterator := sdk.KVStorePrefixIterator(o.orderStore, []byte{})

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.Order
		if err := val.Unmarshal(iterator.Value()); err != nil {
			panic(err)
		}
		if val.OrderType != orderType || val.PositionDirection != direction {
			continue
		}
		if val.Status == types.OrderStatus_FAILED_TO_PLACE {
			continue
		}
		res = append(res, &val)
	}
	return res
}

func (o *BlockOrders) getOrdersByCriteriaMap(orderType map[types.OrderType]bool, direction map[types.PositionDirection]bool) []*types.Order {
	res := []*types.Order{}
	iterator := sdk.KVStorePrefixIterator(o.orderStore, []byte{})

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.Order
		if err := val.Unmarshal(iterator.Value()); err != nil {
			panic(err)
		}
		if _, ok := orderType[val.OrderType]; !ok {
			continue
		}
		if _, ok := direction[val.PositionDirection]; !ok {
			continue
		}
		if val.Status == types.OrderStatus_FAILED_TO_PLACE {
			continue
		}
		res = append(res, &val)
	}
	return res
}

func (o *BlockOrders) Add(newItem *types.Order) {
	keybz := make([]byte, 8)
	binary.BigEndian.PutUint64(keybz, newItem.Id)
	if valbz, err := newItem.Marshal(); err != nil {
		panic(err)
	} else {
		o.orderStore.Set(keybz, valbz)
	}
}

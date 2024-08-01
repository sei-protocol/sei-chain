package dex

import (
	"encoding/binary"
	"sort"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

type BlockOrders struct {
	orderStore *prefix.Store
}

func NewOrders(orderStore prefix.Store) *BlockOrders {
	return &BlockOrders{orderStore: &orderStore}
}

func (o *BlockOrders) Add(newItem *types.Order) {
	keybz := make([]byte, 8)
	binary.BigEndian.PutUint64(keybz, newItem.Id)
	valbz, err := newItem.Marshal()
	if err != nil {
		panic(err)
	}
	o.orderStore.Set(keybz, valbz)
}

func (o *BlockOrders) GetByID(id uint64) *types.Order {
	keybz := make([]byte, 8)
	binary.BigEndian.PutUint64(keybz, id)
	var val types.Order
	if err := val.Unmarshal(o.orderStore.Get(keybz)); err != nil {
		panic(err)
	}
	return &val
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

func (o *BlockOrders) MarkFailedToPlace(failedOrders []types.UnsuccessfulOrder) {
	failedOrdersMap := map[uint64]types.UnsuccessfulOrder{}
	for _, failedOrder := range failedOrders {
		failedOrdersMap[failedOrder.ID] = failedOrder
	}

	keys, vals := o.getKVsToSet(failedOrdersMap)
	for i, key := range keys {
		o.orderStore.Set(key, vals[i])
	}
}

// getKVsToSet iterate through the kvstore and append the key,val items to a list.
// We should avoid writing or reading from the store directly within the iterator.
func (o *BlockOrders) getKVsToSet(failedOrdersMap map[uint64]types.UnsuccessfulOrder) ([][]byte, [][]byte) {
	iterator := sdk.KVStorePrefixIterator(o.orderStore, []byte{})

	defer iterator.Close()

	var keys [][]byte
	var vals [][]byte
	for ; iterator.Valid(); iterator.Next() {
		var val types.Order
		if err := val.Unmarshal(iterator.Value()); err != nil {
			panic(err)
		}
		if failedOrder, ok := failedOrdersMap[val.Id]; ok {
			val.Status = types.OrderStatus_FAILED_TO_PLACE
			val.StatusDescription = failedOrder.Reason
		}
		bz, err := val.Marshal()
		if err != nil {
			panic(err)
		}
		keys = append(keys, iterator.Key())
		vals = append(vals, bz)
	}
	return keys, vals
}

func (o *BlockOrders) GetSortedMarketOrders(direction types.PositionDirection) []*types.Order {
	res := o.getOrdersByCriteria(types.OrderType_MARKET, direction)
	res = append(res, o.getOrdersByCriteria(types.OrderType_FOKMARKET, direction)...)
	res = append(res, o.getOrdersByCriteria(types.OrderType_FOKMARKETBYVALUE, direction)...)
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

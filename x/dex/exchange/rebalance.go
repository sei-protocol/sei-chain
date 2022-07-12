package exchange

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func RebalanceAllocations(order types.OrderBook) map[uint64]sdk.Dec {
	newTotal := order.GetEntry().Quantity
	oldTotal := sdk.ZeroDec()
	for _, allo := range order.GetEntry().Allocations {
		oldTotal = oldTotal.Add(allo.Quantity)
	}
	ratio := newTotal.Quo(oldTotal)
	res := map[uint64]sdk.Dec{}
	if oldTotal.IsZero() {
		return res
	}
	acc := sdk.ZeroDec()
	for _, allocation := range order.GetEntry().Allocations {
		res[allocation.OrderId] = allocation.Quantity.Mul(ratio)
		acc = acc.Add(res[allocation.OrderId])
	}
	numOrders := uint64(len(order.GetEntry().Allocations))
	var ptr uint64
	for acc.LT(newTotal) {
		orderID := order.GetEntry().Allocations[ptr%numOrders].OrderId
		res[orderID] = res[orderID].Add(sdk.SmallestDec())
		ptr++
		acc = acc.Add(sdk.SmallestDec())
	}
	for acc.GT(newTotal) {
		orderID := order.GetEntry().Allocations[ptr%numOrders].OrderId
		res[orderID] = res[orderID].Sub(sdk.SmallestDec())
		ptr++
		acc = acc.Sub(sdk.SmallestDec())
	}
	return res
}

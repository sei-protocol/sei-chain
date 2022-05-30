package exchange

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func RebalanceAllocations(order types.OrderBook) map[string]sdk.Dec {
	newTotal := order.GetEntry().Quantity
	var oldTotal sdk.Dec = sdk.ZeroDec()
	for _, allo := range order.GetEntry().Allocation {
		oldTotal = oldTotal.Add(allo)
	}
	ratio := newTotal.Quo(oldTotal)
	res := map[string]sdk.Dec{}
	if oldTotal.Equal(sdk.ZeroDec()) {
		return res
	}
	var acc sdk.Dec = sdk.ZeroDec()
	for i, creator := range order.GetEntry().AllocationCreator {
		res[creator] = order.GetEntry().Allocation[i].Mul(ratio)
		acc = acc.Add(res[creator])
	}
	numCreators := uint64(len(order.GetEntry().AllocationCreator))
	var ptr uint64 = 0
	for acc.LT(newTotal) {
		creator := order.GetEntry().AllocationCreator[ptr%numCreators]
		res[creator] = res[creator].Add(sdk.SmallestDec())
		ptr += 1
		acc = acc.Add(sdk.SmallestDec())
	}
	for acc.GT(newTotal) {
		creator := order.GetEntry().AllocationCreator[ptr%numCreators]
		res[creator] = res[creator].Sub(sdk.SmallestDec())
		ptr += 1
		acc = acc.Sub(sdk.SmallestDec())
	}
	return res
}

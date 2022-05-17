package exchange

import "github.com/sei-protocol/sei-chain/x/dex/types"

func RebalanceAllocations(order types.OrderBook) map[string]uint64 {
	newTotal := order.GetEntry().Quantity
	var oldTotal uint64 = 0
	for _, allo := range order.GetEntry().GetAllocation() {
		oldTotal += allo
	}
	res := map[string]uint64{}
	if oldTotal == 0 {
		return res
	}
	var acc uint64 = 0
	for i, creator := range order.GetEntry().AllocationCreator {
		res[creator] = order.GetEntry().GetAllocation()[i] * newTotal / oldTotal
		acc += res[creator]
	}
	numCreators := uint64(len(order.GetEntry().AllocationCreator))
	for i := acc; i < newTotal; i++ {
		res[order.GetEntry().AllocationCreator[(i-acc)%numCreators]] += 1
	}
	return res
}

package types

import (
	"sort"
)

func NewMatchResult(
	orders []*Order,
	cancellations []*Cancellation,
	settlements []*SettlementEntry,
) *MatchResult {
	sort.Slice(orders, func(i, j int) bool {
		if i != j && orders[i].Id == orders[j].Id {
			panic("orders have identical IDs")
		}
		return orders[i].Id < orders[j].Id
	})
	sort.Slice(cancellations, func(i, j int) bool {
		if i != j && cancellations[i].Id == cancellations[j].Id {
			panic("cancnellations have identical IDs")
		}
		return cancellations[i].Id < cancellations[j].Id
	})
	sort.SliceStable(settlements, func(i, j int) bool {
		// settlements for the same order ID are always populated
		// by the same goroutine, so the ordering among those
		// settlements are already deterministic as long as the
		// sorting is stable.
		return settlements[i].OrderId < settlements[j].OrderId
	})
	return &MatchResult{
		Orders:        orders,
		Cancellations: cancellations,
		Settlements:   settlements,
	}
}

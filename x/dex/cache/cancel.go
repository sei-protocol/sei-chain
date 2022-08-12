package dex

import (
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/utils/datastructures"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

type BlockCancellations struct {
	memStateItems[*types.Cancellation]
}

func NewCancels() *BlockCancellations {
	return &BlockCancellations{memStateItems: NewItems(utils.PtrCopier[types.Cancellation])}
}

func (o *BlockCancellations) Copy() *BlockCancellations {
	return &BlockCancellations{memStateItems: *o.memStateItems.Copy()}
}

func (o *BlockCancellations) FilterByIds(idsToRemove []uint64) {
	o.mu.Lock()
	defer o.mu.Unlock()

	newItems := []*types.Cancellation{}
	badIDSet := datastructures.NewSyncSet(idsToRemove)
	for _, cancel := range o.internal {
		if !badIDSet.Contains(cancel.Id) {
			newItems = append(newItems, cancel)
		}
	}
	o.internal = newItems
}

func (o *BlockCancellations) GetIdsToCancel() []uint64 {
	o.mu.Lock()
	defer o.mu.Unlock()

	res := []uint64{}
	for _, cancel := range o.internal {
		res = append(res, cancel.Id)
	}
	return res
}

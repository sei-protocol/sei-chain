package data

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// AnchorFirst is the highest road index for which AppQC is persisted.
// It it also the lowest (first) road index stored in-memory by data.State.
type AnchorFirst struct {
	CommitQC *types.CommitQC
	AppQC    *types.AppQC
}

// Anchor contains the retained certified road and its epoch and successor.
// First is absent before a certified road is retained.
type Anchor struct {
	First  utils.Option[AnchorFirst]
	Epochs [2]*types.Epoch
}

// EpochAt returns the anchor epoch containing road, or None if neither contains it.
func (a Anchor) EpochAt(road types.RoadIndex) utils.Option[*types.Epoch] {
	for _, e := range a.Epochs {
		if e.RoadRange().Has(road) {
			return utils.Some(e)
		}
	}
	return utils.None[*types.Epoch]()
}

// CommitQC returns the certified anchor's CommitQC, or None when First is absent.
func (a Anchor) CommitQC() utils.Option[*types.CommitQC] {
	if first, ok := a.First.Get(); ok {
		return utils.Some(first.CommitQC)
	}
	return utils.None[*types.CommitQC]()
}

// NextIndexEpoch returns the epoch containing NextIndex().
func (a Anchor) NextIndexEpoch() *types.Epoch {
	return a.EpochAt(a.NextIndex()).OrPanic("next index epoch missing")
}

// NextIndex returns the road after the certified anchor, or the first epoch's
// starting road when First is absent.
func (a Anchor) NextIndex() types.RoadIndex {
	if qc, ok := a.CommitQC().Get(); ok {
		return qc.Index() + 1
	}
	return a.Epochs[0].RoadRange().First
}

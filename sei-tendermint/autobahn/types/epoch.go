package types

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// EpochIndex is the epoch number.
type EpochIndex uint64

// RoadRange is a half-open range of RoadIndex values [First, Next).
// Matches GlobalRange / LaneRange: Next is exclusive (Next == lastInclusive+1).
type RoadRange struct {
	First RoadIndex
	Next  RoadIndex
}

// OpenRoadRange returns [0, Max) for tests/genesis when no upper bound is known yet.
func OpenRoadRange() RoadRange { return RoadRange{First: 0, Next: utils.Max[RoadIndex]()} }

func (r RoadRange) Has(idx RoadIndex) bool { return idx >= r.First && idx < r.Next }

// IsLastRoad is true when idx+1 == Next (same shape as GlobalRange.IsLastBlock).
func (r RoadRange) IsLastRoad(idx RoadIndex) bool { return idx+1 == r.Next }

// Epoch holds the complete context for a single epoch.
// Retrieved from the local Registry; never transmitted on the wire.
//
// First global block / timestamp floors are not on Epoch: they come from the
// last CommitQC of the prior epoch (or genesis GenDoc via Registry), and the
// registry does not store CommitQCs.
type Epoch struct {
	utils.ReadOnly
	epochIndex EpochIndex
	roads      RoadRange
	committee  *Committee
}

func NewEpoch(index EpochIndex, roads RoadRange, committee *Committee) *Epoch {
	return &Epoch{
		epochIndex: index,
		roads:      roads,
		committee:  committee,
	}
}

func (e *Epoch) EpochIndex() EpochIndex { return e.epochIndex }
func (e *Epoch) RoadRange() RoadRange   { return e.roads }
func (e *Epoch) Committee() *Committee  { return e.committee }

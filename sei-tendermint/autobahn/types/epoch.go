package types

import (
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// EpochIndex is the epoch number.
type EpochIndex uint64

// RoadRange is a half-open road range [First, Next).
type RoadRange struct {
	First RoadIndex
	Next  RoadIndex
}

// EpochRange is a half-open epoch-index range [First, Next).
type EpochRange struct {
	First EpochIndex
	Next  EpochIndex
}

// OpenRoadRange returns a RoadRange covering all road indices from 0.
func OpenRoadRange() RoadRange { return RoadRange{First: 0, Next: utils.Max[RoadIndex]()} }

func (r RoadRange) Has(idx RoadIndex) bool { return r.First <= idx && idx < r.Next }

func (r EpochRange) Has(idx EpochIndex) bool { return r.First <= idx && idx < r.Next }

// Epoch holds the complete context for a single epoch.
// Retrieved from the local Registry; never transmitted on the wire.
type Epoch struct {
	utils.ReadOnly
	epochIndex     EpochIndex
	roads          RoadRange
	firstTimestamp time.Time
	committee      *Committee
	firstBlock     GlobalBlockNumber
}

// NewEpoch constructs an Epoch.
func NewEpoch(index EpochIndex, roads RoadRange, firstTimestamp time.Time, committee *Committee, firstBlock GlobalBlockNumber) *Epoch {
	return &Epoch{
		epochIndex:     index,
		roads:          roads,
		firstTimestamp: firstTimestamp,
		committee:      committee,
		firstBlock:     firstBlock,
	}
}

func (e *Epoch) EpochIndex() EpochIndex        { return e.epochIndex }
func (e *Epoch) RoadRange() RoadRange          { return e.roads }
func (e *Epoch) FirstTimestamp() time.Time     { return e.firstTimestamp }
func (e *Epoch) Committee() *Committee         { return e.committee }
func (e *Epoch) FirstBlock() GlobalBlockNumber { return e.firstBlock }

// IsClosed reports whether the lane's previous membership period has ended in
// this epoch: Joined is strictly before this epoch and the lane is absent from
// this committee.
func (e *Epoch) IsClosed(lane LaneID) bool {
	return lane.Joined < e.epochIndex && !e.committee.HasLane(lane)
}

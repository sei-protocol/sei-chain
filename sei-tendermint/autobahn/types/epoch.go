package types

import (
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// EpochIndex is the epoch number.
type EpochIndex uint64

// RoadRange is an inclusive range of RoadIndex values [First, Last].
type RoadRange struct {
	First RoadIndex
	Last  RoadIndex
}

// OpenRoadRange returns a RoadRange covering all road indices from 0.
// Use in tests and genesis epochs where no upper bound is known yet.
func OpenRoadRange() RoadRange { return RoadRange{First: 0, Last: utils.Max[RoadIndex]()} }

// Has reports whether idx falls within this range (inclusive on both ends).
func (r RoadRange) Has(idx RoadIndex) bool { return idx >= r.First && idx <= r.Last }

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

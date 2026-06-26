package types

import (
	"math"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// RoadRange is an inclusive range of RoadIndex values [First, Last].
type RoadRange struct {
	First RoadIndex
	Last  RoadIndex
}

// OpenRoadRange returns a RoadRange covering all road indices from 0.
// Use in tests and genesis epochs where no upper bound is known yet.
func OpenRoadRange() RoadRange { return RoadRange{First: 0, Last: math.MaxUint64} }

// Epoch holds the complete context for a single epoch.
// Retrieved from the local Registry; never transmitted on the wire.
type Epoch struct {
	utils.ReadOnly
	epochIndex     uint64
	roads          RoadRange
	firstTimestamp time.Time
	committee      *Committee
	firstBlock     GlobalBlockNumber
}

// NewEpoch constructs an Epoch.
func NewEpoch(index uint64, roads RoadRange, firstTimestamp time.Time, committee *Committee, firstBlock GlobalBlockNumber) *Epoch {
	return &Epoch{
		epochIndex:     index,
		roads:          roads,
		firstTimestamp: firstTimestamp,
		committee:      committee,
		firstBlock:     firstBlock,
	}
}

func (e *Epoch) EpochIndex() uint64            { return e.epochIndex }
func (e *Epoch) Roads() RoadRange              { return e.roads }
func (e *Epoch) FirstTimestamp() time.Time     { return e.firstTimestamp }
func (e *Epoch) Committee() *Committee         { return e.committee }
func (e *Epoch) FirstBlock() GlobalBlockNumber { return e.firstBlock }

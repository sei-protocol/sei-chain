package types

import (
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"

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

// Len returns the number of road indices in the range.
func (r RoadRange) Len() uint64 { return uint64(r.Next - r.First) }

func (r RoadRange) Has(idx RoadIndex) bool { return r.First <= idx && idx < r.Next }

func (r EpochRange) Has(idx EpochIndex) bool { return r.First <= idx && idx < r.Next }

// Epoch holds the complete context for a single epoch.
type Epoch struct {
	utils.ReadOnly
	epochIndex EpochIndex
	roads      RoadRange
	committee  *Committee
}

// NewEpoch constructs an Epoch.
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

// IsClosed reports whether the lane's previous membership period has ended in
// this epoch: Joined is strictly before this epoch and the lane is absent from
// this committee.
func (e *Epoch) IsClosed(lane LaneID) bool {
	return lane.Joined < e.epochIndex && !e.committee.HasLane(lane)
}

// EpochConv converts epochs to and from protobuf records.
var EpochConv = protoutils.Conv[*Epoch, *pb.Epoch]{
	Encode: func(e *Epoch) *pb.Epoch {
		return &pb.Epoch{
			Index:     new(uint64(e.epochIndex)),
			Committee: CommitteeConv.Encode(e.committee),
			FirstRoad: new(uint64(e.roads.First)),
			NextRoad:  new(uint64(e.roads.Next)),
		}
	},
	Decode: func(p *pb.Epoch) (*Epoch, error) {
		if p == nil {
			return nil, errors.New("missing")
		}
		if p.Index == nil {
			return nil, errors.New("index: missing")
		}
		if p.FirstRoad == nil || p.NextRoad == nil {
			return nil, errors.New("roads: missing")
		}
		roads := RoadRange{First: RoadIndex(*p.FirstRoad), Next: RoadIndex(*p.NextRoad)}
		if roads.First >= roads.Next {
			return nil, errors.New("roads: empty or reversed range")
		}
		committee, err := CommitteeConv.DecodeReq(p.Committee)
		if err != nil {
			return nil, fmt.Errorf("committee: %w", err)
		}
		idx := EpochIndex(*p.Index)
		for lane := range committee.Lanes().All() {
			if lane.Joined > idx {
				return nil, fmt.Errorf("member joined epoch %d after committee epoch %d", lane.Joined, idx)
			}
		}
		return NewEpoch(idx, roads, committee), nil
	},
}

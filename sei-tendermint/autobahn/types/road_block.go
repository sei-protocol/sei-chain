package types

import (
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/hashable"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// MaxLaneRangeInProposal is the maximum number of blocks a proposal may advance a lane by.
const MaxLaneRangeInProposal = 10

// LaneRange represents a range [first,next) of blocks of a lane.
type LaneRange struct {
	utils.ReadOnly
	lane     LaneID
	first    LaneHeight
	next     LaneHeight
	lastHash LaneBlockHeaderHash
}

// NewLaneRange constructs a LaneRange.
func NewLaneRange(lane LaneID, first LaneHeight, h utils.Option[*LaneBlockHeader]) *LaneRange {
	if h, ok := h.Get(); ok {
		return &LaneRange{lane: lane, first: first, next: h.Height() + 1, lastHash: h.Hash()}
	}
	return &LaneRange{lane: lane, first: first, next: first, lastHash: LaneBlockHeaderHash{}}
}

// Lane of this block range.
func (m *LaneRange) Lane() LaneID { return m.lane }

// First block of the range.
func (m *LaneRange) First() LaneHeight { return m.first }

// Next is the block after the last block of the range.
func (m *LaneRange) Next() LaneHeight { return m.next }

// Len returns the number of blocks in the range.
func (m *LaneRange) Len() uint64 { return uint64(m.next - m.first) }

// LastHash is the hash of the last block of the range.
// Returns a zero hash for an empty range.
func (m *LaneRange) LastHash() LaneBlockHeaderHash { return m.lastHash }

// Verify verifies the LaneRange against the committee.
func (m *LaneRange) Verify(c *Committee) error {
	if !c.HasLane(m.lane) {
		return fmt.Errorf("%q is not a lane", m.lane)
	}
	if m.first > m.next {
		return fmt.Errorf("invalid range [%v,%v)", m.first, m.next)
	}
	if m.first == m.next && m.lastHash != (LaneBlockHeaderHash{}) {
		return errors.New("non-zero hash for an empty range")
	}
	return nil
}

// RoadHeight is the height of a road block.
type RoadHeight uint64

// RoadBlockHeaderHash is the hash of a RoadBlockHeader.
type RoadBlockHeaderHash hashable.Hash[*pb.RoadBlockHeader]

// Bytes converts the RoadBlockHeaderHash to a byte slice.
func (h RoadBlockHeaderHash) Bytes() []byte  { return h[:] }
func (h RoadBlockHeaderHash) String() string { return hex.EncodeToString(h.Bytes()) }

// ParseRoadBlockHeaderHash constructs a RoadBlockHeaderHash from its raw bytes.
func ParseRoadBlockHeaderHash(bytes []byte) (RoadBlockHeaderHash, error) {
	h, err := hashable.ParseHash[*pb.RoadBlockHeader](bytes)
	return RoadBlockHeaderHash(h), err
}

// RoadBlockHeader identifies a road block and contains its timestamp and lane ranges.
type RoadBlockHeader struct {
	utils.ReadOnly
	epochIndex EpochIndex
	height     RoadHeight
	timestamp  time.Time
	laneRanges map[LaneID]*LaneRange
}

func newRoadBlockHeader(epochIndex EpochIndex, height RoadHeight, timestamp time.Time, laneRanges []*LaneRange) *RoadBlockHeader {
	laneRangesM := map[LaneID]*LaneRange{}
	for _, r := range laneRanges {
		laneRangesM[r.Lane()] = r
	}
	return &RoadBlockHeader{epochIndex: epochIndex, height: height, timestamp: timestamp, laneRanges: laneRangesM}
}

// EpochIndex returns the epoch containing this road block.
func (m *RoadBlockHeader) EpochIndex() EpochIndex { return m.epochIndex }

// Height returns the height of the road block.
func (m *RoadBlockHeader) Height() RoadHeight { return m.height }

// view returns the consensus view for this header and view number.
func (m *RoadBlockHeader) view(number ViewNumber) View {
	return View{Height: m.height, Number: number, EpochIndex: m.epochIndex}
}

// Timestamp of the road block.
func (m *RoadBlockHeader) Timestamp() time.Time { return m.timestamp }

// Hash of the RoadBlockHeader.
func (m *RoadBlockHeader) Hash() RoadBlockHeaderHash {
	return RoadBlockHeaderHash(hashable.ToHash(RoadBlockHeaderConv.Encode(m)))
}

// NumBlocks returns the number of lane blocks finalized by this road block.
func (m *RoadBlockHeader) NumBlocks() uint64 {
	var n uint64
	for _, r := range m.laneRanges {
		n += r.Len()
	}
	return n
}

// Arbitrary deterministic minimal diff between consecutive SuperBlocks.
const minTimestampDiff = time.Microsecond

// NextTimestamp returns the lowest allowed timestamp for the next proposal.
func (m *RoadBlockHeader) NextTimestamp() time.Time { return m.Timestamp().Add(minTimestampDiff) }

// Verify checks epoch binding, road range, and lane-range validity.
func (m *RoadBlockHeader) Verify(ep *Epoch) error {
	if err := m.view(0).Verify(ep); err != nil {
		return fmt.Errorf("view: %w", err)
	}
	c := ep.Committee()
	for _, r := range m.laneRanges {
		if got := r.Len(); got > MaxLaneRangeInProposal {
			return fmt.Errorf("laneRange[%v].Len() = %d, want <= %d", r.Lane(), got, MaxLaneRangeInProposal)
		}
		if err := r.Verify(c); err != nil {
			return fmt.Errorf("laneRange[%v]: %w", r.Lane(), err)
		}
	}
	return nil
}

// LaneRange returns the range of blocks of the given lane.
func (m *RoadBlockHeader) LaneRange(lane LaneID) *LaneRange {
	if r, ok := m.laneRanges[lane]; ok {
		return r
	}
	return NewLaneRange(lane, 0, utils.None[*LaneBlockHeader]())
}

// RoadBlock is a committed QC and all its blocks in header order.
type RoadBlock struct {
	utils.ReadOnly
	qc     *FullCommitQC
	blocks []*LaneBlock
}

// NewRoadBlock constructs a road block with lane blocks in header order.
func NewRoadBlock(qc *FullCommitQC, blocks []*LaneBlock) *RoadBlock {
	return &RoadBlock{qc: qc, blocks: blocks}
}

// Header returns the road block header.
func (b *RoadBlock) Header() *RoadBlockHeader { return b.qc.Header() }

// QC returns the full CommitQC for the road.
func (b *RoadBlock) QC() *FullCommitQC { return b.qc }

// LaneBlocks returns the lane blocks in header order.
func (b *RoadBlock) LaneBlocks() []*LaneBlock { return b.blocks }

// SuperBlock returns the merged transactions and execution metadata for the road.
func (b *RoadBlock) SuperBlock() *SuperBlock {
	superBlock := &SuperBlock{
		Hash:      b.Header().Hash(),
		Height:    b.qc.Height(),
		Timestamp: b.Header().Timestamp(),
	}
	for _, block := range b.blocks {
		superBlock.GasWanted += block.Payload().TotalGasWanted()
		superBlock.Txs = append(superBlock.Txs, block.Payload().Txs()...)
	}
	return superBlock
}

// Verify verifies the QC against the epoch and checks its lane blocks in header order.
func (b *RoadBlock) Verify(ep *Epoch) error {
	if err := b.qc.Verify(ep); err != nil {
		return fmt.Errorf("qc.Verify(): %w", err)
	}
	if len(b.blocks) != len(b.qc.Headers()) {
		return ErrQCBlocksMismatch
	}
	for i, block := range b.blocks {
		if err := block.Verify(); err != nil {
			return fmt.Errorf("block %d: %w: %w", i, ErrQCBlocksMismatch, err)
		}
		if block.Header().Hash() != b.qc.Headers()[i].Hash() {
			return ErrQCBlocksMismatch
		}
	}
	return nil
}

// SuperBlock contains all transactions finalized by one CommitQC in header order.
type SuperBlock struct {
	Hash   RoadBlockHeaderHash
	Height RoadHeight
	// Timestamp extracted from CommitQC.
	Timestamp time.Time
	// Sum of GasWanted of the CommitQC's lane blocks.
	// I.e. sum of GasWanted of Txs.
	GasWanted uint64
	Txs       [][]byte
}

// LaneRangeConv is the protobuf converter for LaneRange.
var LaneRangeConv = protoutils.Conv[*LaneRange, *pb.LaneRange]{
	Encode: func(m *LaneRange) *pb.LaneRange {
		return &pb.LaneRange{
			LaneId:   LaneIDConv.Encode(m.lane),
			First:    new(uint64(m.first)),
			Next:     new(uint64(m.next)),
			LastHash: m.lastHash[:],
		}
	},
	Decode: func(m *pb.LaneRange) (*LaneRange, error) {
		lane, err := LaneIDConv.DecodeReq(m.LaneId)
		if err != nil {
			return nil, fmt.Errorf("lane_id: %w", err)
		}
		if m.First == nil {
			return nil, fmt.Errorf("first: missing")
		}
		if m.Next == nil {
			return nil, fmt.Errorf("next: missing")
		}
		lastHash, err := ParseLaneBlockHeaderHash(m.LastHash)
		if err != nil {
			return nil, fmt.Errorf("last_hash: %w", err)
		}
		return &LaneRange{
			lane:     lane,
			first:    LaneHeight(*m.First),
			next:     LaneHeight(*m.Next),
			lastHash: lastHash,
		}, nil
	},
}

// RoadBlockHeaderConv is the protobuf converter for RoadBlockHeader.
var RoadBlockHeaderConv = protoutils.Conv[*RoadBlockHeader, *pb.RoadBlockHeader]{
	Encode: func(m *RoadBlockHeader) *pb.RoadBlockHeader {
		laneRanges := make([]*LaneRange, 0, len(m.laneRanges))
		for _, r := range m.laneRanges {
			laneRanges = append(laneRanges, r)
		}
		sort.Slice(laneRanges, func(i, j int) bool { return laneRanges[i].Lane().Compare(laneRanges[j].Lane()) < 0 })
		return &pb.RoadBlockHeader{
			EpochIndex: new(uint64(m.epochIndex)),
			Height:     new(uint64(m.height)),
			Timestamp:  TimeConv.Encode(m.timestamp),
			LaneRanges: LaneRangeConv.EncodeSlice(laneRanges),
		}
	},
	Decode: func(m *pb.RoadBlockHeader) (*RoadBlockHeader, error) {
		if m.EpochIndex == nil {
			return nil, fmt.Errorf("epoch_index: missing")
		}
		if m.Height == nil {
			return nil, fmt.Errorf("height: missing")
		}
		timestamp, err := TimeConv.DecodeReq(m.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("timestamp: %w", err)
		}
		laneRanges, err := LaneRangeConv.DecodeSlice(m.LaneRanges)
		if err != nil {
			return nil, fmt.Errorf("lane_ranges: %w", err)
		}
		header := newRoadBlockHeader(EpochIndex(*m.EpochIndex), RoadHeight(*m.Height), timestamp, laneRanges)
		if len(header.laneRanges) != len(laneRanges) {
			return nil, fmt.Errorf("lane_ranges: duplicate ranges")
		}
		return header, nil
	},
}

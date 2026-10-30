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
	first    BlockNumber
	next     BlockNumber
	lastHash BlockHeaderHash
}

// NewLaneRange constructs a LaneRange.
func NewLaneRange(lane LaneID, first BlockNumber, h utils.Option[*BlockHeader]) *LaneRange {
	if h, ok := h.Get(); ok {
		return &LaneRange{lane: lane, first: first, next: h.BlockNumber() + 1, lastHash: h.Hash()}
	}
	return &LaneRange{lane: lane, first: first, next: first, lastHash: BlockHeaderHash{}}
}

// Lane of this block range.
func (m *LaneRange) Lane() LaneID { return m.lane }

// First block of the range.
func (m *LaneRange) First() BlockNumber { return m.first }

// Next is the block after the last block of the range.
func (m *LaneRange) Next() BlockNumber { return m.next }

// Len returns the number of blocks in the range.
func (m *LaneRange) Len() uint64 { return uint64(m.next - m.first) }

// LastHash is the certified tip of this range.
// For a non-empty range it is the hash of block Next()-1.
// For an empty range it is the previous commit's LastHash for this lane,
// or zero when the lane has never been extended.
func (m *LaneRange) LastHash() BlockHeaderHash { return m.lastHash }

// Verify verifies the LaneRange against the committee.
func (m *LaneRange) Verify(c *Committee) error {
	if !c.HasLane(m.lane) {
		return fmt.Errorf("%q is not a lane", m.lane)
	}
	if m.first > m.next {
		return fmt.Errorf("invalid range [%v,%v)", m.first, m.next)
	}
	if m.next == 0 && m.lastHash != (BlockHeaderHash{}) {
		return errors.New("non-zero hash for height 0")
	}
	return nil
}

// RoadIndex is the index of the consensus instance.
type RoadIndex uint64

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
	index      RoadIndex
	timestamp  time.Time
	laneRanges map[LaneID]*LaneRange
}

func newRoadBlockHeader(epochIndex EpochIndex, index RoadIndex, timestamp time.Time, laneRanges []*LaneRange) *RoadBlockHeader {
	laneRangesM := map[LaneID]*LaneRange{}
	for _, r := range laneRanges {
		laneRangesM[r.Lane()] = r
	}
	return &RoadBlockHeader{epochIndex: epochIndex, index: index, timestamp: timestamp, laneRanges: laneRangesM}
}

// EpochIndex returns the epoch containing this road block.
func (m *RoadBlockHeader) EpochIndex() EpochIndex { return m.epochIndex }

// Index returns the road block index.
func (m *RoadBlockHeader) Index() RoadIndex { return m.index }

// view returns the consensus view for this header and view number.
func (m *RoadBlockHeader) view(number ViewNumber) View {
	return View{Index: m.index, Number: number, EpochIndex: m.epochIndex}
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
	return NewLaneRange(lane, 0, utils.None[*BlockHeader]())
}

// RoadBlock is a committed QC and all its blocks in header order.
type RoadBlock struct {
	utils.ReadOnly
	qc         *FullCommitQC
	laneBlocks []*Block
}

// NewRoadBlock constructs a road block with lane blocks in header order.
func NewRoadBlock(qc *FullCommitQC, laneBlocks []*Block) *RoadBlock {
	return &RoadBlock{qc: qc, laneBlocks: laneBlocks}
}

// Header returns the road block header.
func (b *RoadBlock) Header() *RoadBlockHeader { return b.qc.Header() }

// QC returns the full CommitQC for the road.
func (b *RoadBlock) QC() *FullCommitQC { return b.qc }

// LaneBlocks returns the lane blocks in header order.
func (b *RoadBlock) LaneBlocks() []*Block { return b.laneBlocks }

// SuperBlock returns the merged transactions and execution metadata for the road.
func (b *RoadBlock) SuperBlock() *SuperBlock {
	superBlock := &SuperBlock{
		Hash:      b.Header().Hash(),
		Height:    b.qc.Index(),
		Timestamp: b.Header().Timestamp(),
	}
	for _, block := range b.laneBlocks {
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
	if len(b.laneBlocks) != len(b.qc.Headers()) {
		return ErrQCBlocksMismatch
	}
	for i, block := range b.laneBlocks {
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
	Height RoadIndex
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
			First:    utils.Alloc(uint64(m.first)),
			Next:     utils.Alloc(uint64(m.next)),
			LastHash: m.lastHash[:],
		}
	},
	Decode: func(m *pb.LaneRange) (*LaneRange, error) {
		lane, err := LaneIDConv.DecodeReq(m.LaneId)
		if err != nil {
			return nil, fmt.Errorf("Lane: %w", err)
		}
		if m.First == nil {
			return nil, fmt.Errorf("First: missing")
		}
		if m.Next == nil {
			return nil, fmt.Errorf("Next: missing")
		}
		lastHash, err := ParseBlockHeaderHash(m.LastHash)
		if err != nil {
			return nil, fmt.Errorf("LastHash: %w", err)
		}
		return &LaneRange{
			lane:     lane,
			first:    BlockNumber(*m.First),
			next:     BlockNumber(*m.Next),
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
			EpochIndex: utils.Alloc(uint64(m.epochIndex)),
			Index:      utils.Alloc(uint64(m.index)),
			Timestamp:  TimeConv.Encode(m.timestamp),
			LaneRanges: LaneRangeConv.EncodeSlice(laneRanges),
		}
	},
	Decode: func(m *pb.RoadBlockHeader) (*RoadBlockHeader, error) {
		if m.EpochIndex == nil {
			return nil, fmt.Errorf("epoch_index: missing")
		}
		if m.Index == nil {
			return nil, fmt.Errorf("index: missing")
		}
		timestamp, err := TimeConv.DecodeReq(m.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("timestamp: %w", err)
		}
		laneRanges, err := LaneRangeConv.DecodeSlice(m.LaneRanges)
		if err != nil {
			return nil, fmt.Errorf("laneRanges: %w", err)
		}
		header := newRoadBlockHeader(EpochIndex(*m.EpochIndex), RoadIndex(*m.Index), timestamp, laneRanges)
		if len(header.laneRanges) != len(laneRanges) {
			return nil, fmt.Errorf("laneRanges: duplicate ranges")
		}
		return header, nil
	},
}

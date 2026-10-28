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
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
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

// ViewNumber is the view number of the consensus instance.
type ViewNumber uint64

// Next view number.
func (n ViewNumber) Next() ViewNumber { return n + 1 }

// View represents a consensus view.
type View struct {
	Index      RoadIndex
	Number     ViewNumber
	EpochIndex EpochIndex
}

// Less checks if v is earlier than b.
func (v View) Less(b View) bool {
	if v.EpochIndex != b.EpochIndex {
		return v.EpochIndex < b.EpochIndex
	}
	if v.Index != b.Index {
		return v.Index < b.Index
	}
	return v.Number < b.Number
}

// Verify checks that the view's epoch index and road index are consistent with ep.
func (v View) Verify(ep *Epoch) error {
	if got, want := v.EpochIndex, ep.EpochIndex(); got != want {
		return fmt.Errorf("epoch_index = %d, want %d", got, want)
	}
	if rr := ep.RoadRange(); !rr.Has(v.Index) {
		return fmt.Errorf("road_index %v not in epoch roads [%v, %v)", v.Index, rr.First, rr.Next)
	}
	return nil
}

// Next returns the next view.
func (v View) Next() View {
	v.Number = v.Number.Next()
	return v
}

// ConsensusSpec is the durable CommitQC tip paired with the epoch of the
// RoadIndex that follows it. CommitQC is None before the first tip; until then
// Epoch is genesis epoch 0. Consensus advances with a spec verbatim.
type ConsensusSpec struct {
	CommitQC utils.Option[*CommitQC]
	Epoch    *Epoch
}

// Index is the RoadIndex of the next view: CommitQC.Index()+1, or the start of
// the genesis epoch if there is no tip.
func (s ConsensusSpec) Index() RoadIndex {
	if qc, ok := s.CommitQC.Get(); ok {
		return qc.Index() + 1
	}
	return s.Epoch.RoadRange().First
}

// ViewSpec is ConsensusSpec plus the TimeoutQC for the current view number.
// Epoch is required; View() and NextTimestamp() panic if it is nil.
type ViewSpec struct {
	ConsensusSpec
	// WARNING: currently we have implicit assumption that
	// TimeoutQC.View().Index == CommitQC.Index.Next(),
	// I.e. that TimeoutQC comes from the expected consensus instance.
	TimeoutQC utils.Option[*TimeoutQC]
}

// View is the view justified by vs.
func (vs *ViewSpec) View() View {
	idx := vs.Index()
	if view := NextViewOpt(vs.TimeoutQC); view.Index == idx {
		view.EpochIndex = vs.Epoch.EpochIndex()
		return view
	}
	return View{Index: idx, Number: 0, EpochIndex: vs.Epoch.EpochIndex()}
}

func (vs *ViewSpec) NextTimestamp() time.Time {
	if cQC, ok := vs.CommitQC.Get(); ok {
		return cQC.Header().NextTimestamp()
	}
	return vs.Epoch.FirstTimestamp()
}

// TimestampRange returns the earliest and latest proposal timestamps accepted
// for this view, inclusive. The width is viewTimeout * (view number + 1).
func (vs *ViewSpec) TimestampRange(viewTimeout time.Duration) (earliest, latest time.Time) {
	earliest = vs.NextTimestamp()
	latest = earliest.Add(viewTimeout * time.Duration(vs.View().Number+1)) //nolint:gosec // view number stays far below 2^63
	return earliest, latest
}

// ClampTimestamp returns timestamp limited to TimestampRange(viewTimeout).
func (vs *ViewSpec) ClampTimestamp(timestamp time.Time, viewTimeout time.Duration) time.Time {
	earliest, latest := vs.TimestampRange(viewTimeout)
	if timestamp.Before(earliest) {
		return earliest
	}
	if timestamp.After(latest) {
		return latest
	}
	return timestamp
}

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

// Proposal is a road block header proposed at a consensus view.
type Proposal struct {
	utils.ReadOnly
	viewNumber ViewNumber
	header     *RoadBlockHeader
}

func newRoadBlockHeader(epochIndex EpochIndex, index RoadIndex, timestamp time.Time, laneRanges []*LaneRange) *RoadBlockHeader {
	laneRangesM := map[LaneID]*LaneRange{}
	for _, r := range laneRanges {
		laneRangesM[r.Lane()] = r
	}
	return &RoadBlockHeader{epochIndex: epochIndex, index: index, timestamp: timestamp, laneRanges: laneRangesM}
}

// Index of the proposal.
func (m *Proposal) Index() RoadIndex { return m.header.Index() }

// View of the proposal.
func (m *Proposal) View() View { return m.header.view(m.viewNumber) }

// Header returns the road block header.
func (m *Proposal) Header() *RoadBlockHeader { return m.header }

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

// Verify checks epoch binding and lane-range bounds, lengths, committee membership,
// and empty height-0 hashes. FullProposal.Verify additionally checks QC-chain continuity.
func (m *Proposal) Verify(ep *Epoch) error {
	return m.header.Verify(ep)
}

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

// FullProposal is a proposal with justification.
type FullProposal struct {
	utils.ReadOnly
	proposal  *Signed[*Proposal]
	laneQCs   map[LaneID]*LaneQC
	timeoutQC utils.Option[*TimeoutQC]
}

// NewReproposal creates a new reproposal based on viewSpec.
// Returns false if reproposal is not expected.
func NewReproposal(
	key SecretKey,
	viewSpec ViewSpec,
) (*FullProposal, bool) {
	timeoutQC, ok := viewSpec.TimeoutQC.Get()
	if !ok {
		return nil, false
	}
	p, ok := timeoutQC.reproposal()
	if !ok {
		return nil, false
	}
	return &FullProposal{
		proposal:  Sign(key, p),
		timeoutQC: utils.Some(timeoutQC),
	}, true
}

// NewProposal creates a new FullProposal signed by key.
// A reproposal re-signs the locked proposal and ignores timestamp and laneQCs.
func NewProposal(
	key SecretKey,
	viewSpec ViewSpec,
	timestamp time.Time,
	laneQCs map[LaneID]*LaneQC,
) (*FullProposal, error) {
	committee := viewSpec.Epoch.Committee()
	if got, want := key.Public(), committee.Leader(viewSpec.View()); got != want {
		return nil, fmt.Errorf("key %q is not the leader %q for view %v", got, want, viewSpec.View())
	}
	if p, ok := NewReproposal(key, viewSpec); ok {
		return p, nil
	}
	proposal, err := buildProposal(committee, viewSpec, timestamp, laneQCs)
	if err != nil {
		return nil, err
	}
	return &FullProposal{
		proposal:  Sign(key, proposal),
		laneQCs:   laneQCs,
		timeoutQC: viewSpec.TimeoutQC,
	}, nil
}

// buildProposal constructs the unsigned Proposal message and returns it along with the
// (possibly cleared) appQC. It contains the non-signing body shared by NewProposal and
// NewProposalForTesting.
func buildProposal(
	committee *Committee,
	viewSpec ViewSpec,
	timestamp time.Time,
	laneQCs map[LaneID]*LaneQC,
) (*Proposal, error) {
	var laneRanges []*LaneRange
	for lane := range committee.Lanes().All() {
		prev := LaneRangeOpt(viewSpec.CommitQC, lane)
		first := prev.Next()
		if lQC, ok := laneQCs[lane]; ok {
			if lQC.Header().Lane() != lane {
				return nil, fmt.Errorf("laneQC %v for lane %v", lQC.Header().Lane(), lane)
			}
			laneRange := NewLaneRange(lane, first, utils.Some(lQC.Header()))
			if got := laneRange.Len(); got > MaxLaneRangeInProposal {
				return nil, fmt.Errorf("laneRange[%v].Len() = %d, want <= %d", lane, got, MaxLaneRangeInProposal)
			}
			laneRanges = append(laneRanges, laneRange)
		} else {
			laneRanges = append(laneRanges, &LaneRange{lane: lane, first: first, next: first, lastHash: prev.LastHash()})
		}
	}
	return &Proposal{viewNumber: viewSpec.View().Number, header: newRoadBlockHeader(viewSpec.View().EpochIndex, viewSpec.View().Index, timestamp, laneRanges)}, nil
}

// NewProposalForTesting builds a FullProposal exactly like NewProposal but attaches the
// provided (typically fake) signature instead of signing with a secret key. FOR
// TESTS/BENCHMARKS ONLY: the resulting proposal will NOT verify. Unlike NewProposal it
// does not support the reproposal path, so viewSpec.TimeoutQC must be None.
func NewProposalForTesting(
	committee *Committee,
	viewSpec ViewSpec,
	timestamp time.Time,
	laneQCs map[LaneID]*LaneQC,
	sig *Signature,
) (*FullProposal, error) {
	proposal, err := buildProposal(committee, viewSpec, timestamp, laneQCs)
	if err != nil {
		return nil, err
	}
	return &FullProposal{
		proposal:  newSigned(proposal, sig),
		laneQCs:   laneQCs,
		timeoutQC: viewSpec.TimeoutQC,
	}, nil
}

// Proposal .
func (m *FullProposal) Proposal() *Signed[*Proposal] { return m.proposal }

// Header returns the proposed road block header.
func (m *FullProposal) Header() *RoadBlockHeader { return m.proposal.Msg().Header() }

// View .
func (m *FullProposal) View() View {
	return m.proposal.Msg().View()
}

// LaneQC .
func (m *FullProposal) LaneQC(lane LaneID) (*LaneQC, bool) {
	qc, ok := m.laneQCs[lane]
	return qc, ok
}

// TimeoutQC returns the timeout QC if it exists.
func (m *FullProposal) TimeoutQC() utils.Option[*TimeoutQC] {
	return m.timeoutQC
}

// Verify checks the FullProposal against vs.
// Timestamps outside TimestampRange(viewTimeout) are rejected.
func (m *FullProposal) Verify(vs ViewSpec, viewTimeout time.Duration) error {
	c := vs.Epoch.Committee()
	return scope.Parallel(func(s scope.ParallelScope) error {
		// Does the view match?
		if got, want := m.proposal.Msg().View(), vs.View(); got != want {
			return fmt.Errorf("view = %v, want %v", m.View(), vs.View())
		}

		earliest, latest := vs.TimestampRange(viewTimeout)
		if got := m.Header().Timestamp(); got.Before(earliest) || got.After(latest) {
			return fmt.Errorf("proposal.Header().Timestamp() = %v, want >= %v and <= %v", got, earliest, latest)
		}
		// Is proposer valid?
		if got, want := m.proposal.sig.key, c.Leader(vs.View()); got != want {
			return fmt.Errorf("proposer %q, want %q", got, want)
		}
		// Verify the proposer's signature.
		if err := m.proposal.VerifySig(); err != nil {
			return fmt.Errorf("proposal signature: %w", err)
		}
		// Do we have the required timeoutQC?
		if got, want := NextViewOpt(vs.TimeoutQC), NextViewOpt(m.timeoutQC); got != want {
			return errors.New("inconsistent timeoutQC")
		}
		// Verify timeoutQC.
		if tQC, ok := m.timeoutQC.Get(); ok {
			s.Spawn(func() error {
				if err := tQC.Verify(vs.Epoch, vs.CommitQC); err != nil {
					return fmt.Errorf("timeoutQC: %w", err)
				}
				return nil
			})
			// Is this a reproposal?
			if want, ok := tQC.reproposal(); ok {
				if len(m.laneQCs) > 0 {
					return errors.New("unnecessary data when reproposing")
				}
				if NewHashed(want).Hash() != m.proposal.hashed.hash {
					return fmt.Errorf("want reproposal %v, got %v", want, m.proposal)
				}
				// Valid reproposal, no further verification needed.
				return nil
			}
		}
		// Verify the proposal's epoch binding, road range, lane structure, and membership.
		proposal := m.proposal.Msg()
		if err := proposal.Verify(vs.Epoch); err != nil {
			return fmt.Errorf("proposal: %w", err)
		}
		// Verify each lane range against the previous commitQC and its laneQC justification.
		for lane := range c.Lanes().All() {
			r := proposal.Header().LaneRange(lane)
			prev := LaneRangeOpt(vs.CommitQC, r.Lane())
			// Verify that range matches previous commitQC.
			if got, want := r.First(), prev.Next(); got != want {
				return fmt.Errorf("laneRange[%v].First() = %v, want %v", r.Lane(), got, want)
			}
			// An empty range carries the previous commit's LastHash.
			if r.Len() == 0 {
				if got, want := r.LastHash(), prev.LastHash(); got != want {
					return fmt.Errorf("laneRange[%v].LastHash() = %v, want %v", r.Lane(), got, want)
				}
			}
			// Verify that the necessary laneQC is present and valid.
			if r.First() < r.Next() {
				qc, ok := m.LaneQC(r.Lane())
				if !ok {
					return fmt.Errorf("missing qc for %q", r.Lane())
				}
				if got, want := qc.Header().BlockNumber(), r.Next()-1; got != want {
					return fmt.Errorf("qc[%v].BlockNumber() = %v, want %v", r.Lane(), got, want)
				}
				if got, want := qc.Header().Hash(), r.LastHash(); got != want {
					return fmt.Errorf("qc[%v].Header().Hash() = %v, want %v", r.Lane(), got, want)
				}
				s.Spawn(func() error {
					if err := qc.Verify(c); err != nil {
						return fmt.Errorf("qc[%v]: %w", r.Lane(), err)
					}
					return nil
				})
			}
		}
		return nil
	})
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

// ProposalConv is the protobuf converter for Proposal.
var ProposalConv = protoutils.Conv[*Proposal, *pb.Proposal]{
	Encode: func(m *Proposal) *pb.Proposal {
		return &pb.Proposal{ViewNumber: utils.Alloc(uint64(m.viewNumber)), Header: RoadBlockHeaderConv.Encode(m.header)}
	},
	Decode: func(m *pb.Proposal) (*Proposal, error) {
		if m.ViewNumber == nil {
			return nil, fmt.Errorf("view_number: missing")
		}
		header, err := RoadBlockHeaderConv.DecodeReq(m.Header)
		if err != nil {
			return nil, fmt.Errorf("header: %w", err)
		}
		return &Proposal{viewNumber: ViewNumber(*m.ViewNumber), header: header}, nil
	},
}

// FullProposalConv is the protobuf converter for FullProposal.
var FullProposalConv = protoutils.Conv[*FullProposal, *pb.FullProposal]{
	Encode: func(m *FullProposal) *pb.FullProposal {
		laneQCs := make([]*LaneQC, 0, len(m.laneQCs))
		for _, qc := range m.laneQCs {
			laneQCs = append(laneQCs, qc)
		}
		return &pb.FullProposal{
			ProposalV2: SignedProposalConv.Encode(m.proposal),
			LaneQcs:    LaneQCConv.EncodeSlice(laneQCs),
			TimeoutQc:  TimeoutQCConv.EncodeOpt(m.timeoutQC),
		}
	},
	Decode: func(m *pb.FullProposal) (*FullProposal, error) {
		proposal, err := SignedProposalConv.DecodeReq(m.ProposalV2)
		if err != nil {
			return nil, fmt.Errorf("proposal: %w", err)
		}
		laneQCs, err := LaneQCConv.DecodeSlice(m.LaneQcs)
		if err != nil {
			return nil, fmt.Errorf("laneQCs: %w", err)
		}
		laneQCsMap := map[LaneID]*LaneQC{}
		for _, qc := range laneQCs {
			laneQCsMap[qc.Header().Lane()] = qc
		}
		timeoutQC, err := TimeoutQCConv.DecodeOpt(m.TimeoutQc)
		if err != nil {
			return nil, fmt.Errorf("timeoutQC: %w", err)
		}
		return &FullProposal{proposal: proposal, laneQCs: laneQCsMap, timeoutQC: timeoutQC}, nil
	},
}

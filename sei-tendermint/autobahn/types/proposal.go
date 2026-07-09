package types

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
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

// LastHash is the hash of the last block of the range.
// Returns a zero hash for an empty range.
func (m *LaneRange) LastHash() BlockHeaderHash { return m.lastHash }

// Verify verifies the LaneRange against the committee.
func (m *LaneRange) Verify(c *Committee) error {
	if !c.HasLane(m.lane) {
		return fmt.Errorf("%q is not a lane", m.lane)
	}
	if m.first > m.next {
		return fmt.Errorf("invalid range [%v,%v)", m.first, m.next)
	}
	if m.first == m.next && m.lastHash != (BlockHeaderHash{}) {
		return errors.New("non-zero hash for an empty range")
	}
	return nil
}

// GlobalRange represents a [First,Next) range of global blocks.
type GlobalRange struct {
	First GlobalBlockNumber
	Next  GlobalBlockNumber
}

// Len returns the number of global blocks in the range.
func (g GlobalRange) Len() uint64 {
	return uint64(g.Next - g.First)
}

func (g GlobalRange) Has(n GlobalBlockNumber) bool {
	return g.First <= n && n < g.Next
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
		return fmt.Errorf("road_index %v not in epoch roads [%v, %v]", v.Index, rr.First, rr.Last)
	}
	return nil
}

// Next returns the next view.
func (v View) Next() View {
	v.Number = v.Number.Next()
	return v
}

// ViewSpec is the full local context for starting a view: justification QCs plus
// the epoch active at that view. Epoch is required; View(), NextGlobalBlock(), and
// NextTimestamp() panic if it is nil.
type ViewSpec struct {
	// WARNING: currently we have implicit assumption that
	// TimeoutQC.View().Index == CommitQC.Index.Next(),
	// I.e. that TimeoutQC comes from the expected consensus instance.
	CommitQC  utils.Option[*CommitQC]
	TimeoutQC utils.Option[*TimeoutQC]
	Epoch     *Epoch // required
}

// NextGlobalBlock returns the first global block number expected in the next proposal.
// CommitQC is None only at global block 0 (genesis), in which case it returns Epoch[0].FirstBlock.
// For all other views, including the first view of a non-genesis epoch, CommitQC is present and it returns CommitQC.GlobalRange().Next.
func (vs *ViewSpec) NextGlobalBlock() GlobalBlockNumber {
	if cQC, ok := vs.CommitQC.Get(); ok {
		return cQC.GlobalRange().Next
	}
	return vs.Epoch.FirstBlock()
}

// View is the view justified by vs.
func (vs *ViewSpec) View() View {
	idx := NextIndexOpt(vs.CommitQC)
	if view := NextViewOpt(vs.TimeoutQC); view.Index == idx {
		view.EpochIndex = vs.Epoch.EpochIndex()
		return view
	}
	return View{Index: idx, Number: 0, EpochIndex: vs.Epoch.EpochIndex()}
}

func (vs *ViewSpec) NextTimestamp() time.Time {
	if cQC, ok := vs.CommitQC.Get(); ok {
		return cQC.Proposal().NextTimestamp()
	}
	return vs.Epoch.FirstTimestamp()
}

// Proposal is the road tipcut proposal.
// It consists of ranges of blocks of each lane.
// AppQC could be nil if we haven't reached any quorum state hash.
type Proposal struct {
	utils.ReadOnly
	view        View
	timestamp   time.Time
	laneRanges  map[LaneID]*LaneRange
	app         utils.Option[*AppProposal]
	globalRange GlobalRange
}

func newProposal(view View, timestamp time.Time, laneRanges []*LaneRange, app utils.Option[*AppProposal], globalFirst GlobalBlockNumber) *Proposal {
	laneRangesM := map[LaneID]*LaneRange{}
	gr := GlobalRange{First: globalFirst, Next: globalFirst}
	for _, r := range laneRanges {
		laneRangesM[r.Lane()] = r
	}
	for _, r := range laneRangesM {
		gr.Next += GlobalBlockNumber(r.Next() - r.First())
	}
	return &Proposal{
		view:        view,
		timestamp:   timestamp,
		laneRanges:  laneRangesM,
		globalRange: gr,
		app:         app,
	}
}

// Index of the proposal.
func (m *Proposal) Index() RoadIndex { return m.view.Index }

// View of the proposal.
func (m *Proposal) View() View { return m.view }

// Timestamp of the proposal.
func (m *Proposal) Timestamp() time.Time { return m.timestamp }

// App .
func (m *Proposal) App() utils.Option[*AppProposal] { return m.app }

// EpochIndex returns the epoch index encoded in the proposal.
func (m *Proposal) EpochIndex() EpochIndex { return m.view.EpochIndex }

// GlobalRange returns the proposed global block range.
func (m *Proposal) GlobalRange() GlobalRange {
	return m.globalRange
}

// Arbitrary deterministic minimal diff between consecutive blocks.
const minTimestampDiff = time.Microsecond

// Monotone timestamp assigned to each block of the proposal.
// Returns None if n does not belong to the proposal's global range.
func (m *Proposal) BlockTimestamp(n GlobalBlockNumber) utils.Option[time.Time] {
	gr := m.GlobalRange()
	if !gr.Has(n) {
		return utils.None[time.Time]()
	}
	//nolint:gosec // TODO: do stricter timestamp validation before running in prod.
	return utils.Some(m.Timestamp().Add(time.Duration(n-gr.First) * minTimestampDiff))
}

// Lowest allowed timestamp for the next index proposal.
func (m *Proposal) NextTimestamp() time.Time {
	//nolint:gosec // TODO: do stricter timestamp validation before running in prod.
	return m.Timestamp().Add(time.Duration(m.globalRange.Len()) * minTimestampDiff)
}

// Verify checks epoch binding, lane-range structural validity (bounds, max-length,
// and lane committee membership), and proposal-hash integrity. QC-chain continuity
// (matching starts against the previous QC) is only enforced by FullProposal.Verify.
func (m *Proposal) Verify(ep *Epoch) error {
	if err := m.view.Verify(ep); err != nil {
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
func (m *Proposal) LaneRange(lane LaneID) *LaneRange {
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
	appQC     utils.Option[*AppQC]
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

// NewProposal creates a new FullProposal.
// timestamp might get replaced to ensure that timestamps are monotone.
func NewProposal(
	key SecretKey,
	viewSpec ViewSpec,
	timestamp time.Time,
	laneQCs map[LaneID]*LaneQC,
	appQC utils.Option[*AppQC],
) (*FullProposal, error) {
	committee := viewSpec.Epoch.Committee()
	if got, want := key.Public(), committee.Leader(viewSpec.View()); got != want {
		return nil, fmt.Errorf("key %q is not the leader %q for view %v", got, want, viewSpec.View())
	}
	if p, ok := NewReproposal(key, viewSpec); ok {
		return p, nil
	}
	proposal, appQC, err := buildProposal(committee, viewSpec, timestamp, laneQCs, appQC)
	if err != nil {
		return nil, err
	}
	return &FullProposal{
		proposal:  Sign(key, proposal),
		laneQCs:   laneQCs,
		appQC:     appQC,
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
	appQC utils.Option[*AppQC],
) (*Proposal, utils.Option[*AppQC], error) {
	var laneRanges []*LaneRange
	for lane := range committee.Lanes().All() {
		first := LaneRangeOpt(viewSpec.CommitQC, lane).Next()
		if lQC, ok := laneQCs[lane]; ok {
			if lQC.Header().Lane() != lane {
				return nil, appQC, fmt.Errorf("laneQC %v for lane %v", lQC.Header().Lane(), lane)
			}
			laneRange := NewLaneRange(lane, first, utils.Some(lQC.Header()))
			if got := laneRange.Len(); got > MaxLaneRangeInProposal {
				return nil, appQC, fmt.Errorf("laneRange[%v].Len() = %d, want <= %d", lane, got, MaxLaneRangeInProposal)
			}
			laneRanges = append(laneRanges, laneRange)
		} else {
			laneRanges = append(laneRanges, NewLaneRange(lane, first, utils.None[*BlockHeader]()))
		}
	}
	app := ProposalOpt(appQC)
	// If the new appProposal is not later than the previous one, then clear appQC.
	if old := AppOpt(ProposalOpt(viewSpec.CommitQC)); NextOpt(app) <= NextOpt(old) {
		app = old
		appQC = utils.None[*AppQC]()
	}
	// If the new appProposal is from the future (which may happen if this node is behind), then clear appQC.
	// The proposal will be useless in this case, but at least it will be valid.
	if a, ok := app.Get(); ok && a.GlobalNumber() >= viewSpec.NextGlobalBlock() {
		app = utils.None[*AppProposal]()
		appQC = utils.None[*AppQC]()
	}
	// Normalize the creation timestamp.
	if wantMin := viewSpec.NextTimestamp(); timestamp.Before(wantMin) {
		timestamp = wantMin
	}
	return newProposal(viewSpec.View(), timestamp, laneRanges, app, viewSpec.NextGlobalBlock()), appQC, nil
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
	appQC utils.Option[*AppQC],
	sig *Signature,
) (*FullProposal, error) {
	proposal, appQC, err := buildProposal(committee, viewSpec, timestamp, laneQCs, appQC)
	if err != nil {
		return nil, err
	}
	return &FullProposal{
		proposal:  newSigned(proposal, sig),
		laneQCs:   laneQCs,
		appQC:     appQC,
		timeoutQC: viewSpec.TimeoutQC,
	}, nil
}

// Proposal .
func (m *FullProposal) Proposal() *Signed[*Proposal] { return m.proposal }

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

// Verify verifies the FullProposal against the current view.
func (m *FullProposal) Verify(vs ViewSpec) error {
	c := vs.Epoch.Committee()
	return scope.Parallel(func(s scope.ParallelScope) error {
		// Does the view match?
		if got, want := m.proposal.Msg().View(), vs.View(); got != want {
			return fmt.Errorf("view = %v, want %v", m.View(), vs.View())
		}
		if got, want := m.proposal.Msg().GlobalRange().First, vs.NextGlobalBlock(); got != want {
			return fmt.Errorf("proposal.GlobalRange().First = %v, want %v", got, want)
		}
		// Is the timestamp monotone?
		if got, wantMin := m.proposal.Msg().Timestamp(), vs.NextTimestamp(); got.Before(wantMin) {
			return fmt.Errorf("proposal.Timestamp() = %v, want >= %v", got, wantMin)
		}
		// Is proposer valid?
		if got, want := m.proposal.sig.key, c.Leader(vs.View()); got != want {
			return fmt.Errorf("proposer %q, want %q", got, want)
		}
		// Verify the proposer's signature.
		if err := m.proposal.VerifySig(c); err != nil {
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
				if len(m.laneQCs) > 0 || m.appQC.IsPresent() {
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
			r := proposal.LaneRange(lane)
			// Verify that range matches previous commitQC.
			if got, want := r.First(), LaneRangeOpt(vs.CommitQC, r.Lane()).Next(); got != want {
				return fmt.Errorf("laneRange[%v].First() = %v, want %v", r.Lane(), got, want)
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
		// Verify the appQC.
		if got, wantMin := NextOpt(m.proposal.Msg().App()), NextOpt(AppOpt(ProposalOpt(vs.CommitQC))); got < wantMin {
			return errors.New("AppProposal lower than in previous CommitQC")
		} else if got == wantMin {
			if m.appQC.IsPresent() {
				return errors.New("unnecessary appQC")
			}
		} else {
			app, _ := m.proposal.Msg().App().Get()
			// TODO: relax to allow current_epoch-1 once epoch transitions are wired up.
			if got, want := app.EpochIndex(), m.proposal.Msg().EpochIndex(); got != want {
				return fmt.Errorf("app epoch_index %d != proposal epoch_index %d", got, want)
			}
			appQC, ok := m.appQC.Get()
			if !ok {
				return errors.New("appQC missing")
			}
			if appQC.vote.hash != NewHashed(NewAppVote(app)).hash {
				return errors.New("appQC doesn't match the proposal")
			}
			s.Spawn(func() error {
				if err := appQC.Verify(c); err != nil {
					return fmt.Errorf("appQC: %w", err)
				}
				return nil
			})
			if got, want := appQC.Proposal().GlobalNumber(), vs.NextGlobalBlock(); got >= want {
				return fmt.Errorf("appQC for block %v, while only %v blocks were finalized", got, want)
			}
		}
		return nil
	})
}

// LaneRangeConv is the protobuf converter for LaneRange.
var LaneRangeConv = protoutils.Conv[*LaneRange, *pb.LaneRange]{
	Encode: func(m *LaneRange) *pb.LaneRange {
		return &pb.LaneRange{
			Lane:     PublicKeyConv.Encode(m.lane),
			First:    utils.Alloc(uint64(m.first)),
			Next:     utils.Alloc(uint64(m.next)),
			LastHash: m.lastHash[:],
		}
	},
	Decode: func(m *pb.LaneRange) (*LaneRange, error) {
		lane, err := PublicKeyConv.DecodeReq(m.Lane)
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

// ViewConv is the protobuf converter for View.
var ViewConv = protoutils.Conv[View, *pb.View]{
	Encode: func(m View) *pb.View {
		return &pb.View{
			Index:      utils.Alloc(uint64(m.Index)),
			Number:     utils.Alloc(uint64(m.Number)),
			EpochIndex: utils.Alloc(uint64(m.EpochIndex)),
		}
	},
	Decode: func(m *pb.View) (View, error) {
		if m.Index == nil {
			return View{}, fmt.Errorf("index: missing")
		}
		if m.Number == nil {
			return View{}, fmt.Errorf("number: missing")
		}
		if m.EpochIndex == nil {
			return View{}, fmt.Errorf("epoch_index: missing")
		}
		return View{
			Index:      RoadIndex(*m.Index),
			Number:     ViewNumber(*m.Number),
			EpochIndex: EpochIndex(*m.EpochIndex),
		}, nil
	},
}

// ProposalConv is the protobuf converter for Proposal.
var ProposalConv = protoutils.Conv[*Proposal, *pb.Proposal]{
	Encode: func(m *Proposal) *pb.Proposal {
		laneRanges := make([]*LaneRange, 0, len(m.laneRanges))
		for _, r := range m.laneRanges {
			laneRanges = append(laneRanges, r)
		}
		sort.Slice(laneRanges, func(i, j int) bool { return laneRanges[i].Lane().Compare(laneRanges[j].Lane()) < 0 })
		return &pb.Proposal{
			View:        ViewConv.Encode(m.view),
			Timestamp:   TimeConv.Encode(m.timestamp),
			LaneRanges:  LaneRangeConv.EncodeSlice(laneRanges),
			App:         AppProposalConv.EncodeOpt(m.app),
			GlobalFirst: utils.Alloc(uint64(m.globalRange.First)),
		}
	},
	Decode: func(m *pb.Proposal) (*Proposal, error) {
		view, err := ViewConv.DecodeReq(m.View)
		if err != nil {
			return nil, fmt.Errorf("view: %w", err)
		}
		laneRanges, err := LaneRangeConv.DecodeSlice(m.LaneRanges)
		if err != nil {
			return nil, fmt.Errorf("laneRanges: %w", err)
		}
		timestamp, err := TimeConv.DecodeReq(m.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("timestamp: %w", err)
		}
		app, err := AppProposalConv.DecodeOpt(m.App)
		if err != nil {
			return nil, fmt.Errorf("appQC: %w", err)
		}
		// Hard-reject messages with absent global_first/epoch_index.
		// Autobahn is pre-production; there is no rolling-upgrade path from
		// messages encoded before these fields were added.
		if m.GlobalFirst == nil {
			return nil, fmt.Errorf("global_first: missing")
		}
		proposal := newProposal(view, timestamp, laneRanges, app, GlobalBlockNumber(*m.GlobalFirst))
		if len(proposal.laneRanges) != len(laneRanges) {
			return nil, fmt.Errorf("laneRanges: duplicate ranges")
		}
		return proposal, nil
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
			AppQc:      AppQCConv.EncodeOpt(m.appQC),
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
		appQC, err := AppQCConv.DecodeOpt(m.AppQc)
		if err != nil {
			return nil, fmt.Errorf("appQC: %w", err)
		}
		timeoutQC, err := TimeoutQCConv.DecodeOpt(m.TimeoutQc)
		if err != nil {
			return nil, fmt.Errorf("timeoutQC: %w", err)
		}
		return &FullProposal{proposal: proposal, laneQCs: laneQCsMap, appQC: appQC, timeoutQC: timeoutQC}, nil
	},
}

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
	if !c.Lanes().Has(m.lane) {
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

// RoadIndex is the index of the consensus instance.
type RoadIndex uint64

// ViewNumber is the view number of the consensus instance.
type ViewNumber uint64

// Next view number.
func (n ViewNumber) Next() ViewNumber { return n + 1 }

// View represents a consensus view.
type View struct {
	Index  RoadIndex
	Number ViewNumber
}

// Less checks if v is earlier than b.
func (v View) Less(b View) bool {
	if v.Index != b.Index {
		return v.Index < b.Index
	}
	return v.Number < b.Number
}

// Next returns the next view.
func (v View) Next() View {
	v.Number = v.Number.Next()
	return v
}

// ViewSpec is a justification to start a given view.
type ViewSpec struct {
	// WARNING: currently we have implicit assumption that
	// TimeoutQC.View().Index == CommitQC.Index.Next(),
	// I.e. that TimeoutQC comes from the expected consensus instance.
	CommitQC  utils.Option[*CommitQC]
	TimeoutQC utils.Option[*TimeoutQC]
}

// View is the view justified by vs.
func (vs *ViewSpec) View() View {
	idx := NextIndexOpt(vs.CommitQC)
	if view := NextViewOpt(vs.TimeoutQC); view.Index == idx {
		return view
	}
	return View{Index: idx, Number: 0}
}

// Proposal is the road tipcut proposal.
// It consists of ranges of blocks of each lane.
// AppQC could be nil if we haven't reached any quorum state hash.
type Proposal struct {
	utils.ReadOnly
	view       View
	createdAt  time.Time
	laneRanges map[LaneID]*LaneRange
	app        utils.Option[*AppProposal]
	// derived
	globalRange GlobalRange
}

func newProposal(view View, createdAt time.Time, laneRanges []*LaneRange, app utils.Option[*AppProposal]) *Proposal {
	laneRangesM := map[LaneID]*LaneRange{}
	globalRange := GlobalRange{}
	for _, r := range laneRanges {
		laneRangesM[r.Lane()] = r
		globalRange.First += GlobalBlockNumber(r.First())
		globalRange.Next += GlobalBlockNumber(r.Next())
	}
	return &Proposal{
		view:        view,
		createdAt:   createdAt,
		laneRanges:  laneRangesM,
		globalRange: globalRange,
		app:         app,
	}
}

// Index of the proposal.
func (m *Proposal) Index() RoadIndex { return m.view.Index }

// View of the proposal.
func (m *Proposal) View() View { return m.view }

// CreatedAt of the proposal.
func (m *Proposal) CreatedAt() time.Time { return m.createdAt }

// App .
func (m *Proposal) App() utils.Option[*AppProposal] { return m.app }

// GlobalRange returns the proposed global block range.
func (m *Proposal) GlobalRange() GlobalRange {
	var g GlobalRange
	for _, r := range m.laneRanges {
		g.First += GlobalBlockNumber(r.First())
		g.Next += GlobalBlockNumber(r.Next())
	}
	return g
}

// Verify checks that every present lane range belongs to the committee
// and is internally valid. Lanes may be omitted — omitted lanes are
// treated as implicit empty ranges by FullProposal.Verify.
func (m *Proposal) Verify(c *Committee) error {
	for _, r := range m.laneRanges {
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
func NewProposal(
	key SecretKey,
	committee *Committee,
	viewSpec ViewSpec,
	createdAt time.Time,
	laneQCs map[LaneID]*LaneQC,
	appQC utils.Option[*AppQC],
) (*FullProposal, error) {
	if got, want := key.Public(), committee.Leader(viewSpec.View()); got != want {
		return nil, fmt.Errorf("key %q is not the leader %q for view %v", got, want, viewSpec.View())
	}
	if p, ok := NewReproposal(key, viewSpec); ok {
		return p, nil
	}
	var laneRanges []*LaneRange
	for _, lane := range committee.Lanes().All() {
		first := LaneRangeOpt(viewSpec.CommitQC, lane).Next()
		if lQC, ok := laneQCs[lane]; ok {
			if lQC.Header().Lane() != lane {
				return nil, fmt.Errorf("laneQC %v for lane %v", lQC.Header().Lane(), lane)
			}
			laneRanges = append(laneRanges, NewLaneRange(lane, first, utils.Some(lQC.Header())))
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
	proposal := newProposal(
		viewSpec.View(),
		time.Now(),
		laneRanges,
		app,
	)
	return &FullProposal{
		proposal:  Sign(key, proposal),
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
func (m *FullProposal) Verify(c *Committee, vs ViewSpec) error {
	return scope.Parallel(func(s scope.ParallelScope) error {
		// Does the view match?
		if got, want := m.proposal.Msg().View(), vs.View(); got != want {
			return fmt.Errorf("view = %v, want %v", m.View(), vs.View())
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
				if err := tQC.Verify(c, vs.CommitQC); err != nil {
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
		// Verify the proposal's lane structure against the committee.
		proposal := m.proposal.Msg()
		if err := proposal.Verify(c); err != nil {
			return fmt.Errorf("proposal: %w", err)
		}
		// Verify each lane range against the previous commitQC and its laneQC justification.
		for _, lane := range c.Lanes().All() {
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
			if got, want := appQC.Proposal().GlobalNumber(), GlobalRangeOpt(vs.CommitQC).Next; got >= want {
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
		lane, err := PublicKeyConv.Decode(m.Lane)
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
			Index:  utils.Alloc(uint64(m.Index)),
			Number: utils.Alloc(uint64(m.Number)),
		}
	},
	Decode: func(m *pb.View) (View, error) {
		if m == nil {
			return View{}, nil
		}
		if m.Index == nil {
			return View{}, fmt.Errorf("index: missing")
		}
		if m.Number == nil {
			return View{}, fmt.Errorf("number: missing")
		}
		return View{
			Index:  RoadIndex(*m.Index),
			Number: ViewNumber(*m.Number),
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
			View:       ViewConv.Encode(m.view),
			CreatedAt:  TimeConv.Encode(m.createdAt),
			LaneRanges: LaneRangeConv.EncodeSlice(laneRanges),
			App:        AppProposalConv.EncodeOpt(m.app),
		}
	},
	Decode: func(m *pb.Proposal) (*Proposal, error) {
		view, err := ViewConv.Decode(m.View)
		if err != nil {
			return nil, fmt.Errorf("view: %w", err)
		}
		laneRanges, err := LaneRangeConv.DecodeSlice(m.LaneRanges)
		if err != nil {
			return nil, fmt.Errorf("laneRanges: %w", err)
		}
		createdAt, err := TimeConv.Decode(m.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("createdAt: %w", err)
		}
		app, err := AppProposalConv.DecodeOpt(m.App)
		if err != nil {
			return nil, fmt.Errorf("appQC: %w", err)
		}
		return newProposal(
			view,
			createdAt,
			laneRanges,
			app,
		), nil
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
			Proposal:  SignedMsgConv[*Proposal]().Encode(m.proposal),
			LaneQcs:   LaneQCConv.EncodeSlice(laneQCs),
			AppQc:     AppQCConv.EncodeOpt(m.appQC),
			TimeoutQc: TimeoutQCConv.EncodeOpt(m.timeoutQC),
		}
	},
	Decode: func(m *pb.FullProposal) (*FullProposal, error) {
		proposal, err := SignedMsgConv[*Proposal]().DecodeReq(m.Proposal)
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

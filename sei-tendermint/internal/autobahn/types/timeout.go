package types

import (
	"errors"
	"fmt"

	"github.com/tendermint/tendermint/internal/autobahn/pb"
	"github.com/tendermint/tendermint/internal/protoutils"
	"github.com/tendermint/tendermint/libs/utils"
)

// TimeoutVote .
type TimeoutVote struct {
	utils.ReadOnly
	view            View
	latestPrepareQC utils.Option[ViewNumber]
}

// NewTimeoutVote creates a new TimeoutVote.
func NewTimeoutVote(view View, latestPrepareQC utils.Option[ViewNumber]) *TimeoutVote {
	return &TimeoutVote{
		view:            view,
		latestPrepareQC: latestPrepareQC,
	}
}

// View .
func (m *TimeoutVote) View() View {
	return m.view
}

// latestPrepareQCView is the highest view number for which a PrepareQC was observed by the node.
func (m *TimeoutVote) latestPrepareQCView() utils.Option[View] {
	return utils.MapOpt(m.latestPrepareQC, func(n ViewNumber) View {
		return View{Index: m.view.Index, Number: n}
	})
}

// FullTimeoutVote .
type FullTimeoutVote struct {
	utils.ReadOnly
	vote            *Signed[*TimeoutVote]
	latestPrepareQC utils.Option[*PrepareQC]
}

// NewFullTimeoutVote creates a new FullTimeoutVote.
func NewFullTimeoutVote(key SecretKey, view View, latestPrepareQC utils.Option[*PrepareQC]) *FullTimeoutVote {
	vote := &TimeoutVote{
		view:            view,
		latestPrepareQC: utils.MapOpt(latestPrepareQC, func(qc *PrepareQC) ViewNumber { return qc.Proposal().View().Number }),
	}
	return &FullTimeoutVote{
		vote:            Sign(key, vote),
		latestPrepareQC: latestPrepareQC,
	}
}

// Vote .
func (m *FullTimeoutVote) Vote() *Signed[*TimeoutVote] {
	return m.vote
}

// View .
func (m *FullTimeoutVote) View() View {
	return m.vote.Msg().View()
}

// Verify verifies the FullTimeoutVote against the committee.
func (m *FullTimeoutVote) Verify(c *Committee) error {
	if err := m.vote.VerifySig(c); err != nil {
		return err
	}
	if want, ok := m.vote.Msg().latestPrepareQCView().Get(); ok {
		pQC, ok := m.latestPrepareQC.Get()
		if !ok {
			return errors.New("missing latestPrepareQC")
		}
		// TODO: verifying PrepareQC in all Timeout votes might be too inefficient.
		// If it is, we can skip duplicated verification.
		if err := pQC.Verify(c); err != nil {
			return fmt.Errorf("latestPrepareQC: %w", err)
		}
		if got := pQC.Proposal().View(); got != want {
			return fmt.Errorf("latestPrepareQC view mismatch, got %v, want %v", got, want)
		}
	} else {
		if _, ok := m.latestPrepareQC.Get(); ok {
			return errors.New("unnecessary latestPrepareQC")
		}
	}
	return nil
}

// TimeoutQC .
type TimeoutQC struct {
	utils.ReadOnly
	votes           []*Signed[*TimeoutVote]
	latestPrepareQC utils.Option[*PrepareQC]
}

// NewTimeoutQC creates a new TimeoutQC.
func NewTimeoutQC(fullVotes []*FullTimeoutVote) *TimeoutQC {
	latestPrepareQC := utils.None[*PrepareQC]()
	var votes []*Signed[*TimeoutVote]
	for _, v := range fullVotes {
		votes = append(votes, v.Vote())
		if qc := v.latestPrepareQC; NextViewOpt(latestPrepareQC).Less(NextViewOpt(qc)) {
			latestPrepareQC = qc
		}
	}
	return &TimeoutQC{
		votes:           votes,
		latestPrepareQC: latestPrepareQC,
	}
}

// View .
func (m *TimeoutQC) View() View {
	return m.Votes()[0].Msg().View()
}

// Votes .
func (m *TimeoutQC) Votes() []*Signed[*TimeoutVote] {
	return m.votes
}

// LatestPrepareQC returns the highest PrepareQC observed by signers.
func (m *TimeoutQC) LatestPrepareQC() utils.Option[*PrepareQC] {
	return m.latestPrepareQC
}

// Verify verifies the TimeoutQC against the committee and the previous CommitQC.
// Verifying TimeoutQC should NOT require previous TimeoutQC,
// since observing prior TimeoutQCs is not required in the pb.
func (m *TimeoutQC) Verify(c *Committee, prev utils.Option[*CommitQC]) error {
	// Verify the signatures.
	done := map[PublicKey]struct{}{}
	for _, v := range m.votes {
		if _, ok := done[v.sig.key]; ok {
			return fmt.Errorf("duplicate vote from %q", v.sig.key)
		}
		done[v.sig.key] = struct{}{}
		if err := v.VerifySig(c); err != nil {
			return err
		}
	}
	// Verify that we have enough votes.
	if got, want := len(done), c.TimeoutQuorum(); got < want {
		return fmt.Errorf("got %v votes, want >= %v", got, want)
	}
	// Check that the TimeoutQC is from the correct consensus instance.
	h := utils.None[ViewNumber]()
	view := m.View()
	if got, want := view.Index, NextIndexOpt(prev); got != want {
		return fmt.Errorf("timeoutQC.View().Index = %v, want %v", got, want)
	}
	// Check that the votes come from the same view.
	for _, v := range m.Votes() {
		if got := v.Msg().View(); got != view {
			return fmt.Errorf("votes[%q].View() = %v, want %v", v.sig.key, got, view)
		}
		if x, ok := v.Msg().latestPrepareQC.Get(); ok && x >= NextOpt(h) {
			h = utils.Some(x)
		}
	}
	// Check that the prepareQC is present iff needed.
	if vn, ok := h.Get(); ok {
		pQC, ok := m.latestPrepareQC.Get()
		if !ok {
			return errors.New("missing latestPrepareQC")
		}
		if got, want := pQC.Proposal().View(), (View{Index: view.Index, Number: vn}); got != want {
			return fmt.Errorf("latestPrepareQC view number mismatch, got %v, want %v", got, want)
		}
		if err := pQC.Verify(c); err != nil {
			return fmt.Errorf("higPrepareQC: %w", err)
		}
	} else {
		if _, ok := m.latestPrepareQC.Get(); ok {
			return errors.New("unnecessary latestPrepareQC")
		}
	}
	return nil
}

func (m *TimeoutQC) reproposal() (*Proposal, bool) {
	pQC, ok := m.latestPrepareQC.Get()
	if !ok {
		return nil, false
	}
	p := pQC.Proposal()
	// TODO(gprusak): this unnecessarily accesses internal state and does the copy. Fix it.
	var laneRanges []*LaneRange
	for _, l := range p.laneRanges {
		laneRanges = append(laneRanges, l)
	}
	return newProposal(
		m.View().Next(),
		p.CreatedAt(),
		laneRanges,
		p.App(),
	), true
}

// TimeoutVoteConv is the protobuf converter for TimeoutVote.
var TimeoutVoteConv = protoutils.Conv[*TimeoutVote, *pb.TimeoutVote]{
	Encode: func(m *TimeoutVote) *pb.TimeoutVote {
		return &pb.TimeoutVote{
			View: ViewConv.Encode(m.view),
			LatestPrepareQcViewNumber: func() *uint64 {
				if v, ok := m.latestPrepareQC.Get(); ok {
					return utils.Alloc(uint64(v))
				}
				return nil
			}(),
		}
	},
	Decode: func(m *pb.TimeoutVote) (*TimeoutVote, error) {
		view, err := ViewConv.DecodeReq(m.View)
		if err != nil {
			return nil, fmt.Errorf("view: %w", err)
		}
		return &TimeoutVote{
			view: view,
			latestPrepareQC: func() utils.Option[ViewNumber] {
				if v := m.LatestPrepareQcViewNumber; v != nil {
					return utils.Some(ViewNumber(*v))
				}
				return utils.None[ViewNumber]()
			}(),
		}, nil
	},
}

// FullTimeoutVoteConv is the protobuf converter for FullTimeoutVote.
var FullTimeoutVoteConv = protoutils.Conv[*FullTimeoutVote, *pb.FullTimeoutVote]{
	Encode: func(m *FullTimeoutVote) *pb.FullTimeoutVote {
		return &pb.FullTimeoutVote{
			Vote:            SignedMsgConv[*TimeoutVote]().Encode(m.vote),
			LatestPrepareQc: PrepareQCConv.EncodeOpt(m.latestPrepareQC),
		}
	},
	Decode: func(m *pb.FullTimeoutVote) (*FullTimeoutVote, error) {
		vote, err := SignedMsgConv[*TimeoutVote]().Decode(m.Vote)
		if err != nil {
			return nil, fmt.Errorf("timeoutVote: %w", err)
		}
		latestPrepareQC, err := PrepareQCConv.DecodeOpt(m.LatestPrepareQc)
		if err != nil {
			return nil, fmt.Errorf("latestPrepareQc: %w", err)
		}
		return &FullTimeoutVote{
			vote:            vote,
			latestPrepareQC: latestPrepareQC,
		}, nil
	},
}

// TimeoutQCConv is the protobuf converter for TimeoutQC.
var TimeoutQCConv = protoutils.Conv[*TimeoutQC, *pb.TimeoutQC]{
	Encode: func(m *TimeoutQC) *pb.TimeoutQC {
		return &pb.TimeoutQC{
			Votes:           SignedMsgConv[*TimeoutVote]().EncodeSlice(m.votes),
			LatestPrepareQc: PrepareQCConv.EncodeOpt(m.latestPrepareQC),
		}
	},
	Decode: func(m *pb.TimeoutQC) (*TimeoutQC, error) {
		votes, err := SignedMsgConv[*TimeoutVote]().DecodeSlice(m.Votes)
		if err != nil {
			return nil, fmt.Errorf("votes: %w", err)
		}
		latestPrepareQC, err := PrepareQCConv.DecodeOpt(m.LatestPrepareQc)
		if err != nil {
			return nil, fmt.Errorf("latestPrepareQc: %w", err)
		}
		return &TimeoutQC{
			votes:           votes,
			latestPrepareQC: latestPrepareQC,
		}, nil
	},
}

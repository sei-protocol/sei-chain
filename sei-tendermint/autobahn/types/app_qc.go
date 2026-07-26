package types

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// AppQC .
type AppQC struct {
	utils.ReadOnly
	vote *Hashed[*AppVote]
	sigs []*Signature
}

// NewAppQC create a new stateQC.
func NewAppQC(votes []*Signed[*AppVote]) *AppQC {
	if len(votes) == 0 {
		panic("qc cannot be empty")
	}
	sigs := make([]*Signature, len(votes))
	for i, v := range votes {
		sigs[i] = v.sig
	}
	return &AppQC{vote: votes[0].hashed, sigs: sigs}
}

// Proposal .
func (m *AppQC) Proposal() *AppProposal { return m.vote.Msg().Proposal() }

// Next is the number of the next global block to finalize AppHash for.
func (m *AppQC) Next() RoadIndex {
	return m.Proposal().Next()
}

// Verify checks epoch_index / road against ep, then quorum under ep's committee.
func (m *AppQC) Verify(ep *Epoch) error {
	p := m.Proposal()
	if got, want := p.EpochIndex(), ep.EpochIndex(); got != want {
		return fmt.Errorf("appQC epoch %d, want %d", got, want)
	}
	if rr := ep.RoadRange(); !rr.Has(p.RoadIndex()) {
		return fmt.Errorf("app road_index %v not in epoch %d roads [%v,%v)",
			p.RoadIndex(), ep.EpochIndex(), rr.First, rr.Next)
	}
	c := ep.Committee()
	return m.vote.verifyQC(c, c.AppQuorum(), m.sigs)
}

// AppQCConv is a protobuf converter for AppQC.
var AppQCConv = protoutils.Conv[*AppQC, *pb.AppQC]{
	Encode: func(m *AppQC) *pb.AppQC {
		return &pb.AppQC{
			Vote: AppVoteConv.Encode(m.vote.Msg()),
			Sigs: SignatureConv.EncodeSlice(m.sigs),
		}
	},
	Decode: func(m *pb.AppQC) (*AppQC, error) {
		vote, err := AppVoteConv.DecodeReq(m.Vote)
		if err != nil {
			return nil, fmt.Errorf("proposal: %w", err)
		}
		sigs, err := SignatureConv.DecodeSlice(m.Sigs)
		if err != nil {
			return nil, fmt.Errorf("sigs: %w", err)
		}
		return &AppQC{vote: NewHashed(vote), sigs: sigs}, nil
	},
}

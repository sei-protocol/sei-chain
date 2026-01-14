package types

import (
	"fmt"
	
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/internal/protoutils"
	"github.com/tendermint/tendermint/internal/autobahn/pb"
)

// PrepareQC .
type PrepareQC struct {
	utils.ReadOnly
	vote *Hashed[*PrepareVote]
	sigs []*Signature
}

// NewPrepareQC creates a new PrepareQC.
// PANICS if votes is empty.
func NewPrepareQC(votes []*Signed[*PrepareVote]) *PrepareQC {
	if len(votes) == 0 {
		panic("qc cannot be empty")
	}
	sigs := make([]*Signature, len(votes))
	for i, v := range votes {
		sigs[i] = v.sig
	}
	return &PrepareQC{vote: votes[0].hashed, sigs: sigs}
}

// Proposal .
func (m *PrepareQC) Proposal() *Proposal { return m.vote.Msg().Proposal() }

// View .
func (m *PrepareQC) View() View {
	return m.vote.Msg().Proposal().View()
}

// Verify verifies the PrepareQC against the committee.
// Currently it doesn't require the previous CommitQC.
func (m *PrepareQC) Verify(c *Committee) error {
	return m.vote.verifyQC(c, c.PrepareQuorum(), m.sigs)
}

// PrepareQCConv is a protobuf converter for PrepareQC.
var PrepareQCConv = protoutils.Conv[*PrepareQC, *pb.PrepareQC]{
	Encode: func(m *PrepareQC) *pb.PrepareQC {
		return &pb.PrepareQC{
			Vote: PrepareVoteConv.Encode(m.vote.Msg()),
			Sigs: SignatureConv.EncodeSlice(m.sigs),
		}
	},
	Decode: func(m *pb.PrepareQC) (*PrepareQC, error) {
		vote, err := PrepareVoteConv.DecodeReq(m.Vote)
		if err != nil {
			return nil, fmt.Errorf("vote: %w", err)
		}
		sigs, err := SignatureConv.DecodeSlice(m.Sigs)
		if err != nil {
			return nil, fmt.Errorf("sigs: %w", err)
		}
		return &PrepareQC{vote: NewHashed(vote), sigs: sigs}, nil
	},
}

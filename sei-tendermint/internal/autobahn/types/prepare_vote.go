package types

import (
	"fmt"
	
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/internal/protoutils"
	"github.com/tendermint/tendermint/internal/autobahn/pb"
)

// PrepareVote .
type PrepareVote struct {
	utils.ReadOnly
	proposal *Proposal
}

// NewPrepareVote creates a new PrepareVote.
func NewPrepareVote(proposal *Proposal) *PrepareVote {
	return &PrepareVote{proposal: proposal}
}

// Proposal .
func (m *PrepareVote) Proposal() *Proposal { return m.proposal }

// PrepareVoteConv is the protobuf converter for PrepareVote.
var PrepareVoteConv = protoutils.Conv[*PrepareVote, *pb.Proposal]{
	Encode: func(m *PrepareVote) *pb.Proposal {
		return ProposalConv.Encode(m.proposal)
	},
	Decode: func(m *pb.Proposal) (*PrepareVote, error) {
		proposal, err := ProposalConv.DecodeReq(m)
		if err != nil {
			return nil, fmt.Errorf("proposal: %w", err)
		}
		return &PrepareVote{proposal: proposal}, nil
	},
}

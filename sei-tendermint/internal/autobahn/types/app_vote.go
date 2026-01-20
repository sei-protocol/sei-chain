package types

import (
	"fmt"
	"github.com/tendermint/tendermint/internal/autobahn/pb"
	"github.com/tendermint/tendermint/internal/protoutils"
	"github.com/tendermint/tendermint/libs/utils"
)

// AppVote .
type AppVote struct {
	utils.ReadOnly
	proposal *AppProposal
}

// NewAppVote creates a new AppVote.
func NewAppVote(proposal *AppProposal) *AppVote {
	return &AppVote{proposal: proposal}
}

// Proposal returns the state proposal.
func (m *AppVote) Proposal() *AppProposal { return m.proposal }

// AppVoteConv is the protobuf converter for AppVote.
var AppVoteConv = protoutils.Conv[*AppVote, *pb.AppProposal]{
	Encode: func(m *AppVote) *pb.AppProposal {
		return AppProposalConv.Encode(m.proposal)
	},
	Decode: func(m *pb.AppProposal) (*AppVote, error) {
		proposal, err := AppProposalConv.DecodeReq(m)
		if err != nil {
			return nil, fmt.Errorf("proposal: %w", err)
		}
		return &AppVote{proposal: proposal}, nil
	},
}

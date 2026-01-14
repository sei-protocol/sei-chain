package types

import (
	"fmt"

	"github.com/sei-protocol/sei-stream/pkg/utils"
	"github.com/tendermint/tendermint/internal/autobahn/pkg/protocol"
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
var AppVoteConv = utils.ProtoConv[*AppVote, *protocol.AppProposal]{
	Encode: func(m *AppVote) *protocol.AppProposal {
		return AppProposalConv.Encode(m.proposal)
	},
	Decode: func(m *protocol.AppProposal) (*AppVote, error) {
		proposal, err := AppProposalConv.DecodeReq(m)
		if err != nil {
			return nil, fmt.Errorf("proposal: %w", err)
		}
		return &AppVote{proposal: proposal}, nil
	},
}

package types

import (
	"fmt"

	"github.com/sei-protocol/sei-stream/pkg/utils"
	"github.com/tendermint/tendermint/internal/autobahn/pkg/protocol"
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
var PrepareVoteConv = utils.ProtoConv[*PrepareVote, *protocol.Proposal]{
	Encode: func(m *PrepareVote) *protocol.Proposal {
		return ProposalConv.Encode(m.proposal)
	},
	Decode: func(m *protocol.Proposal) (*PrepareVote, error) {
		proposal, err := ProposalConv.DecodeReq(m)
		if err != nil {
			return nil, fmt.Errorf("proposal: %w", err)
		}
		return &PrepareVote{proposal: proposal}, nil
	},
}

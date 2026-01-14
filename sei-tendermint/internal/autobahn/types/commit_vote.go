package types

import (
	"fmt"

	"github.com/sei-protocol/sei-stream/pkg/utils"
	"github.com/tendermint/tendermint/internal/autobahn/pkg/protocol"
)

// CommitVote .
type CommitVote struct {
	utils.ReadOnly
	proposal *Proposal
}

// NewCommitVote creates a new CommitVote.
func NewCommitVote(proposal *Proposal) *CommitVote {
	return &CommitVote{proposal: proposal}
}

// Proposal .
func (m *CommitVote) Proposal() *Proposal { return m.proposal }

// CommitVoteConv is the protobuf converter for CommitVote.
var CommitVoteConv = utils.ProtoConv[*CommitVote, *protocol.Proposal]{
	Encode: func(m *CommitVote) *protocol.Proposal {
		return ProposalConv.Encode(m.proposal)
	},
	Decode: func(m *protocol.Proposal) (*CommitVote, error) {
		proposal, err := ProposalConv.DecodeReq(m)
		if err != nil {
			return nil, fmt.Errorf("proposal: %w", err)
		}
		return &CommitVote{proposal: proposal}, nil
	},
}

package types

import (
	"fmt"

	"github.com/tendermint/tendermint/internal/autobahn/pb"
	"github.com/tendermint/tendermint/internal/protoutils"
	"github.com/tendermint/tendermint/libs/utils"
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
var CommitVoteConv = protoutils.Conv[*CommitVote, *pb.Proposal]{
	Encode: func(m *CommitVote) *pb.Proposal {
		return ProposalConv.Encode(m.proposal)
	},
	Decode: func(m *pb.Proposal) (*CommitVote, error) {
		proposal, err := ProposalConv.DecodeReq(m)
		if err != nil {
			return nil, fmt.Errorf("proposal: %w", err)
		}
		return &CommitVote{proposal: proposal}, nil
	},
}

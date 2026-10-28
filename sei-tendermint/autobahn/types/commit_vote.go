package types

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// CommitVote is a vote for a road block header at a consensus view.
type CommitVote struct {
	utils.ReadOnly
	viewNumber ViewNumber
	header     *RoadBlockHeader
}

// NewCommitVote creates a commit vote for qc.
func NewCommitVote(qc *PrepareQC) *CommitVote {
	return &CommitVote{viewNumber: qc.View().Number, header: qc.Header()}
}

// View returns the vote's consensus view.
func (m *CommitVote) View() View { return m.header.view(m.viewNumber) }

// Header returns the voted road block header.
func (m *CommitVote) Header() *RoadBlockHeader { return m.header }

// Verify verifies the vote against the epoch.
func (m *CommitVote) Verify(ep *Epoch) error {
	return m.header.Verify(ep)
}

// CommitVoteConv is the protobuf converter for CommitVote.
var CommitVoteConv = protoutils.Conv[*CommitVote, *pb.CommitVote]{
	Encode: func(m *CommitVote) *pb.CommitVote {
		return &pb.CommitVote{ViewNumber: new(uint64(m.viewNumber)), Header: RoadBlockHeaderConv.Encode(m.header)}
	},
	Decode: func(m *pb.CommitVote) (*CommitVote, error) {
		if m.ViewNumber == nil {
			return nil, fmt.Errorf("view_number: missing")
		}
		header, err := RoadBlockHeaderConv.DecodeReq(m.Header)
		if err != nil {
			return nil, fmt.Errorf("header: %w", err)
		}
		return &CommitVote{viewNumber: ViewNumber(*m.ViewNumber), header: header}, nil
	},
}

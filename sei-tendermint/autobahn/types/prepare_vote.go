package types

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// PrepareVote is a vote for a road block header at a consensus view.
type PrepareVote struct {
	utils.ReadOnly
	viewNumber ViewNumber
	header     *RoadBlockHeader
}

// NewPrepareVote creates a new PrepareVote.
func NewPrepareVote(viewNumber ViewNumber, header *RoadBlockHeader) *PrepareVote {
	return &PrepareVote{viewNumber: viewNumber, header: header}
}

// View returns the vote's consensus view.
func (m *PrepareVote) View() View { return m.header.view(m.viewNumber) }

// Header returns the voted road block header.
func (m *PrepareVote) Header() *RoadBlockHeader { return m.header }

// Verify verifies the vote against the epoch.
func (m *PrepareVote) Verify(ep *Epoch) error {
	return m.header.Verify(ep)
}

// PrepareVoteConv is the protobuf converter for PrepareVote.
var PrepareVoteConv = protoutils.Conv[*PrepareVote, *pb.PrepareVote]{
	Encode: func(m *PrepareVote) *pb.PrepareVote {
		return &pb.PrepareVote{ViewNumber: utils.Alloc(uint64(m.viewNumber)), Header: RoadBlockHeaderConv.Encode(m.header)}
	},
	Decode: func(m *pb.PrepareVote) (*PrepareVote, error) {
		if m.ViewNumber == nil {
			return nil, fmt.Errorf("view_number: missing")
		}
		header, err := RoadBlockHeaderConv.DecodeReq(m.Header)
		if err != nil {
			return nil, fmt.Errorf("header: %w", err)
		}
		return &PrepareVote{viewNumber: ViewNumber(*m.ViewNumber), header: header}, nil
	},
}

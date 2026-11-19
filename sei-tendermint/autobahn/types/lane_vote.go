package types

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// LaneVote .
type LaneVote struct {
	utils.ReadOnly
	header *LaneBlockHeader
}

// NewLaneVote creates a new LaneVote.
func NewLaneVote(header *LaneBlockHeader) *LaneVote {
	return &LaneVote{header: header}
}

// Header .
func (m *LaneVote) Header() *LaneBlockHeader { return m.header }

// Verify verifies that the LaneVote is consistent with the Committee.
func (m *LaneVote) Verify(c *Committee) error {
	return m.header.Verify(c)
}

// LaneVoteConv is the protobuf converter for LaneVote.
var LaneVoteConv = protoutils.Conv[*LaneVote, *pb.LaneBlockHeader]{
	Encode: func(m *LaneVote) *pb.LaneBlockHeader {
		return LaneBlockHeaderConv.Encode(m.header)
	},
	Decode: func(m *pb.LaneBlockHeader) (*LaneVote, error) {
		header, err := LaneBlockHeaderConv.DecodeReq(m)
		if err != nil {
			return nil, fmt.Errorf("header: %w", err)
		}
		return &LaneVote{header: header}, nil
	},
}

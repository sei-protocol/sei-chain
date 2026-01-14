package types

import (
	"fmt"
	
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/internal/protoutils"
	"github.com/tendermint/tendermint/internal/autobahn/pb"
)

// LaneVote .
type LaneVote struct {
	utils.ReadOnly
	header *BlockHeader
}

// NewLaneVote creates a new LaneVote.
func NewLaneVote(header *BlockHeader) *LaneVote {
	return &LaneVote{header: header}
}

// Header .
func (m *LaneVote) Header() *BlockHeader { return m.header }

// Verify verifies that the LaneVote is consistent with the Committee.
func (m *LaneVote) Verify(c *Committee) error {
	return m.header.Verify(c)
}

// LaneVoteConv is the protobuf converter for LaneVote.
var LaneVoteConv = protoutils.Conv[*LaneVote, *pb.BlockHeader]{
	Encode: func(m *LaneVote) *pb.BlockHeader {
		return BlockHeaderConv.Encode(m.header)
	},
	Decode: func(m *pb.BlockHeader) (*LaneVote, error) {
		header, err := BlockHeaderConv.DecodeReq(m)
		if err != nil {
			return nil, fmt.Errorf("header: %w", err)
		}
		return &LaneVote{header: header}, nil
	},
}

package types

import (
	"fmt"
	
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/internal/protoutils"
	"github.com/tendermint/tendermint/internal/autobahn/pb"
)

// LaneProposal .
type LaneProposal struct {
	utils.ReadOnly
	block *Block
}

// NewLaneProposal constructs a new LaneProposal.
func NewLaneProposal(block *Block) *LaneProposal {
	return &LaneProposal{block: block}
}

// Block .
func (m *LaneProposal) Block() *Block { return m.block }

// Verify verifies that the LaneProposal is consistent with the Committee.
func (m *LaneProposal) Verify(c *Committee) error {
	return m.block.Verify(c)
}

// LaneProposalConv is a protobuf converter for LaneProposal.
var LaneProposalConv = protoutils.Conv[*LaneProposal, *pb.Block]{
	Encode: func(m *LaneProposal) *pb.Block {
		return BlockConv.Encode(m.block)
	},
	Decode: func(m *pb.Block) (*LaneProposal, error) {
		block, err := BlockConv.Decode(m)
		if err != nil {
			return nil, fmt.Errorf("block: %w", err)
		}
		return &LaneProposal{block: block}, nil
	},
}

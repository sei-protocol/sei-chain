package types

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// LaneProposal .
type LaneProposal struct {
	utils.ReadOnly
	block *LaneBlock
}

// NewLaneProposal constructs a new LaneProposal.
func NewLaneProposal(block *LaneBlock) *LaneProposal {
	return &LaneProposal{block: block}
}

// LaneBlock .
func (m *LaneProposal) Block() *LaneBlock { return m.block }

// Verify checks the proposal's internal integrity.
func (m *LaneProposal) Verify() error {
	return m.block.Verify()
}

// LaneProposalConv is a protobuf converter for LaneProposal.
var LaneProposalConv = protoutils.Conv[*LaneProposal, *pb.LaneBlock]{
	Encode: func(m *LaneProposal) *pb.LaneBlock {
		return LaneBlockConv.Encode(m.block)
	},
	Decode: func(m *pb.LaneBlock) (*LaneProposal, error) {
		block, err := LaneBlockConv.Decode(m)
		if err != nil {
			return nil, fmt.Errorf("block: %w", err)
		}
		return &LaneProposal{block: block}, nil
	},
}

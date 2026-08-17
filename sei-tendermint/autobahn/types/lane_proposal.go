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
	block *Block
}

// NewLaneProposal constructs a new LaneProposal.
func NewLaneProposal(block *Block) *LaneProposal {
	return &LaneProposal{block: block}
}

// Block .
func (m *LaneProposal) Block() *Block { return m.block }

// VerifyPayload checks that the payload hashes to the header payload hash.
func (m *LaneProposal) VerifyPayload() error {
	b := m.block
	if got, want := b.payload.Hash(), b.header.payloadHash; got != want {
		return fmt.Errorf("payload.Hash() = %v, want %v", got, want)
	}
	return nil
}

// VerifyCommitteeMembership checks that the proposal's lane is in the committee.
func (m *LaneProposal) VerifyCommitteeMembership(c *Committee) error {
	return m.block.header.Verify(c)
}

// VerifyLaneProposalPayloadAndSignature verifies payload hash and signature. It
// does not check committee membership.
func VerifyLaneProposalPayloadAndSignature(p *Signed[*LaneProposal]) error {
	if err := p.Msg().VerifyPayload(); err != nil {
		return fmt.Errorf("VerifyPayload(): %w", err)
	}
	if err := p.VerifySignature(); err != nil {
		return fmt.Errorf("VerifySignature(): %w", err)
	}
	return nil
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

package types

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// LaneVote is a vote for a lane block header within an epoch.
type LaneVote struct {
	utils.ReadOnly
	epochIndex EpochIndex
	header     *LaneBlockHeader
}

// NewLaneVote creates a new LaneVote.
func NewLaneVote(epochIndex EpochIndex, header *LaneBlockHeader) *LaneVote {
	return &LaneVote{epochIndex: epochIndex, header: header}
}

// EpochIndex returns the epoch this vote belongs to.
func (m *LaneVote) EpochIndex() EpochIndex { return m.epochIndex }

// Header returns the voted lane block header.
func (m *LaneVote) Header() *LaneBlockHeader { return m.header }

// Verify verifies the vote against the epoch.
func (m *LaneVote) Verify(epoch *Epoch) error {
	if m.epochIndex != epoch.EpochIndex() {
		return fmt.Errorf("epoch_index: got %v, want %v", m.epochIndex, epoch.EpochIndex())
	}
	return m.header.Verify(epoch.Committee())
}

// LaneVoteConv is the protobuf converter for LaneVote.
var LaneVoteConv = protoutils.Conv[*LaneVote, *pb.LaneVote]{
	Encode: func(m *LaneVote) *pb.LaneVote {
		return &pb.LaneVote{EpochIndex: new(uint64(m.epochIndex)), Header: LaneBlockHeaderConv.Encode(m.header)}
	},
	Decode: func(m *pb.LaneVote) (*LaneVote, error) {
		if m.EpochIndex == nil {
			return nil, fmt.Errorf("epoch_index: missing")
		}
		header, err := LaneBlockHeaderConv.DecodeReq(m.Header)
		if err != nil {
			return nil, fmt.Errorf("header: %w", err)
		}
		return NewLaneVote(EpochIndex(*m.EpochIndex), header), nil
	},
}

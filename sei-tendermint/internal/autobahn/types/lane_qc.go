package types

import (
	"fmt"
	
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/internal/protoutils"
	"github.com/tendermint/tendermint/internal/autobahn/pb"
)

// LaneQC .
type LaneQC struct {
	utils.ReadOnly
	vote *Hashed[*LaneVote]
	sigs []*Signature
}

// NewLaneQC constructs a new LaneQC.
func NewLaneQC(votes []*Signed[*LaneVote]) *LaneQC {
	if len(votes) == 0 {
		panic("qc cannot be empty")
	}
	sigs := make([]*Signature, len(votes))
	for i, v := range votes {
		sigs[i] = v.sig
	}
	return &LaneQC{vote: votes[0].hashed, sigs: sigs}
}

// Header .
func (m *LaneQC) Header() *BlockHeader { return m.vote.Msg().header }

// Next is the number of the first block not known to be available.
func (m *LaneQC) Next() BlockNumber { return m.Header().Next() }

// Verify verifies LaneQC against the committee.
func (m *LaneQC) Verify(c *Committee) error {
	return m.vote.verifyQC(c, c.LaneQuorum(), m.sigs)
}

// LaneQCConv is a protobuf converter for LaneQC.
var LaneQCConv = protoutils.Conv[*LaneQC, *pb.LaneQC]{
	Encode: func(m *LaneQC) *pb.LaneQC {
		return &pb.LaneQC{
			Vote: LaneVoteConv.Encode(m.vote.Msg()),
			Sigs: SignatureConv.EncodeSlice(m.sigs),
		}
	},
	Decode: func(m *pb.LaneQC) (*LaneQC, error) {
		vote, err := LaneVoteConv.DecodeReq(m.Vote)
		if err != nil {
			return nil, fmt.Errorf("vote: %w", err)
		}
		sigs, err := SignatureConv.DecodeSlice(m.Sigs)
		if err != nil {
			return nil, fmt.Errorf("sigs: %w", err)
		}
		return &LaneQC{vote: NewHashed(vote), sigs: sigs}, nil
	},
}

package types

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
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
func (m *LaneQC) Header() *LaneBlockHeader { return m.vote.Msg().header }

// EpochIndex returns the epoch this certificate belongs to.
func (m *LaneQC) EpochIndex() EpochIndex { return m.vote.Msg().EpochIndex() }

// Next is the number of the first block not known to be available.
func (m *LaneQC) Next() LaneHeight { return m.Header().Next() }

// Verify verifies LaneQC against the epoch.
func (m *LaneQC) Verify(epoch *Epoch) error {
	if err := m.vote.Msg().Verify(epoch); err != nil {
		return err
	}
	c := epoch.Committee()
	return m.vote.verifyQC(c, c.LaneQuorum(), m.sigs)
}

// LaneQCConv is a protobuf converter for LaneQC.
var LaneQCConv = protoutils.Conv[*LaneQC, *pb.LaneQC]{
	Encode: func(m *LaneQC) *pb.LaneQC {
		return &pb.LaneQC{
			VoteV3: LaneVoteConv.Encode(m.vote.Msg()),
			Sigs:   SignatureConv.EncodeSlice(m.sigs),
		}
	},
	Decode: func(m *pb.LaneQC) (*LaneQC, error) {
		vote, err := LaneVoteConv.DecodeReq(m.VoteV3)
		if err != nil {
			return nil, fmt.Errorf("vote_v3: %w", err)
		}
		sigs, err := SignatureConv.DecodeSlice(m.Sigs)
		if err != nil {
			return nil, fmt.Errorf("sigs: %w", err)
		}
		return &LaneQC{vote: NewHashed(vote), sigs: sigs}, nil
	},
}

package types

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// PrepareQC .
type PrepareQC struct {
	utils.ReadOnly
	vote *Hashed[*PrepareVote]
	sigs []*Signature
}

// NewPrepareQC creates a new PrepareQC.
// PANICS if votes is empty.
func NewPrepareQC(votes []*Signed[*PrepareVote]) *PrepareQC {
	if len(votes) == 0 {
		panic("qc cannot be empty")
	}
	sigs := make([]*Signature, len(votes))
	for i, v := range votes {
		sigs[i] = v.sig
	}
	return &PrepareQC{vote: votes[0].hashed, sigs: sigs}
}

// Header returns the prepared road block header.
func (m *PrepareQC) Header() *RoadBlockHeader { return m.vote.Msg().Header() }

// View .
func (m *PrepareQC) View() View {
	return m.vote.Msg().View()
}

// Verify verifies the PrepareQC against the epoch.
func (m *PrepareQC) Verify(ep *Epoch) error {
	if err := m.vote.Msg().Verify(ep); err != nil {
		return err
	}
	c := ep.Committee()
	return m.vote.verifyQC(c, c.PrepareQuorum(), m.sigs)
}

// PrepareQCConv is a protobuf converter for PrepareQC.
var PrepareQCConv = protoutils.Conv[*PrepareQC, *pb.PrepareQC]{
	Encode: func(m *PrepareQC) *pb.PrepareQC {
		return &pb.PrepareQC{
			VoteV2: PrepareVoteConv.Encode(m.vote.Msg()),
			Sigs:   SignatureConv.EncodeSlice(m.sigs),
		}
	},
	Decode: func(m *pb.PrepareQC) (*PrepareQC, error) {
		vote, err := PrepareVoteConv.DecodeReq(m.VoteV2)
		if err != nil {
			return nil, fmt.Errorf("vote: %w", err)
		}
		sigs, err := SignatureConv.DecodeSlice(m.Sigs)
		if err != nil {
			return nil, fmt.Errorf("sigs: %w", err)
		}
		return &PrepareQC{vote: NewHashed(vote), sigs: sigs}, nil
	},
}

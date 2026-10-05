package types

import (
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
)

// ConsensusMsg is the interface for all consensus messages.
type ConsensusMsg interface {
	isConsensusMsg()
	View() View
}

// ConsensusMsgPrepareVote is a PrepareVote variant of ConsensusMsg.
type ConsensusMsgPrepareVote struct{ *Signed[*PrepareVote] }

// ConsensusMsgCommitVote is a CommitVote variant of ConsensusMsg.
type ConsensusMsgCommitVote struct{ *Signed[*CommitVote] }

// View implements ConsensusMsg.
func (m *ConsensusMsgPrepareVote) View() View { return m.Msg().Proposal().View() }

// View implements ConsensusMsg.
func (m *ConsensusMsgCommitVote) View() View { return m.Msg().Proposal().View() }

func (m *FullProposal) isConsensusMsg()            {}
func (m *ConsensusMsgPrepareVote) isConsensusMsg() {}
func (m *ConsensusMsgCommitVote) isConsensusMsg()  {}
func (m *FullTimeoutVote) isConsensusMsg()         {}
func (m *TimeoutQC) isConsensusMsg()               {}

// ConsensusMsgConv is the protobuf converter for ConsensusMsg.
var ConsensusMsgConv = protoutils.Conv[ConsensusMsg, *pb.ConsensusMsg]{
	Encode: func(m ConsensusMsg) *pb.ConsensusMsg {
		switch m := m.(type) {
		case *FullProposal:
			return &pb.ConsensusMsg{
				T: &pb.ConsensusMsg_Proposal{Proposal: FullProposalConv.Encode(m)},
			}
		case *ConsensusMsgPrepareVote:
			return &pb.ConsensusMsg{
				T: &pb.ConsensusMsg_PrepareVoteV2{PrepareVoteV2: SignedPrepareVoteConv.Encode(m.Signed)},
			}
		case *ConsensusMsgCommitVote:
			return &pb.ConsensusMsg{
				T: &pb.ConsensusMsg_CommitVoteV2{CommitVoteV2: SignedCommitVoteConv.Encode(m.Signed)},
			}
		case *FullTimeoutVote:
			return &pb.ConsensusMsg{
				T: &pb.ConsensusMsg_TimeoutVote{TimeoutVote: FullTimeoutVoteConv.Encode(m)},
			}
		case *TimeoutQC:
			return &pb.ConsensusMsg{
				T: &pb.ConsensusMsg_TimeoutQc{TimeoutQc: TimeoutQCConv.Encode(m)},
			}
		default:
			panic(fmt.Sprintf("Unknown ConsensusMsg type: %T", m))
		}
	},
	Decode: func(m *pb.ConsensusMsg) (ConsensusMsg, error) {
		if m.T == nil {
			return nil, errors.New("empty")
		}
		switch t := m.T.(type) {
		case *pb.ConsensusMsg_Proposal:
			return FullProposalConv.DecodeReq(t.Proposal)
		case *pb.ConsensusMsg_PrepareVoteV2:
			vote, err := SignedPrepareVoteConv.DecodeReq(t.PrepareVoteV2)
			if err != nil {
				return nil, fmt.Errorf("prepareVote: %w", err)
			}
			return &ConsensusMsgPrepareVote{vote}, nil
		case *pb.ConsensusMsg_CommitVoteV2:
			vote, err := SignedCommitVoteConv.DecodeReq(t.CommitVoteV2)
			if err != nil {
				return nil, fmt.Errorf("commitVote: %w", err)
			}
			return &ConsensusMsgCommitVote{vote}, nil
		case *pb.ConsensusMsg_TimeoutVote:
			return FullTimeoutVoteConv.DecodeReq(t.TimeoutVote)
		case *pb.ConsensusMsg_TimeoutQc:
			return TimeoutQCConv.DecodeReq(t.TimeoutQc)
		default:
			return nil, fmt.Errorf("unknown ConsensusMsg type: %T", t)
		}
	},
}

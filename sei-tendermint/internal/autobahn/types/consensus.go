package types

import (
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-stream/pkg/utils"
	"github.com/tendermint/tendermint/internal/autobahn/pkg/protocol"
)

// ConsensusReq is the interface for all consensus messages.
type ConsensusReq interface {
	isConsensusReq()
	View() View
}

// ConsensusReqPrepareVote is a PrepareVote variant of ConsensusReq.
type ConsensusReqPrepareVote struct{ *Signed[*PrepareVote] }

// ConsensusReqCommitVote is a CommitVote variant of ConsensusReq.
type ConsensusReqCommitVote struct{ *Signed[*CommitVote] }

// View implements ConsensusReq.
func (m *ConsensusReqPrepareVote) View() View { return m.Msg().Proposal().View() }

// View implements ConsensusReq.
func (m *ConsensusReqCommitVote) View() View { return m.Msg().Proposal().View() }

func (m *FullProposal) isConsensusReq()            {}
func (m *ConsensusReqPrepareVote) isConsensusReq() {}
func (m *ConsensusReqCommitVote) isConsensusReq()  {}
func (m *FullTimeoutVote) isConsensusReq()         {}
func (m *TimeoutQC) isConsensusReq()               {}

// ConsensusReqConv is the protobuf converter for ConsensusReq.
var ConsensusReqConv = utils.ProtoConv[ConsensusReq, *protocol.ConsensusReq]{
	Encode: func(m ConsensusReq) *protocol.ConsensusReq {
		switch m := m.(type) {
		case *FullProposal:
			return &protocol.ConsensusReq{
				T: &protocol.ConsensusReq_Proposal{Proposal: FullProposalConv.Encode(m)},
			}
		case *ConsensusReqPrepareVote:
			return &protocol.ConsensusReq{
				T: &protocol.ConsensusReq_PrepareVote{PrepareVote: SignedMsgConv[*PrepareVote]().Encode(m.Signed)},
			}
		case *ConsensusReqCommitVote:
			return &protocol.ConsensusReq{
				T: &protocol.ConsensusReq_CommitVote{CommitVote: SignedMsgConv[*CommitVote]().Encode(m.Signed)},
			}
		case *FullTimeoutVote:
			return &protocol.ConsensusReq{
				T: &protocol.ConsensusReq_TimeoutVote{TimeoutVote: FullTimeoutVoteConv.Encode(m)},
			}
		case *TimeoutQC:
			return &protocol.ConsensusReq{
				T: &protocol.ConsensusReq_TimeoutQc{TimeoutQc: TimeoutQCConv.Encode(m)},
			}
		default:
			panic(fmt.Sprintf("Unknown ConsensusReq type: %T", m))
		}
	},
	Decode: func(m *protocol.ConsensusReq) (ConsensusReq, error) {
		if m.T == nil {
			return nil, errors.New("empty")
		}
		switch t := m.T.(type) {
		case *protocol.ConsensusReq_Proposal:
			return FullProposalConv.DecodeReq(t.Proposal)
		case *protocol.ConsensusReq_PrepareVote:
			vote, err := SignedMsgConv[*PrepareVote]().DecodeReq(t.PrepareVote)
			if err != nil {
				return nil, fmt.Errorf("prepareVote: %w", err)
			}
			return &ConsensusReqPrepareVote{vote}, nil
		case *protocol.ConsensusReq_CommitVote:
			vote, err := SignedMsgConv[*CommitVote]().DecodeReq(t.CommitVote)
			if err != nil {
				return nil, fmt.Errorf("commitVote: %w", err)
			}
			return &ConsensusReqCommitVote{vote}, nil
		case *protocol.ConsensusReq_TimeoutVote:
			return FullTimeoutVoteConv.DecodeReq(t.TimeoutVote)
		case *protocol.ConsensusReq_TimeoutQc:
			return TimeoutQCConv.DecodeReq(t.TimeoutQc)
		default:
			return nil, fmt.Errorf("unknown ConsensusReq type: %T", t)
		}
	},
}

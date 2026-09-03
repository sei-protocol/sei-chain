package types

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// AppHash represents EVM state hash.
// We don't even know here how long the hash can be.
type AppHash []byte

// AppProposal .
type AppProposal struct {
	utils.ReadOnly
	epochIndex  EpochIndex
	roadIndex   RoadIndex
	globalRange GlobalRange
	appHash     AppHash
}

// NewAppProposal creates a new AppProposal.
func NewAppProposal(proposal *Proposal, appHash AppHash) *AppProposal {
	return &AppProposal{
		globalRange: proposal.GlobalRange(),
		roadIndex:   proposal.Index(),
		appHash:     appHash,
		epochIndex:  proposal.EpochIndex(),
	}
}

// RoadIndex returns the road index of the proposal.
func (m *AppProposal) RoadIndex() RoadIndex { return m.roadIndex }

// AppHash .
func (m *AppProposal) AppHash() AppHash { return m.appHash }

// EpochIndex returns the epoch this proposal belongs to.
func (m *AppProposal) EpochIndex() EpochIndex { return m.epochIndex }

// GlobalRange returns the global block range covered by the proposal.
func (m *AppProposal) GlobalRange() GlobalRange { return m.globalRange }

// Next is the next road index after this proposal.
func (m *AppProposal) Next() RoadIndex {
	return m.RoadIndex() + 1
}

// Verify verifies that the AppProposal is consistent with the CommitQC.
func (m *AppProposal) Verify(qc *CommitQC) error {
	if got, want := m.RoadIndex(), qc.Proposal().Index(); got != want {
		return fmt.Errorf("roadIndex() = %v, want %v", got, want)
	}
	if got, want := m.EpochIndex(), qc.Proposal().EpochIndex(); got != want {
		return fmt.Errorf("epoch_index = %d, want %d", got, want)
	}
	if got, want := m.GlobalRange(), qc.GlobalRange(); got != want {
		return fmt.Errorf("global_range = %v, want %v", got, want)
	}
	return nil
}

// AppProposalConv is a protobuf converter for AppProposal.
var AppProposalConv = protoutils.Conv[*AppProposal, *pb.AppProposal]{
	Encode: func(m *AppProposal) *pb.AppProposal {
		return &pb.AppProposal{
			RoadIndex:   utils.Alloc(uint64(m.roadIndex)),
			AppHash:     m.appHash,
			EpochIndex:  utils.Alloc(uint64(m.epochIndex)),
			GlobalFirst: utils.Alloc(uint64(m.globalRange.First)),
			GlobalNext:  utils.Alloc(uint64(m.globalRange.Next)),
		}
	},
	Decode: func(m *pb.AppProposal) (*AppProposal, error) {
		if m.RoadIndex == nil {
			return nil, fmt.Errorf("road_index: missing")
		}
		if m.EpochIndex == nil {
			return nil, fmt.Errorf("epoch_index: missing")
		}
		if m.GlobalFirst == nil {
			return nil, fmt.Errorf("global_first: missing")
		}
		if m.GlobalNext == nil {
			return nil, fmt.Errorf("global_next: missing")
		}
		if len(m.AppHash) == 0 {
			return nil, fmt.Errorf("app_hash: missing")
		}
		return &AppProposal{
			epochIndex: EpochIndex(*m.EpochIndex),
			roadIndex:  RoadIndex(*m.RoadIndex),
			globalRange: GlobalRange{
				First: GlobalBlockNumber(*m.GlobalFirst),
				Next:  GlobalBlockNumber(*m.GlobalNext),
			},
			appHash: AppHash(m.AppHash),
		}, nil
	},
}

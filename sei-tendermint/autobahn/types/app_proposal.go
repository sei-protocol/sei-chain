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
	globalNumber GlobalBlockNumber
	roadIndex    RoadIndex
	appHash      AppHash
	epochIndex   EpochIndex
}

// NewAppProposal creates a new AppProposal.
func NewAppProposal(globalNumber GlobalBlockNumber, roadIndex RoadIndex, appHash AppHash, epochIndex EpochIndex) *AppProposal {
	return &AppProposal{globalNumber: globalNumber, roadIndex: roadIndex, appHash: appHash, epochIndex: epochIndex}
}

// GlobalNumber .
func (m *AppProposal) GlobalNumber() GlobalBlockNumber { return m.globalNumber }

// RoadIndex returns the road index of the proposal.
func (m *AppProposal) RoadIndex() RoadIndex { return m.roadIndex }

// AppHash .
func (m *AppProposal) AppHash() AppHash { return m.appHash }

// EpochIndex returns the epoch this proposal belongs to.
func (m *AppProposal) EpochIndex() EpochIndex { return m.epochIndex }

// Next is the next global block number to compute AppHash for.
func (m *AppProposal) Next() RoadIndex {
	return m.RoadIndex() + 1
}

// Verify verifies that the AppProposal is consistent with the CommitQC.
func (m *AppProposal) Verify(qc *CommitQC) error {
	if got, want := m.RoadIndex(), qc.Proposal().Index(); got != want {
		return fmt.Errorf("roadIndex() = %v, want %v", got, want)
	}
	if got, want := m.GlobalNumber(), qc.GlobalRange(); got < want.First || got >= want.Next {
		return fmt.Errorf("globalNumber() = %v, want in range [%v,%v)", got, want.First, want.Next)
	}
	if got, want := m.EpochIndex(), qc.Proposal().EpochIndex(); got != want {
		return fmt.Errorf("epoch_index = %d, want %d", got, want)
	}
	return nil
}

// AppProposalConv is a protobuf converter for AppProposal.
var AppProposalConv = protoutils.Conv[*AppProposal, *pb.AppProposal]{
	Encode: func(m *AppProposal) *pb.AppProposal {
		return &pb.AppProposal{
			GlobalNumber: utils.Alloc(uint64(m.globalNumber)),
			RoadIndex:    utils.Alloc(uint64(m.roadIndex)),
			AppHash:      m.appHash,
			EpochIndex:   utils.Alloc(uint64(m.epochIndex)),
		}
	},
	Decode: func(m *pb.AppProposal) (*AppProposal, error) {
		if m.GlobalNumber == nil {
			return nil, fmt.Errorf("global_number: missing")
		}
		if m.RoadIndex == nil {
			return nil, fmt.Errorf("road_index: missing")
		}
		if m.EpochIndex == nil {
			return nil, fmt.Errorf("epoch_index: missing")
		}
		return &AppProposal{
			globalNumber: GlobalBlockNumber(*m.GlobalNumber),
			roadIndex:    RoadIndex(*m.RoadIndex),
			appHash:      AppHash(m.AppHash),
			epochIndex:   EpochIndex(*m.EpochIndex),
		}, nil
	},
}

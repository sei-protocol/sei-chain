package types

import (
	"fmt"

	"github.com/sei-protocol/sei-stream/pkg/utils"
	"github.com/tendermint/tendermint/internal/autobahn/pkg/protocol"
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
}

// NewAppProposal creates a new AppProposal.
func NewAppProposal(globalNumber GlobalBlockNumber, roadIndex RoadIndex, appHash AppHash) *AppProposal {
	return &AppProposal{globalNumber: globalNumber, roadIndex: roadIndex, appHash: appHash}
}

// GlobalNumber .
func (m *AppProposal) GlobalNumber() GlobalBlockNumber { return m.globalNumber }

// RoadIndex returns the road index of the proposal.
func (m *AppProposal) RoadIndex() RoadIndex { return m.roadIndex }

// AppHash .
func (m *AppProposal) AppHash() AppHash { return m.appHash }

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
	return nil
}

// AppProposalConv is a protobuf converter for AppProposal.
var AppProposalConv = utils.ProtoConv[*AppProposal, *protocol.AppProposal]{
	Encode: func(m *AppProposal) *protocol.AppProposal {
		return &protocol.AppProposal{
			GlobalNumber: uint64(m.globalNumber),
			RoadIndex:    uint64(m.roadIndex),
			AppHash:      m.appHash,
		}
	},
	Decode: func(m *protocol.AppProposal) (*AppProposal, error) {
		return &AppProposal{
			globalNumber: GlobalBlockNumber(m.GlobalNumber),
			roadIndex:    RoadIndex(m.RoadIndex),
			appHash:      AppHash(m.AppHash),
		}, nil
	},
}

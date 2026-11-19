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
	epochIndex EpochIndex
	height     RoadHeight
	appHash    AppHash
}

// NewAppProposal creates a new AppProposal.
func NewAppProposal(view View, appHash AppHash) *AppProposal {
	return &AppProposal{
		epochIndex: view.EpochIndex,
		height:     view.Height,
		appHash:    appHash,
	}
}

// Height returns the road height of the proposal.
func (m *AppProposal) Height() RoadHeight { return m.height }

// AppHash .
func (m *AppProposal) AppHash() AppHash { return m.appHash }

// EpochIndex returns the epoch this proposal belongs to.
func (m *AppProposal) EpochIndex() EpochIndex { return m.epochIndex }

// Verify verifies that the AppProposal is consistent with the CommitQC.
func (m *AppProposal) Verify(qc *CommitQC) error {
	if got, want := m.Height(), qc.Height(); got != want {
		return fmt.Errorf("Height() = %v, want %v", got, want)
	}
	if got, want := m.EpochIndex(), qc.View().EpochIndex; got != want {
		return fmt.Errorf("epoch_index = %d, want %d", got, want)
	}

	return nil
}

// AppProposalConv is a protobuf converter for AppProposal.
var AppProposalConv = protoutils.Conv[*AppProposal, *pb.AppProposal]{
	Encode: func(m *AppProposal) *pb.AppProposal {
		return &pb.AppProposal{
			Height:     new(uint64(m.height)),
			AppHash:    m.appHash,
			EpochIndex: new(uint64(m.epochIndex)),
		}
	},
	Decode: func(m *pb.AppProposal) (*AppProposal, error) {
		if m.Height == nil {
			return nil, fmt.Errorf("height: missing")
		}
		if m.EpochIndex == nil {
			return nil, fmt.Errorf("epoch_index: missing")
		}
		if len(m.AppHash) == 0 {
			return nil, fmt.Errorf("app_hash: missing")
		}
		return &AppProposal{
			epochIndex: EpochIndex(*m.EpochIndex),
			height:     RoadHeight(*m.Height),
			appHash:    AppHash(m.AppHash),
		}, nil
	},
}

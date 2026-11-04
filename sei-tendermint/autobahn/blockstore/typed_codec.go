package blockstore

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// Consensus values are encoded only at the LittDB boundary.

// encodeLaneBlock marshals a lane block to its protobuf bytes.
func encodeLaneBlock(block *types.Block) []byte { return types.BlockConv.Marshal(block) }

// decodeLaneBlock unmarshals a lane block from its protobuf bytes.
func decodeLaneBlock(value []byte) (*types.Block, error) {
	block, err := types.BlockConv.Unmarshal(value)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal block: %w", err)
	}
	return block, nil
}

// encodeQC marshals a FullCommitQC to its protobuf bytes.
func encodeQC(qc *types.FullCommitQC) []byte {
	return types.FullCommitQCConv.Marshal(qc)
}

// decodeQC unmarshals a FullCommitQC from its protobuf bytes.
func decodeQC(value []byte) (*types.FullCommitQC, error) {
	qc, err := types.FullCommitQCConv.Unmarshal(value)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal qc: %w", err)
	}
	return qc, nil
}

// decodeAppProposal unmarshals an application proposal.
func decodeAppProposal(value []byte) (*types.AppProposal, error) {
	appProposal, err := types.AppProposalConv.Unmarshal(value)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal appProposal: %w", err)
	}
	return appProposal, nil
}

// decodeAppQC unmarshals an application QC.
func decodeAppQC(value []byte) (*types.AppQC, error) {
	appQC, err := types.AppQCConv.Unmarshal(value)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal appQC: %w", err)
	}
	return appQC, nil
}

package blockstore

import (
	"fmt"

	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// This file is the single home for translating between the in-memory consensus
// types and the opaque byte values the underlying BlockDB stores.

// encodeBlock marshals a block to its protobuf bytes.
func encodeBlock(block *types.Block) []byte { return types.BlockConv.Marshal(block) }

// decodeBlock unmarshals a block from its protobuf bytes.
func decodeBlock(value []byte) (*types.Block, error) {
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

// encodeAppProposal marshals an AppProposal to its protobuf bytes.
func encodeAppProposal(appProposal *types.AppProposal) []byte {
	return types.AppProposalConv.Marshal(appProposal)
}

// decodeAppProposal unmarshals an AppProposal from the value produced by
// encodeAppProposal.
func decodeAppProposal(value []byte) (*types.AppProposal, error) {
	appProposal, err := types.AppProposalConv.Unmarshal(value)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal appProposal: %w", err)
	}
	return appProposal, nil
}

// encodeAppQC marshals an AppQC to its protobuf bytes.
func encodeAppQC(appQC *types.AppQC) []byte {
	return types.AppQCConv.Marshal(appQC)
}

// decodeAppQC unmarshals an AppQC from the value produced by encodeAppQC.
func decodeAppQC(value []byte) (*types.AppQC, error) {
	appQC, err := types.AppQCConv.Unmarshal(value)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal appQC: %w", err)
	}
	return appQC, nil
}

// decodeQCGroupBlocks decodes and validates a QC's complete block set.
func decodeQCGroupBlocks(qc *types.FullCommitQC, blocks []blocktypes.BlockRecord) (types.QCGroup, error) {
	group := types.QCGroup{QC: qc, Blocks: make([]*types.Block, 0, len(blocks))}
	if len(blocks) != len(qc.Headers()) || uint64(len(blocks)) != qc.QC().GlobalRange().Len() {
		return types.QCGroup{}, types.ErrQCBlocksMismatch
	}
	for i, value := range blocks {
		block, err := decodeBlock(value.Value)
		if err != nil {
			return types.QCGroup{}, err
		}
		if types.GlobalBlockNumber(value.Number) != qc.QC().GlobalRange().First+types.GlobalBlockNumber(i) || block.Header().Hash() != qc.Headers()[i].Hash() { //nolint:gosec // non-negative slice index
			return types.QCGroup{}, types.ErrQCBlocksMismatch
		}
		group.Blocks = append(group.Blocks, block)
	}
	return group, nil
}

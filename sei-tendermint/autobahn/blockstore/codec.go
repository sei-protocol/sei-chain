package blockstore

import (
	"encoding/binary"
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

// encodeBlocks encodes the lane blocks in CommitQC header order.
func encodeBlocks(blocks []*types.Block) []byte {
	var value []byte
	for _, block := range blocks {
		encoded := encodeBlock(block)
		value = binary.BigEndian.AppendUint32(value, uint32(len(encoded))) //nolint:gosec // lane block size is bounded
		value = append(value, encoded...)
	}
	return value
}

// decodeBlocks decodes a complete ordered set of lane blocks.
func decodeBlocks(value []byte) ([]*types.Block, error) {
	var blocks []*types.Block
	for len(value) > 0 {
		if len(value) < 4 {
			return nil, fmt.Errorf("truncated lane block length")
		}
		length := uint64(binary.BigEndian.Uint32(value))
		value = value[4:]
		if length > uint64(len(value)) {
			return nil, fmt.Errorf("truncated lane block")
		}
		block, err := decodeBlock(value[:length])
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
		value = value[length:]
	}
	return blocks, nil
}

// validateBlocks checks that lane blocks match their CommitQC in header order.
func validateBlocks(qc *types.FullCommitQC, blocks []*types.Block) error {
	if len(blocks) != len(qc.Headers()) || uint64(len(blocks)) != qc.QC().Proposal().NumBlocks() {
		return types.ErrQCBlocksMismatch
	}
	for i, block := range blocks {
		if err := block.Verify(); err != nil {
			return fmt.Errorf("block %d: %w", i, err)
		}
		if block.Header().Hash() != qc.Headers()[i].Hash() {
			return types.ErrQCBlocksMismatch
		}
	}
	return nil
}

// decodeQCGroupBlocks decodes and validates a QC's complete block set.
func decodeQCGroupBlocks(qc *types.FullCommitQC, records []blocktypes.BlockRecord) (types.QCGroup, error) {
	if len(records) != 1 || types.RoadIndex(records[0].Number) != qc.Index() {
		return types.QCGroup{}, types.ErrQCBlocksMismatch
	}
	blocks, err := decodeBlocks(records[0].Value)
	if err != nil {
		return types.QCGroup{}, err
	}
	if err := validateBlocks(qc, blocks); err != nil {
		return types.QCGroup{}, err
	}
	return types.QCGroup{QC: qc, Blocks: blocks}, nil
}

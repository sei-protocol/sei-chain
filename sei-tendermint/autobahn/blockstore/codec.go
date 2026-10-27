package blockstore

import (
	"fmt"

	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// This file is the single home for translating between the in-memory consensus
// types and the opaque byte values the underlying BlockDB stores.

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

// encodeBlock encodes a road block with its SuperBlock lookup hash.
func encodeBlock(block *types.RoadBlock) blocktypes.BlockRecord {
	qc, blocks := block.QC(), block.LaneBlocks()
	record := blocktypes.BlockRecord{
		Index: blocktypes.BlockNumber(qc.Index()),
		Hash:  blocktypes.BlockHash(block.SuperBlock().CalculateBlockHash()),
		QC:    encodeQC(qc),
	}
	for _, lane := range blocks {
		record.LaneBlocks = append(record.LaneBlocks, encodeLaneBlock(lane))
	}
	return record
}

// decodeBlock decodes a road block record and checks its index and lookup hash.
func decodeBlock(record blocktypes.BlockRecord) (*types.RoadBlock, error) {
	qc, err := decodeQC(record.QC)
	if err != nil {
		return nil, err
	}
	if qc.Index() != types.RoadIndex(record.Index) {
		return nil, types.ErrQCBlocksMismatch
	}
	blocks := make([]*types.Block, len(record.LaneBlocks))
	for i, value := range record.LaneBlocks {
		block, err := decodeLaneBlock(value)
		if err != nil {
			return nil, err
		}
		blocks[i] = block
	}
	block := types.NewRoadBlock(qc, blocks)
	if blocktypes.BlockHash(block.SuperBlock().CalculateBlockHash()) != record.Hash {
		return nil, types.ErrQCBlocksMismatch
	}
	return block, nil
}

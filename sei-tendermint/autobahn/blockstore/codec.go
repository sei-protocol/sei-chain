package blockstore

import (
	"encoding/binary"
	"fmt"

	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// This file is the single home for translating between the in-memory consensus
// types and the opaque byte values the underlying BlockDB stores.

// blockValuePrefixLen is the size of the GlobalBlockNumber preceding block proto bytes.
const blockValuePrefixLen = 8

// encodeBlock marshals a block with its GlobalBlockNumber prefix.
func encodeBlock(n types.GlobalBlockNumber, blk *types.Block) []byte {
	proto := types.BlockConv.Marshal(blk)
	value := make([]byte, 0, blockValuePrefixLen+len(proto))
	// By-hash lookups need the number even when they read only this block's slice.
	value = binary.BigEndian.AppendUint64(value, uint64(n))
	value = append(value, proto...)
	return value
}

// decodeBlock unmarshals a block and its embedded GlobalBlockNumber from the
// value produced by encodeBlock.
func decodeBlock(value []byte) (types.GlobalBlockNumber, *types.Block, error) {
	if len(value) < blockValuePrefixLen {
		return 0, nil, fmt.Errorf("block value too short: %d bytes", len(value))
	}
	n := types.GlobalBlockNumber(binary.BigEndian.Uint64(value[:blockValuePrefixLen]))
	blk, err := types.BlockConv.Unmarshal(value[blockValuePrefixLen:])
	if err != nil {
		return 0, nil, fmt.Errorf("failed to unmarshal block: %w", err)
	}
	return n, blk, nil
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
	value, err := blocktypes.ConsumeVersion(value)
	if err != nil {
		return nil, err
	}
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
	value, err := blocktypes.ConsumeVersion(value)
	if err != nil {
		return nil, err
	}
	appQC, err := types.AppQCConv.Unmarshal(value)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal appQC: %w", err)
	}
	return appQC, nil
}

// decodeQCGroup decodes a complete committed group.
func decodeQCGroup(value []byte) (types.QCGroup, error) {
	qcValue, blocks, err := blocktypes.DecodeQCGroup(value)
	if err != nil {
		return types.QCGroup{}, err
	}
	qc, err := decodeQC(qcValue)
	if err != nil {
		return types.QCGroup{}, err
	}
	group := types.QCGroup{QC: qc, Blocks: make([]*types.Block, 0, len(blocks))}
	if len(blocks) != len(qc.Headers()) || uint64(len(blocks)) != qc.QC().GlobalRange().Len() {
		return types.QCGroup{}, types.ErrQCBlocksMismatch
	}
	for i, value := range blocks {
		n, block, err := decodeBlock(value)
		if err != nil {
			return types.QCGroup{}, err
		}
		if n != qc.QC().GlobalRange().First+types.GlobalBlockNumber(i) || block.Header().Hash() != qc.Headers()[i].Hash() { //nolint:gosec // non-negative slice index
			return types.QCGroup{}, types.ErrQCBlocksMismatch
		}
		group.Blocks = append(group.Blocks, block)
	}
	return group, nil
}

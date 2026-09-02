package blockstore

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// This file is the single home for translating between the in-memory consensus
// types and the opaque byte values the underlying BlockDB stores.

// Serialization version for blocks.
const blockSerializationVersion byte = 1

// Serialization version for QCs.
const qcSerializationVersion byte = 1

// Serialization version for AppQCs.
const appQCSerializationVersion byte = 1

// Serialization version for AppProposals.
const appProposalSerializationVersion byte = 1

// blockValuePrefixLen is the fixed header preceding a block's proto bytes: one
// version byte followed by the 8-byte big-endian GlobalBlockNumber.
const blockValuePrefixLen = 1 + 8

// encodeBlock marshals a block to the bytes stored as its record value. The value
// is framed as [version:1][GlobalBlockNumber:8 big-endian][proto(Block)]. The
// number is embedded so a by-hash lookup — which reaches this same shared value
// through an alias that carries only the hash — can still recover it.
func encodeBlock(n types.GlobalBlockNumber, blk *types.Block) []byte {
	proto := types.BlockConv.Marshal(blk)
	value := make([]byte, 0, blockValuePrefixLen+len(proto))
	value = append(value, blockSerializationVersion)
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
	if value[0] != blockSerializationVersion {
		return 0, nil, fmt.Errorf("unsupported block serialization version %d", value[0])
	}
	n := types.GlobalBlockNumber(binary.BigEndian.Uint64(value[1:blockValuePrefixLen]))
	blk, err := types.BlockConv.Unmarshal(value[blockValuePrefixLen:])
	if err != nil {
		return 0, nil, fmt.Errorf("failed to unmarshal block: %w", err)
	}
	return n, blk, nil
}

// encodeQC marshals a FullCommitQC to the bytes stored as its record value,
// framed as [version:1][proto(FullCommitQC)].
func encodeQC(qc *types.FullCommitQC) []byte {
	proto := types.FullCommitQCConv.Marshal(qc)
	value := make([]byte, 0, 1+len(proto))
	value = append(value, qcSerializationVersion)
	value = append(value, proto...)
	return value
}

// decodeQC unmarshals a FullCommitQC from the value produced by encodeQC.
func decodeQC(value []byte) (*types.FullCommitQC, error) {
	if len(value) < 1 {
		return nil, fmt.Errorf("qc value too short: %d bytes", len(value))
	}
	if value[0] != qcSerializationVersion {
		return nil, fmt.Errorf("unsupported qc serialization version %d", value[0])
	}
	qc, err := types.FullCommitQCConv.Unmarshal(value[1:])
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal qc: %w", err)
	}
	return qc, nil
}

// encodeAppProposal marshals an AppProposal to the bytes stored as its record
// value, framed as [version:1][proto(AppProposal)].
func encodeAppProposal(appProposal *types.AppProposal) []byte {
	proto := types.AppProposalConv.Marshal(appProposal)
	value := make([]byte, 0, 1+len(proto))
	value = append(value, appProposalSerializationVersion)
	value = append(value, proto...)
	return value
}

// decodeAppProposal unmarshals an AppProposal from the value produced by
// encodeAppProposal.
func decodeAppProposal(value []byte) (*types.AppProposal, error) {
	if len(value) < 1 {
		return nil, fmt.Errorf("appProposal value too short: %d bytes", len(value))
	}
	if value[0] != appProposalSerializationVersion {
		return nil, fmt.Errorf("unsupported appProposal serialization version %d", value[0])
	}
	appProposal, err := types.AppProposalConv.Unmarshal(value[1:])
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal appProposal: %w", err)
	}
	return appProposal, nil
}

// encodeAppQC marshals an AppQC to the bytes stored as its record value,
// framed as [version:1][proto(AppQC)].
func encodeAppQC(appQC *types.AppQC) []byte {
	proto := types.AppQCConv.Marshal(appQC)
	value := make([]byte, 0, 1+len(proto))
	value = append(value, appQCSerializationVersion)
	value = append(value, proto...)
	return value
}

// decodeAppQC unmarshals an AppQC from the value produced by encodeAppQC.
func decodeAppQC(value []byte) (*types.AppQC, error) {
	if len(value) < 1 {
		return nil, fmt.Errorf("appQC value too short: %d bytes", len(value))
	}
	if value[0] != appQCSerializationVersion {
		return nil, fmt.Errorf("unsupported appQC serialization version %d", value[0])
	}
	appQC, err := types.AppQCConv.Unmarshal(value[1:])
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal appQC: %w", err)
	}
	return appQC, nil
}

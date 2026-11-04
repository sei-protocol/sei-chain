package blockstore

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"

	litttypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
)

// record contains the encoded data passed to LittDB.
type record struct {
	Key     ledgerKey
	Value   []byte
	Aliases []*litttypes.SecondaryKey
}

// encodeBlock encodes a complete road block with QC, hash, and lane-block aliases.
// It panics if the encoded record exceeds the uint32 size limit.
func encodeBlock(block *types.RoadBlock) record {
	index, hash := block.QC().Index(), block.Header().Hash()
	qc := types.FullCommitQCConv.Marshal(block.QC())
	lanes := make([][]byte, len(block.LaneBlocks()))
	size := uint64(12) + uint64(len(hash)) + uint64(len(qc))
	for i, lane := range block.LaneBlocks() {
		lanes[i] = types.BlockConv.Marshal(lane)
		size += 4 + uint64(len(lanes[i]))
	}
	r := record{Key: (blockKey{Index: index}).encode(), Value: make([]byte, 0, checkedSize(size))}
	r.appendUint64(uint64(index))
	r.appendBlockHash(hash)
	r.appendBytes(qc, (qcKey{Index: index}).encode())
	for i, lane := range lanes {
		r.appendBytes(lane, (laneBlockKey{Index: index, Offset: uint64(i)}).encode())
	}
	r.addAlias((blockHashKey{Hash: hash}).encode())
	return r
}

// addAlias aliases the complete current value under key.
func (r *record) addAlias(key ledgerKey) {
	r.Aliases = append(r.Aliases, &litttypes.SecondaryKey{Key: key.bytes(), Length: checkedSize(uint64(len(r.Value)))})
}

// appendUint64 appends a big-endian uint64 to the record's value.
func (r *record) appendUint64(value uint64) {
	r.Value = binary.BigEndian.AppendUint64(r.Value, value)
}

// appendBlockHash appends a fixed-width block hash to the record's value.
func (r *record) appendBlockHash(hash types.RoadBlockHeaderHash) {
	r.appendRawBytes(hash[:])
}

// appendRawBytes appends bytes without a length prefix.
func (r *record) appendRawBytes(value []byte) {
	_ = checkedSize(uint64(len(r.Value)) + uint64(len(value)))
	r.Value = append(r.Value, value...)
}

// appendBytes appends a uint32-length-prefixed value and aliases its payload under the supplied keys.
// The caller must ensure the resulting record fits within the uint32 size limit.
func (r *record) appendBytes(value []byte, aliases ...ledgerKey) {
	length := uint32(len(value)) //nolint:gosec // validated before encoding
	r.Value = binary.BigEndian.AppendUint32(r.Value, length)
	offset := uint32(len(r.Value)) //nolint:gosec // validated before encoding
	r.appendRawBytes(value)
	for _, key := range aliases {
		r.Aliases = append(r.Aliases, &litttypes.SecondaryKey{Key: key.bytes(), Offset: offset, Length: length})
	}
}

// encodeAppQC encodes an application QC at its road index.
func encodeAppQC(qc *types.AppQC) record {
	r := record{Key: (appQCKey{Index: qc.Proposal().Index()}).encode()}
	r.appendRawBytes(types.AppQCConv.Marshal(qc))
	return r
}

// encodeAppProposal encodes an application proposal at its road index.
func encodeAppProposal(proposal *types.AppProposal) record {
	r := record{Key: (appProposalKey{Index: proposal.Index()}).encode()}
	r.appendRawBytes(types.AppProposalConv.Marshal(proposal))
	return r
}

// consumeValue returns the next length-prefixed payload and the remaining input.
// It returns an error if the length prefix or payload is truncated.
func consumeValue(value []byte) ([]byte, []byte, error) {
	if len(value) < 4 {
		return nil, nil, fmt.Errorf("truncated value length")
	}
	length := uint64(binary.BigEndian.Uint32(value))
	value = value[4:]
	if length > uint64(len(value)) {
		return nil, nil, fmt.Errorf("truncated value")
	}
	return value[:length], value[length:], nil
}

// decodeBlock decodes a road block and validates its stored index and hash.
func decodeBlock(value []byte) (*types.RoadBlock, error) {
	if len(value) < 8 {
		return nil, fmt.Errorf("truncated road index")
	}
	index := types.RoadIndex(binary.BigEndian.Uint64(value))
	value = value[8:]
	var hash types.RoadBlockHeaderHash
	if len(value) < len(hash) {
		return nil, fmt.Errorf("truncated block hash")
	}
	copy(hash[:], value[:len(hash)])
	value = value[len(hash):]
	qcBytes, value, err := consumeValue(value)
	if err != nil {
		return nil, fmt.Errorf("consumeValue(): %w", err)
	}
	qc, err := decodeQC(qcBytes)
	if err != nil {
		return nil, fmt.Errorf("decodeQC(): %w", err)
	}
	if qc.Index() != index {
		return nil, types.ErrQCBlocksMismatch
	}
	var lanes []*types.Block
	for len(value) > 0 {
		var laneBytes []byte
		laneBytes, value, err = consumeValue(value)
		if err != nil {
			return nil, fmt.Errorf("consumeValue(): %w", err)
		}
		lane, err := decodeLaneBlock(laneBytes)
		if err != nil {
			return nil, fmt.Errorf("decodeLaneBlock(): %w", err)
		}
		lanes = append(lanes, lane)
	}
	block := types.NewRoadBlock(qc, lanes)
	if block.Header().Hash() != hash {
		return nil, types.ErrQCBlocksMismatch
	}
	return block, nil
}

func checkedSize(size uint64) uint32 {
	if size >= math.MaxUint32 {
		panic("block record exceeds value size limit")
	}
	return uint32(size)
}

// decodeLaneBlock unmarshals a lane block from its protobuf bytes.
func decodeLaneBlock(value []byte) (*types.Block, error) {
	block, err := types.BlockConv.Unmarshal(value)
	if err != nil {
		return nil, fmt.Errorf("types.BlockConv.Unmarshal(): %w", err)
	}
	return block, nil
}

// decodeQC unmarshals a FullCommitQC from its protobuf bytes.
func decodeQC(value []byte) (*types.FullCommitQC, error) {
	qc, err := types.FullCommitQCConv.Unmarshal(value)
	if err != nil {
		return nil, fmt.Errorf("types.FullCommitQCConv.Unmarshal(): %w", err)
	}
	return qc, nil
}

// decodeAppProposal unmarshals an application proposal.
func decodeAppProposal(value []byte) (*types.AppProposal, error) {
	appProposal, err := types.AppProposalConv.Unmarshal(value)
	if err != nil {
		return nil, fmt.Errorf("types.AppProposalConv.Unmarshal(): %w", err)
	}
	return appProposal, nil
}

// decodeAppQC unmarshals an application QC.
func decodeAppQC(value []byte) (*types.AppQC, error) {
	appQC, err := types.AppQCConv.Unmarshal(value)
	if err != nil {
		return nil, fmt.Errorf("types.AppQCConv.Unmarshal(): %w", err)
	}
	return appQC, nil
}

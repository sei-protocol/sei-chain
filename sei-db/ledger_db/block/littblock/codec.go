package littblock

import (
	"encoding/binary"
	"fmt"
	"math"

	litttypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"
)

// record contains the encoded data passed to LittDB.
type record struct {
	Key     ledgerKey
	Value   []byte
	Aliases []*litttypes.SecondaryKey
}

// encodeBlock encodes a complete SuperBlock with QC, hash, and lane-block aliases.
// It panics if the encoded record exceeds the uint32 size limit.
func encodeBlock(block blocktypes.BlockRecord) record {
	size := encodedBlockSize(block)
	r := record{Key: (blockKey{Index: block.Index}).encode(), Value: make([]byte, 0, size)}
	r.appendUint64(uint64(block.Index))
	r.appendBlockHash(block.Hash)
	r.appendBytes(block.QC, (qcKey{Index: block.Index}).encode())
	for i, lane := range block.LaneBlocks {
		r.appendBytes(lane, (laneBlockKey{Index: block.Index, Offset: uint64(i)}).encode())
	}
	r.addAlias((blockHashKey{Hash: block.Hash}).encode())
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
func (r *record) appendBlockHash(hash blocktypes.BlockHash) {
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
func encodeAppQC(qc blocktypes.AppQCRecord) record {
	r := record{Key: (appQCKey{Index: qc.Index}).encode()}
	r.appendRawBytes(qc.Data)
	return r
}

// encodeAppProposal encodes an application proposal at its road index.
func encodeAppProposal(proposal blocktypes.AppProposalRecord) record {
	r := record{Key: (appProposalKey{Index: proposal.Index}).encode()}
	r.appendRawBytes(proposal.Data)
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

// decodeBlock decodes a SuperBlock record, including an empty set of lane blocks.
func decodeBlock(value []byte) (blocktypes.BlockRecord, error) {
	if len(value) < 8 {
		return blocktypes.BlockRecord{}, fmt.Errorf("truncated road index")
	}
	block := blocktypes.BlockRecord{Index: blocktypes.BlockNumber(binary.BigEndian.Uint64(value))}
	value = value[8:]
	var err error
	if len(value) < len(block.Hash) {
		return blocktypes.BlockRecord{}, fmt.Errorf("truncated block hash")
	}
	copy(block.Hash[:], value[:len(block.Hash)])
	value = value[len(block.Hash):]
	block.QC, value, err = consumeValue(value)
	if err != nil {
		return blocktypes.BlockRecord{}, err
	}
	for len(value) > 0 {
		var laneBlock []byte
		laneBlock, value, err = consumeValue(value)
		if err != nil {
			return blocktypes.BlockRecord{}, err
		}
		block.LaneBlocks = append(block.LaneBlocks, laneBlock)
	}
	return block, nil
}

// encodedBlockSize returns the framed record size, panicking if it exceeds LittDB's value limit.
func encodedBlockSize(block blocktypes.BlockRecord) uint32 {
	size := uint64(12) + uint64(len(block.Hash)) + uint64(len(block.QC))
	for _, lane := range block.LaneBlocks {
		size += 4 + uint64(len(lane))
	}
	return checkedSize(size)
}

func checkedSize(size uint64) uint32 {
	if size >= math.MaxUint32 {
		panic("block record exceeds value size limit")
	}
	return uint32(size)
}

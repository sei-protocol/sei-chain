package littblock

import (
	"encoding/binary"
	"fmt"
	"math"
	"slices"

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
	r := record{Key: kindSuperBlock.key(block.Index), Value: make([]byte, 0, size)}
	r.Value = binary.BigEndian.AppendUint64(r.Value, uint64(block.Index))
	r.Value, _, _ = appendValue(r.Value, block.Hash)
	var offset, length uint32
	r.Value, offset, length = appendValue(r.Value, block.QC)
	r.Aliases = append(r.Aliases, &litttypes.SecondaryKey{Key: kindQC.key(block.Index), Offset: offset, Length: length})
	for i, lane := range block.LaneBlocks {
		r.Value, offset, length = appendValue(r.Value, lane)
		r.Aliases = append(r.Aliases, &litttypes.SecondaryKey{Key: laneBlockKey(block.Index, uint64(i)), Offset: offset, Length: length})
	}
	r.Aliases = append(r.Aliases, &litttypes.SecondaryKey{Key: blockHashKey(block.Hash), Length: size})
	return r
}

// appendValue appends a uint32-length-prefixed value and returns its payload offset and length.
// The caller must ensure the resulting buffer fits within the uint32 size limit.
func appendValue(dst, value []byte) ([]byte, uint32, uint32) {
	length := uint32(len(value)) //nolint:gosec // validated before encoding
	dst = binary.BigEndian.AppendUint32(dst, length)
	offset := uint32(len(dst)) //nolint:gosec // validated before encoding
	return append(dst, value...), offset, length
}

// encodeAppQC encodes an application QC at its road index.
func encodeAppQC(qc blocktypes.AppQCRecord) record { return encodeApp(kindAppQC, qc.Index, qc.Data) }

// encodeAppProposal encodes an application proposal at its road index.
func encodeAppProposal(proposal blocktypes.AppProposalRecord) record {
	return encodeApp(kindAppProp, proposal.Index, proposal.Data)
}

// encodeApp stores an app record's opaque payload under its road index.
func encodeApp(kind recordKind, index blocktypes.BlockNumber, data []byte) record {
	_ = checkedSize(uint64(len(data)))
	return record{Key: kind.key(index), Value: slices.Clone(data)}
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
	block.Hash, value, err = consumeValue(value)
	if err != nil {
		return blocktypes.BlockRecord{}, err
	}
	block.QC, value, err = consumeValue(value)
	if err != nil {
		return blocktypes.BlockRecord{}, err
	}
	for len(value) > 0 {
		var lane []byte
		lane, value, err = consumeValue(value)
		if err != nil {
			return blocktypes.BlockRecord{}, err
		}
		block.LaneBlocks = append(block.LaneBlocks, lane)
	}
	return block, nil
}

// ledgerKey is a varint kind followed by a road index, lane position, or SuperBlock hash.
type ledgerKey []byte

// key returns the numbered lookup key for kind.
func (kind recordKind) key(n blocktypes.BlockNumber) ledgerKey {
	return numberKey(kind, n)
}

// primaryKind identifies primary record keys; secondary or unknown kinds return false.
func (k ledgerKey) primaryKind() (recordKind, bool) {
	kind := keyKind(k)
	switch kind {
	case kindSuperBlock, kindAppProp, kindAppQC:
		return kind, true
	default:
		return 0, false
	}
}

// number returns the block number from a numbered key.
func (k ledgerKey) number() blocktypes.BlockNumber { return decodeNumberKey(k) }

// encodeKey encodes a record number as an 8-byte big-endian value. Big-endian
// is deliberate: it makes lexicographic byte order match numeric order. This is
// the inner codec shared by the prefixed key builders below.
func encodeKey(n blocktypes.BlockNumber) []byte { return binary.BigEndian.AppendUint64(nil, uint64(n)) }

// decodeKey decodes an 8-byte value produced by encodeKey.
func decodeKey(b [8]byte) blocktypes.BlockNumber {
	return blocktypes.BlockNumber(binary.BigEndian.Uint64(b[:]))
}

// numberKey returns the key for kind at road index n.
func numberKey(kind recordKind, n blocktypes.BlockNumber) ledgerKey {
	return append(binary.AppendUvarint(nil, uint64(kind)), encodeKey(n)...)
}

// blockHashKey returns the secondary key under which a block is reachable by its header hash.
func blockHashKey(hash blocktypes.BlockHash) ledgerKey {
	return append(binary.AppendUvarint(nil, uint64(kindBlockHash)), hash...)
}

// keyKind returns the unsigned varint kind, or MaxUint64 for a malformed prefix.
func keyKind(k []byte) recordKind {
	kind, n := binary.Uvarint(k)
	if n <= 0 {
		return recordKind(math.MaxUint64)
	}
	return recordKind(kind)
}

// decodeNumberKey decodes a key's block number after its varint kind.
func decodeNumberKey(k []byte) blocktypes.BlockNumber {
	_, n := binary.Uvarint(k)
	return decodeKey([8]byte(k[n:]))
}

// laneBlockKey returns a lane block alias keyed by road index and header offset.
func laneBlockKey(index blocktypes.BlockNumber, offset uint64) ledgerKey {
	return binary.BigEndian.AppendUint64(numberKey(kindLaneBlock, index), offset)
}

// encodedBlockSize returns the framed record size, panicking if it exceeds LittDB's value limit.
func encodedBlockSize(block blocktypes.BlockRecord) uint32 {
	size := uint64(16) + uint64(len(block.Hash)) + uint64(len(block.QC))
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

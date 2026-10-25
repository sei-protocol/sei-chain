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

// encodeQCGroup encodes a QC and its numbered blocks as one record with lookup aliases.
// It panics for an empty group, an unrepresentable block range, or an oversized value.
func encodeQCGroup(group blocktypes.QCGroupRecord) record {
	if len(group.Blocks) == 0 {
		panic("QC group requires blocks")
	}
	size := uint64(4) + uint64(len(group.QC.Data))
	for _, block := range group.Blocks {
		if block.Number == math.MaxUint64 {
			panic("block number has no exclusive upper bound")
		}
		size += 12 + uint64(len(block.Value))
	}
	if size >= math.MaxUint32 {
		panic("QC group exceeds value size limit")
	}
	key := kindQCGroup.key(group.Blocks[0].Number)
	r := record{Key: key}
	r.Value, _, _ = appendValue(r.Value, group.QC.Data)
	qcLength := uint32(len(r.Value)) //nolint:gosec // the encoder bounds the value size
	for _, block := range group.Blocks {
		value := binary.BigEndian.AppendUint64(nil, uint64(block.Number))
		value = append(value, block.Value...)
		var offset, length uint32
		r.Value, offset, length = appendValue(r.Value, value)
		key := kindBlock.key(block.Number)
		qcKey := kindQC.key(block.Number)
		r.Aliases = append(r.Aliases,
			&litttypes.SecondaryKey{Key: qcKey, Length: qcLength},
			&litttypes.SecondaryKey{Key: key, Offset: offset, Length: length},
			&litttypes.SecondaryKey{Key: blockHashKey(block.Hash), Offset: offset, Length: length})
	}
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

// encodeAppQC encodes an application QC and its covered-number aliases.
func encodeAppQC(qc blocktypes.AppQCRecord) record {
	return encodeApp(kindAppQC, qc.First, qc.Next, qc.Data)
}

// encodeAppProposal encodes an application proposal and its covered-number aliases.
func encodeAppProposal(proposal blocktypes.AppProposalRecord) record {
	return encodeApp(kindAppProp, proposal.First, proposal.Next, proposal.Data)
}

// encodeApp encodes the block range and opaque payload with aliases covering [first, next).
// It panics if the encoded value exceeds the uint32 size limit.
func encodeApp(kind recordKind, first, next blocktypes.BlockNumber, data []byte) record {
	if uint64(len(data))+16 >= math.MaxUint32 {
		panic("app record exceeds value size limit")
	}
	key := kind.key(first)
	r := record{Key: key}
	r.Value = binary.BigEndian.AppendUint64(r.Value, uint64(first))
	r.Value = binary.BigEndian.AppendUint64(r.Value, uint64(next))
	r.Value = append(r.Value, data...)
	for n := first + 1; n < next; n++ {
		key := kind.key(n)
		r.Aliases = append(r.Aliases, &litttypes.SecondaryKey{Key: key, Length: uint32(len(r.Value))}) //nolint:gosec // checked above
	}
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

// decodeQCGroup reads a QC and its numbered blocks in encoded order, rejecting empty groups.
// QC and block payloads remain opaque.
func decodeQCGroup(value []byte) (blocktypes.QCGroupRecord, error) {
	var group blocktypes.QCGroupRecord
	var err error
	group.QC.Data, value, err = consumeValue(value)
	if err != nil {
		return blocktypes.QCGroupRecord{}, err
	}
	for len(value) > 0 {
		var encoded []byte
		encoded, value, err = consumeValue(value)
		if err != nil {
			return blocktypes.QCGroupRecord{}, err
		}
		block, err := decodeBlock(encoded)
		if err != nil {
			return blocktypes.QCGroupRecord{}, err
		}
		group.Blocks = append(group.Blocks, block)
	}
	if len(group.Blocks) == 0 {
		return blocktypes.QCGroupRecord{}, fmt.Errorf("QC group requires blocks")
	}
	return group, nil
}

// decodeBlock reads the block number and opaque payload from a block entry.
// Hash is unset because it is stored only in the secondary lookup key.
func decodeBlock(value []byte) (blocktypes.BlockRecord, error) {
	if len(value) < 8 {
		return blocktypes.BlockRecord{}, fmt.Errorf("truncated block number")
	}
	return blocktypes.BlockRecord{
		Number: blocktypes.BlockNumber(binary.BigEndian.Uint64(value)),
		Value:  value[8:],
	}, nil
}

// decodeApp reads the first and next block numbers and the opaque application payload.
func decodeApp(value []byte) (blocktypes.BlockNumber, blocktypes.BlockNumber, []byte, error) {
	if len(value) < 16 {
		return 0, 0, nil, fmt.Errorf("truncated app record")
	}
	return blocktypes.BlockNumber(binary.BigEndian.Uint64(value[:8])), blocktypes.BlockNumber(binary.BigEndian.Uint64(value[8:16])), value[16:], nil
}

// decodeAppQC reads an application QC record without decoding its opaque payload.
func decodeAppQC(value []byte) (blocktypes.AppQCRecord, error) {
	first, next, data, err := decodeApp(value)
	return blocktypes.AppQCRecord{First: first, Next: next, Data: data}, err
}

// decodeAppProposal reads an application proposal record without decoding its opaque payload.
func decodeAppProposal(value []byte) (blocktypes.AppProposalRecord, error) {
	first, next, data, err := decodeApp(value)
	return blocktypes.AppProposalRecord{First: first, Next: next, Data: data}, err
}

// decodePayload returns a numbered lookup's opaque payload after validating its framing.
func decodePayload(kind recordKind, value []byte) ([]byte, error) {
	switch kind {
	case kindQC:
		data, _, err := consumeValue(value)
		return data, err
	case kindBlock:
		if len(value) < 8 {
			return nil, fmt.Errorf("truncated block number")
		}
		return value[8:], nil
	case kindAppQC, kindAppProp:
		if len(value) < 16 {
			return nil, fmt.Errorf("truncated app record")
		}
		return value[16:], nil
	default:
		panic(fmt.Errorf("invalid lookup record kind %d", kind))
	}
}

// recordKind identifies a primary record or secondary lookup namespace.
type recordKind uint64

const (
	kindQCGroup recordKind = iota + 1
	kindAppQC
	kindBlock
	kindBlockHash
	kindAppProp
	kindQC
)

// ledgerKey is a varint kind followed by a block number or block hash.
type ledgerKey []byte

// key returns the numbered lookup key for kind.
func (kind recordKind) key(n blocktypes.BlockNumber) ledgerKey {
	return numberKey(kind, n)
}

// primaryKind identifies primary record keys; secondary or unknown kinds return false.
func (k ledgerKey) primaryKind() (recordKind, bool) {
	kind := keyKind(k)
	switch kind {
	case kindQCGroup, kindAppProp, kindAppQC:
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

// numberKey returns the key under which a record of the given kind prefix is
// stored at number n — used both for a record's primary key and for each
// covered-number alias.
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

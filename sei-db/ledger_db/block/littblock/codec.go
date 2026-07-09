package littblock

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// This file is the single home for translating between the in-memory consensus
// types and the byte representations LittDB stores (both keys and values).

// Blocks and QCs share one LittDB table. Each key carries a 1-byte kind prefix
// so the block-number key space and the QC-number key space never collide:
//
//   - kindBlock     'b' + 8-byte big-endian GlobalBlockNumber (block primary key)
//   - kindBlockHash 'h' + 32-byte header hash               (block hash alias)
//   - kindQC        'q' + 8-byte big-endian GlobalBlockNumber (QC primary + covered aliases)
const (
	kindBlock     byte = 'b'
	kindBlockHash byte = 'h'
	kindQC        byte = 'q'
)

// encodeKey encodes a GlobalBlockNumber as an 8-byte big-endian value. Big-endian
// is deliberate: it makes lexicographic byte order match numeric order. This is
// the inner codec shared by the prefixed key builders below.
func encodeKey(n types.GlobalBlockNumber) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(n))
	return b
}

// decodeKey decodes an 8-byte value produced by encodeKey.
func decodeKey(b []byte) types.GlobalBlockNumber {
	return types.GlobalBlockNumber(binary.BigEndian.Uint64(b))
}

// blockKey returns the primary key under which a block at number n is stored.
func blockKey(n types.GlobalBlockNumber) []byte {
	return append([]byte{kindBlock}, encodeKey(n)...)
}

// blockHashKey returns the secondary (alias) key under which a block is reachable
// by its header hash.
func blockHashKey(hash types.BlockHeaderHash) []byte {
	return append([]byte{kindBlockHash}, hash.Bytes()...)
}

// qcKey returns the key for QC number n — used both for a QC's primary key (its
// lowerBound) and for each covered-number alias.
func qcKey(n types.GlobalBlockNumber) []byte {
	return append([]byte{kindQC}, encodeKey(n)...)
}

// keyKind returns the kind prefix byte of a stored key.
func keyKind(key []byte) byte {
	return key[0]
}

// decodeNumberKey decodes the GlobalBlockNumber from a kindBlock or kindQC key
// (i.e. a key whose prefix is followed by an 8-byte big-endian number).
func decodeNumberKey(key []byte) types.GlobalBlockNumber {
	return decodeKey(key[1:])
}

// Serialization version for blocks.
const blockSerializationVersion byte = 1

// Serialization version for QCs.
const qcSerializationVersion byte = 1

// blockValuePrefixLen is the fixed header preceding a block's proto bytes: one
// version byte followed by the 8-byte big-endian GlobalBlockNumber.
const blockValuePrefixLen = 1 + 8

// encodeBlock marshals a block to the bytes stored as its table value. The value
// is framed as [version:1][GlobalBlockNumber:8 big-endian][proto(Block)]. The
// number is embedded so a by-hash lookup — which reaches this same shared value
// through a secondary key that carries only the hash — can still recover it.
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

// encodeQC marshals a FullCommitQC to the bytes stored as its table value,
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

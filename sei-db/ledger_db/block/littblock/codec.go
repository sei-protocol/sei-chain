package littblock

import (
	"encoding/binary"

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

// encodeBlock marshals a block to the bytes stored as its table value.
func encodeBlock(blk *types.Block) []byte {
	return types.BlockConv.Marshal(blk)
}

// decodeBlock unmarshals a block from its stored table value.
func decodeBlock(value []byte) (*types.Block, error) {
	return types.BlockConv.Unmarshal(value)
}

// encodeQC marshals a FullCommitQC to the bytes stored as its table value.
func encodeQC(qc *types.FullCommitQC) []byte {
	return types.FullCommitQCConv.Marshal(qc)
}

// decodeQC unmarshals a FullCommitQC from its stored table value.
func decodeQC(value []byte) (*types.FullCommitQC, error) {
	return types.FullCommitQCConv.Unmarshal(value)
}

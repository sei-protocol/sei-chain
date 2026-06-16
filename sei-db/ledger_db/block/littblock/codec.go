package littblock

import (
	"encoding/binary"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// This file is the single home for translating between the in-memory consensus
// types and the byte representations LittDB stores (both keys and values).

// encodeKey encodes a GlobalBlockNumber as an 8-byte big-endian key. Big-endian
// is deliberate: it makes lexicographic byte order match numeric order.
func encodeKey(n types.GlobalBlockNumber) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(n))
	return b
}

// decodeKey decodes a key produced by encodeKey.
func decodeKey(b []byte) types.GlobalBlockNumber {
	return types.GlobalBlockNumber(binary.BigEndian.Uint64(b))
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

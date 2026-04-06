package littblockdb

import (
	"encoding/binary"
	"fmt"

	blockdb "github.com/sei-protocol/sei-chain/sei-db/block_db"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
)

// Key prefixes. Every key stored in the LittDB table is prefixed with one of these bytes so that
// height keys, block-hash keys, and tx-hash keys occupy non-overlapping namespaces.
const (
	prefixHeight byte = 'H'
	prefixBlock  byte = 'B'
	prefixTxHash byte = 'T'
)

// Value wire format (single contiguous blob per block):
//
//   [8B height LE]
//   [4B hashLen LE] [hash]
//   [4B blockDataLen LE] [blockData]
//   [4B txCount LE]
//   per tx:
//     [4B txHashLen LE] [txHash]
//     [4B txDataLen LE] [txData]
//
// Secondary keys:
//   'B' + blockHash  → Offset=0, Length=len(value)        (full blob alias)
//   'T' + txHash     → Offset=txDataStart, Length=txDataLen (raw tx bytes only)

func encodeHeightKey(height uint64) []byte {
	k := make([]byte, 9)
	k[0] = prefixHeight
	binary.BigEndian.PutUint64(k[1:], height)
	return k
}

func encodeBlockHashKey(hash []byte) []byte {
	k := make([]byte, 1+len(hash))
	k[0] = prefixBlock
	copy(k[1:], hash)
	return k
}

func encodeTxHashKey(hash []byte) []byte {
	k := make([]byte, 1+len(hash))
	k[0] = prefixTxHash
	copy(k[1:], hash)
	return k
}

// marshalBlock serializes a BinaryBlock into a contiguous value blob and computes the secondary
// keys that point into it. The returned secondary keys include one for the block hash (aliasing
// the full value) and one per transaction hash (pointing to the raw tx data sub-range).
func marshalBlock(block *blockdb.BinaryBlock) (value []byte, secondaryKeys []*types.SecondaryKey) {
	size := 8 + // height
		4 + len(block.Hash) + // hash
		4 + len(block.BlockData) + // blockData
		4 // txCount
	for _, tx := range block.Transactions {
		size += 4 + len(tx.Hash) + 4 + len(tx.Transaction)
	}

	buf := make([]byte, size)
	off := 0

	binary.LittleEndian.PutUint64(buf[off:], block.Height)
	off += 8

	binary.LittleEndian.PutUint32(buf[off:], uint32(len(block.Hash))) //nolint:gosec
	off += 4
	copy(buf[off:], block.Hash)
	off += len(block.Hash)

	binary.LittleEndian.PutUint32(buf[off:], uint32(len(block.BlockData))) //nolint:gosec
	off += 4
	copy(buf[off:], block.BlockData)
	off += len(block.BlockData)

	binary.LittleEndian.PutUint32(buf[off:], uint32(len(block.Transactions))) //nolint:gosec
	off += 4

	// 1 for block hash + 1 per tx hash
	secondaryKeys = make([]*types.SecondaryKey, 0, 1+len(block.Transactions))

	for _, tx := range block.Transactions {
		binary.LittleEndian.PutUint32(buf[off:], uint32(len(tx.Hash))) //nolint:gosec
		off += 4
		copy(buf[off:], tx.Hash)
		off += len(tx.Hash)

		txDataOffset := off + 4                                               // skip the txDataLen prefix
		binary.LittleEndian.PutUint32(buf[off:], uint32(len(tx.Transaction))) //nolint:gosec
		off += 4
		copy(buf[off:], tx.Transaction)
		off += len(tx.Transaction)

		secondaryKeys = append(secondaryKeys, &types.SecondaryKey{
			Key:    encodeTxHashKey(tx.Hash),
			Offset: uint32(txDataOffset),        //nolint:gosec
			Length: uint32(len(tx.Transaction)), //nolint:gosec
		})
	}

	// Block hash secondary key: aliases the full value.
	secondaryKeys = append(secondaryKeys, &types.SecondaryKey{
		Key:    encodeBlockHashKey(block.Hash),
		Offset: 0,
		Length: uint32(len(buf)), //nolint:gosec
	})

	return buf, secondaryKeys
}

// unmarshalBlock deserializes a full value blob (as returned by a primary-key or block-hash
// secondary-key lookup) into a BinaryBlock.
func unmarshalBlock(buf []byte) (*blockdb.BinaryBlock, error) {
	if len(buf) < 8 {
		return nil, fmt.Errorf("block value too short: %d bytes", len(buf))
	}
	off := 0

	height := binary.LittleEndian.Uint64(buf[off:])
	off += 8

	if off+4 > len(buf) {
		return nil, fmt.Errorf("block value truncated at hash length")
	}
	hashLen := int(binary.LittleEndian.Uint32(buf[off:]))
	off += 4
	if off+hashLen > len(buf) {
		return nil, fmt.Errorf("block value truncated at hash")
	}
	hash := make([]byte, hashLen)
	copy(hash, buf[off:off+hashLen])
	off += hashLen

	if off+4 > len(buf) {
		return nil, fmt.Errorf("block value truncated at data length")
	}
	dataLen := int(binary.LittleEndian.Uint32(buf[off:]))
	off += 4
	if off+dataLen > len(buf) {
		return nil, fmt.Errorf("block value truncated at data")
	}
	data := make([]byte, dataLen)
	copy(data, buf[off:off+dataLen])
	off += dataLen

	if off+4 > len(buf) {
		return nil, fmt.Errorf("block value truncated at tx count")
	}
	txCount := int(binary.LittleEndian.Uint32(buf[off:]))
	off += 4

	txs := make([]*blockdb.BinaryTransaction, txCount)
	for i := 0; i < txCount; i++ {
		if off+4 > len(buf) {
			return nil, fmt.Errorf("block value truncated at tx %d hash length", i)
		}
		txHashLen := int(binary.LittleEndian.Uint32(buf[off:]))
		off += 4
		if off+txHashLen > len(buf) {
			return nil, fmt.Errorf("block value truncated at tx %d hash", i)
		}
		txHash := make([]byte, txHashLen)
		copy(txHash, buf[off:off+txHashLen])
		off += txHashLen

		if off+4 > len(buf) {
			return nil, fmt.Errorf("block value truncated at tx %d data length", i)
		}
		txDataLen := int(binary.LittleEndian.Uint32(buf[off:]))
		off += 4
		if off+txDataLen > len(buf) {
			return nil, fmt.Errorf("block value truncated at tx %d data", i)
		}
		txData := make([]byte, txDataLen)
		copy(txData, buf[off:off+txDataLen])
		off += txDataLen

		txs[i] = &blockdb.BinaryTransaction{
			Hash:        txHash,
			Transaction: txData,
		}
	}

	return &blockdb.BinaryBlock{
		Height:       height,
		Hash:         hash,
		BlockData:    data,
		Transactions: txs,
	}, nil
}

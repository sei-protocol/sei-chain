package pebbleblockdb

import (
	"encoding/binary"
	"fmt"
)

// Block header wire format (stored under the B prefix):
//
//   [4B: hashLen][hashBytes][4B: dataLen][dataBytes][4B: txCount]
//     per tx: [4B: txHashLen][txHashBytes]
//
// No transaction payloads — those live under the X prefix.

func marshalBlockHeader(hash, blockData []byte, txHashes [][]byte) []byte {
	size := 4 + len(hash) + 4 + len(blockData) + 4
	for _, h := range txHashes {
		size += 4 + len(h)
	}

	buf := make([]byte, size)
	off := 0

	binary.LittleEndian.PutUint32(buf[off:], uint32(len(hash)))
	off += 4
	copy(buf[off:], hash)
	off += len(hash)

	binary.LittleEndian.PutUint32(buf[off:], uint32(len(blockData)))
	off += 4
	copy(buf[off:], blockData)
	off += len(blockData)

	binary.LittleEndian.PutUint32(buf[off:], uint32(len(txHashes)))
	off += 4

	for _, h := range txHashes {
		binary.LittleEndian.PutUint32(buf[off:], uint32(len(h)))
		off += 4
		copy(buf[off:], h)
		off += len(h)
	}
	return buf
}

type blockHeader struct {
	hash     []byte
	data     []byte
	txHashes [][]byte
}

func unmarshalBlockHeader(buf []byte) (*blockHeader, error) {
	if len(buf) < 4 {
		return nil, fmt.Errorf("block header too short: %d bytes", len(buf))
	}
	off := 0

	hashLen := int(binary.LittleEndian.Uint32(buf[off:]))
	off += 4
	if off+hashLen > len(buf) {
		return nil, fmt.Errorf("block header truncated at hash")
	}
	hash := make([]byte, hashLen)
	copy(hash, buf[off:off+hashLen])
	off += hashLen

	if off+4 > len(buf) {
		return nil, fmt.Errorf("block header truncated at data length")
	}
	dataLen := int(binary.LittleEndian.Uint32(buf[off:]))
	off += 4
	if off+dataLen > len(buf) {
		return nil, fmt.Errorf("block header truncated at data")
	}
	data := make([]byte, dataLen)
	copy(data, buf[off:off+dataLen])
	off += dataLen

	if off+4 > len(buf) {
		return nil, fmt.Errorf("block header truncated at tx count")
	}
	txCount := int(binary.LittleEndian.Uint32(buf[off:]))
	off += 4

	txHashes := make([][]byte, txCount)
	for i := 0; i < txCount; i++ {
		if off+4 > len(buf) {
			return nil, fmt.Errorf("block header truncated at tx hash %d length", i)
		}
		hLen := int(binary.LittleEndian.Uint32(buf[off:]))
		off += 4
		if off+hLen > len(buf) {
			return nil, fmt.Errorf("block header truncated at tx hash %d", i)
		}
		h := make([]byte, hLen)
		copy(h, buf[off:off+hLen])
		off += hLen
		txHashes[i] = h
	}

	return &blockHeader{hash: hash, data: data, txHashes: txHashes}, nil
}

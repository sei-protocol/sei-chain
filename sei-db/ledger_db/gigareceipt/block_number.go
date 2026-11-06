package gigareceipt

import "encoding/binary"

// blockNumLen is the byte length of an encoded block number.
const blockNumLen = 8

// encodeBlockNumber returns blockNumber as blockNumLen big-endian bytes.
func encodeBlockNumber(blockNumber uint64) []byte {
	buf := make([]byte, blockNumLen)
	binary.BigEndian.PutUint64(buf, blockNumber)
	return buf
}

// decodeBlockNumber reads a block number written by encodeBlockNumber.
func decodeBlockNumber(b []byte) uint64 {
	return binary.BigEndian.Uint64(b)
}

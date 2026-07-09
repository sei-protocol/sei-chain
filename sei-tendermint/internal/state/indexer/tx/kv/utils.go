package kv

import (
	"encoding/binary"
	"fmt"

	"github.com/google/orderedcode"
)

// int64ToBytes encodes an int64 as a varint. Used for the height-ordered index
// watermark value, which is a point value read/written by key (never scanned in
// order), so a compact non-order-preserving encoding is fine.
func int64ToBytes(i int64) []byte {
	buf := make([]byte, binary.MaxVarintLen64)
	n := binary.PutVarint(buf, i)
	return buf[:n]
}

// int64FromBytes decodes a varint-encoded int64 produced by int64ToBytes.
func int64FromBytes(bz []byte) int64 {
	v, _ := binary.Varint(bz)
	return v
}

// intInSlice returns true if a is found in the list.
func intInSlice(a int, list []int) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

// parseHeightIndexFromKey extracts the (height, index) suffix of a secondary
// (event) key. The tx index encodes each event key as
// orderedcode(compositeKey, value, height, int64(index)).
func parseHeightIndexFromKey(key []byte) (int64, uint32, error) {
	var (
		compositeKey, value string
		height, index       int64
	)

	remaining, err := orderedcode.Parse(string(key), &compositeKey, &value, &height, &index)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse event key: %w", err)
	}
	if len(remaining) != 0 {
		return 0, 0, fmt.Errorf("unexpected remainder in key: %s", remaining)
	}

	return height, uint32(index), nil //nolint:gosec // index is stored as int64(uint32) by secondaryKey, so the round-trip back to uint32 cannot overflow
}

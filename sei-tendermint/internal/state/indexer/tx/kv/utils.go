package kv

import (
	"fmt"

	"github.com/google/orderedcode"
)

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

	return height, uint32(index), nil
}

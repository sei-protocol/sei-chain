package kv

import (
	"encoding/binary"
	"fmt"
	"strconv"

	"github.com/google/orderedcode"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/pubsub/query/syntax"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// maxBoundedPrealloc caps how much the bounded fast path preallocates for its
// result slice, so a very large (or disabled) limit does not eagerly allocate.
const maxBoundedPrealloc = 4096

// boundedCap returns a sensible initial capacity for a result slice given a
// limit. A non-positive limit means "unbounded", in which case we let the
// slice grow on demand.
func boundedCap(limit int) int {
	if limit <= 0 {
		return 0
	}
	if limit < maxBoundedPrealloc {
		return limit
	}
	return maxBoundedPrealloc
}

// heightInRange reports whether height h falls within the (already
// inclusivity-adjusted) bounds of a numeric block.height query range.
func heightInRange(h int64, qr indexer.QueryRange) bool {
	if lower := qr.LowerBoundValue(); lower != nil {
		if lb, ok := lower.(int64); ok && h < lb {
			return false
		}
	}
	if upper := qr.UpperBoundValue(); upper != nil {
		if ub, ok := upper.(int64); ok && h > ub {
			return false
		}
	}
	return true
}

// prefixUpperBound returns the exclusive end key for iterating over prefix,
// i.e. the smallest key strictly greater than every key having the prefix.
// It returns nil when prefix is empty or all bytes are 0xFF (no upper bound).
func prefixUpperBound(prefix []byte) []byte {
	end := make([]byte, len(prefix))
	copy(end, prefix)
	for i := len(end) - 1; i >= 0; i-- {
		if end[i] != 0xFF {
			end[i]++
			return end[:i+1]
		}
	}
	return nil
}

func intInSlice(a int, list []int) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}

	return false
}

func int64FromBytes(bz []byte) int64 {
	v, _ := binary.Varint(bz)
	return v
}

func int64ToBytes(i int64) []byte {
	buf := make([]byte, binary.MaxVarintLen64)
	n := binary.PutVarint(buf, i)
	return buf[:n]
}

func heightKey(height int64) ([]byte, error) {
	return orderedcode.Append(
		nil,
		types.BlockHeightKey,
		height,
	)
}

func eventKey(compositeKey, typ, eventValue string, height int64) ([]byte, error) {
	return orderedcode.Append(
		nil,
		compositeKey,
		eventValue,
		height,
		typ,
	)
}

func parseValueFromPrimaryKey(key []byte) (string, error) {
	var (
		compositeKey string
		height       int64
	)

	remaining, err := orderedcode.Parse(string(key), &compositeKey, &height)
	if err != nil {
		return "", fmt.Errorf("failed to parse event key: %w", err)
	}

	if len(remaining) != 0 {
		return "", fmt.Errorf("unexpected remainder in key: %s", remaining)
	}

	return strconv.FormatInt(height, 10), nil
}

func parseValueFromEventKey(key []byte) (string, error) {
	var (
		compositeKey, typ, eventValue string
		height                        int64
	)

	remaining, err := orderedcode.Parse(string(key), &compositeKey, &eventValue, &height, &typ)
	if err != nil {
		return "", fmt.Errorf("failed to parse event key: %w", err)
	}

	if len(remaining) != 0 {
		return "", fmt.Errorf("unexpected remainder in key: %s", remaining)
	}

	return eventValue, nil
}

func lookForHeight(conditions []syntax.Condition) (int64, bool) {
	for _, c := range conditions {
		if c.Tag == types.BlockHeightKey && c.Op == syntax.TEq {
			return int64(c.Arg.Number()), true
		}
	}

	return 0, false
}

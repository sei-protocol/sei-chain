package kv

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"

	"github.com/google/orderedcode"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/pubsub/query/syntax"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

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

// eventKeyHeightOrdered builds a height-ordered event key:
// orderedcode(blockHeightOrderedKey, compositeKey, height, eventValue, typ).
// Placing the (real int64) height ahead of the value lets EXISTS-by-tag queries
// scan in height order and early-stop at the limit.
func eventKeyHeightOrdered(compositeKey, typ, eventValue string, height int64) ([]byte, error) {
	return orderedcode.Append(
		nil,
		blockHeightOrderedKey,
		compositeKey,
		height,
		eventValue,
		typ,
	)
}

// prefixHeightOrdered returns the scan prefix orderedcode(blockHeightOrderedKey,
// compositeKey) covering every height-ordered entry for a composite tag, in
// height order.
func prefixHeightOrdered(compositeKey string) ([]byte, error) {
	return orderedcode.Append(nil, blockHeightOrderedKey, compositeKey)
}

// heightOrderedBounds returns the [start, end) key range restricting a
// height-ordered scan of compositeKey to heights [lo, hi]. Because keys are
// orderedcode(blockHeightOrderedKey, compositeKey, height, ...), start seeks to
// the first key at height lo and end is the first key past height hi, so the
// scan visits only in-window entries instead of scanning the whole prefix. When
// hi is unbounded (math.MaxInt64) end is the prefix upper bound, avoiding
// overflow.
func heightOrderedBounds(compositeKey string, lo, hi int64) (start, end []byte, err error) {
	start, err = orderedcode.Append(nil, blockHeightOrderedKey, compositeKey, lo)
	if err != nil {
		return nil, nil, err
	}
	if hi == math.MaxInt64 {
		prefix, err := prefixHeightOrdered(compositeKey)
		if err != nil {
			return nil, nil, err
		}
		return start, indexer.PrefixUpperBound(prefix), nil
	}
	end, err = orderedcode.Append(nil, blockHeightOrderedKey, compositeKey, hi+1)
	if err != nil {
		return nil, nil, err
	}
	return start, end, nil
}

// parseHeightFromHeightOrderedKey extracts the height from a height-ordered
// event key. See eventKeyHeightOrdered for the layout.
func parseHeightFromHeightOrderedKey(key []byte) (int64, error) {
	var (
		ns, compositeKey, eventValue, typ string
		height                            int64
	)

	remaining, err := orderedcode.Parse(string(key), &ns, &compositeKey, &height, &eventValue, &typ)
	if err != nil {
		return 0, fmt.Errorf("failed to parse height-ordered key: %w", err)
	}
	if len(remaining) != 0 {
		return 0, fmt.Errorf("unexpected remainder in key: %s", remaining)
	}

	return height, nil
}

// watermarkKey is the reserved key holding the lowest height covered by the
// height-ordered index on this node.
func watermarkKey() ([]byte, error) {
	return orderedcode.Append(nil, blockWatermarkKey)
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

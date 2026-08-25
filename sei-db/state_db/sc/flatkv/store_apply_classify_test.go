package flatkv

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// classifyTestPool returns a pool for classifying, closed when the test ends.
func classifyTestPool(t *testing.T) threading.Pool {
	t.Helper()
	pool := threading.NewElasticPool("classify-test", 4)
	t.Cleanup(pool.Close)
	return pool
}

// A unit size below one classifies serially. It must not be planned into units of no pairs, which would
// never consume the block and so would hang whatever called it — a config value that stops a node dead.
func TestClassifyUnitSizeBelowOneFallsBackToSerial(t *testing.T) {
	pool := classifyTestPool(t)
	changeSets := fatChangeSets(classifyPairs(64))

	var hints [keys.EVMKeyKindCount]int
	serial, err := classifyAndPrefix(changeSets, hints)
	require.NoError(t, err)

	for _, unitSize := range []int{0, -1} {
		got, err := classifyAndPrefixParallel(changeSets, hints, pool, unitSize)
		require.NoError(t, err, "unit size %d", unitSize)
		requireSameClassification(t, serial, got, fmt.Sprintf("unit size %d", unitSize))
	}
}

// Classifying in parallel must produce exactly what classifying serially produces, at any unit size —
// including sizes that do not divide the block evenly, and sizes larger than the block.
func TestClassifyParallelMatchesSerial(t *testing.T) {
	pool := classifyTestPool(t)

	for _, pairCount := range []int{1, 2, 7, 64, 1000} {
		changeSets := fatChangeSets(classifyPairs(pairCount))

		var hints [keys.EVMKeyKindCount]int
		serial, err := classifyAndPrefix(changeSets, hints)
		require.NoError(t, err)

		for _, unitSize := range []int{1, 3, 8, 64, 4096} {
			got, err := classifyAndPrefixParallel(changeSets, hints, pool, unitSize)
			require.NoError(t, err)
			requireSameClassification(t, serial, got,
				fmt.Sprintf("%d pairs at unit size %d", pairCount, unitSize))
		}
	}
}

// The order pairs arrived in has to survive being classified in pieces. Downstream resolves a key written
// more than once in a block by taking the last one it sees, so a repeated key whose writes land in
// different units must still come out oldest first.
func TestClassifyParallelKeepsBlockOrderAcrossUnits(t *testing.T) {
	pool := classifyTestPool(t)

	// One key written at both ends of the block, so its two writes cannot share a unit.
	const pairCount = 64
	pairs := classifyPairs(pairCount)
	repeated := pairs[0].Key
	pairs[len(pairs)-1] = &proto.KVPair{Key: repeated, Value: []byte("last")}
	pairs[0] = &proto.KVPair{Key: repeated, Value: []byte("first")}

	var hints [keys.EVMKeyKindCount]int
	got, err := classifyAndPrefixParallel(fatChangeSets(pairs), hints, pool, 8)
	require.NoError(t, err)

	kind, _ := keys.ParseEVMKey(repeated)
	var seen [][]byte
	for _, change := range got[kind] {
		if string(change.value) == "first" || string(change.value) == "last" {
			seen = append(seen, change.value)
		}
	}
	require.Len(t, seen, 2, "both writes to the repeated key must be kept")
	require.Equal(t, "first", string(seen[0]), "the earlier write must come first")
	require.Equal(t, "last", string(seen[1]), "the later write must come last, so it wins downstream")
}

// A malformed block is rejected whichever unit holds the offending pair.
func TestClassifyParallelRejectsEmptyKey(t *testing.T) {
	pool := classifyTestPool(t)

	pairs := classifyPairs(64)
	pairs[40] = &proto.KVPair{Key: nil, Value: []byte("x")}

	var hints [keys.EVMKeyKindCount]int
	_, err := classifyAndPrefixParallel(fatChangeSets(pairs), hints, pool, 8)
	require.ErrorContains(t, err, "empty key")
}

// classifyPairs returns n changeset pairs spanning several key kinds, so classification has to route
// them to different buckets rather than filling one.
func classifyPairs(n int) []*proto.KVPair {
	pairs := make([]*proto.KVPair, 0, n)
	for i := 0; i < n; i++ {
		switch i % 3 {
		case 0:
			pairs = append(pairs, &proto.KVPair{
				Key:   keys.BuildEVMKey(keys.EVMKeyNonce, benchAddr(i)),
				Value: []byte(fmt.Sprintf("nonce-%d", i)),
			})
		case 1:
			pairs = append(pairs, &proto.KVPair{
				Key:   keys.BuildEVMKey(keys.EVMKeyCodeHash, benchAddr(i)),
				Value: []byte(fmt.Sprintf("codehash-%d", i)),
			})
		default:
			pairs = append(pairs, &proto.KVPair{
				Key:   keys.BuildEVMKey(keys.EVMKeyStorage, append(benchAddr(i), benchSlot(i)...)),
				Value: []byte(fmt.Sprintf("storage-%d", i)),
			})
		}
	}
	return pairs
}

// requireSameClassification asserts two classifications hold the same changes, in the same order, in
// every bucket.
func requireSameClassification(t *testing.T, want classifiedChanges, got classifiedChanges, context string) {
	t.Helper()
	for kind := range want {
		require.Len(t, got[kind], len(want[kind]), "%s: bucket %d length", context, kind)
		for i := range want[kind] {
			require.Equal(t, want[kind][i].key, got[kind][i].key,
				"%s: bucket %d entry %d key", context, kind, i)
			require.Equal(t, want[kind][i].value, got[kind][i].value,
				"%s: bucket %d entry %d value", context, kind, i)
		}
	}
}

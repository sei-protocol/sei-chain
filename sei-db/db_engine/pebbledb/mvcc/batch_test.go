package mvcc

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"slices"
	"testing"

	"github.com/cockroachdb/pebble/v2/batchrepr"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

func TestStorePrefixFormat(t *testing.T) {
	require.Equal(t, []byte(fmt.Sprintf(StorePrefixTpl, "evm")), storePrefix("evm"))
	require.Equal(t, []byte("s/k:evm/key"), prependStoreKey("evm", []byte("key")))
	require.Equal(t, []byte("key"), prependStoreKey("", []byte("key")))
}

func TestEncodeMVCCStoreKey(t *testing.T) {
	key := []byte("abc")
	for _, descending := range []bool{true, false} {
		got := encodeMVCCStoreKey("store1", key, 42, descending)
		user, ver, ok := SplitMVCCKey(got)
		require.True(t, ok)
		require.Equal(t, []byte("s/k:store1/abc"), user)

		var v int64
		var err error
		if descending {
			v, err = decodeUint64Descending(ver)
		} else {
			v, err = decodeUint64Ascending(ver)
		}
		require.NoError(t, err)
		require.Equal(t, int64(42), v)
	}

	got := encodeMVCCStoreKey("", key, 42, true)
	user, _, ok := SplitMVCCKey(got)
	require.True(t, ok)
	require.Equal(t, key, user)

	require.Equal(t, []byte("s/k:s/k\x00"), encodeMVCCStoreKey("s", []byte("k"), 0, true))
}

func TestBatchGrow(t *testing.T) {
	b, err := NewBatch(nil, 1, true, "test")
	require.NoError(t, err)
	require.Equal(t, 16, cap(b.ops))

	b.grow(1000)
	require.Equal(t, 1000, cap(b.ops))

	b.grow(10)
	require.Equal(t, 1000, cap(b.ops))

	b.grow(0)
	require.Equal(t, 1000, cap(b.ops))
}

func TestPebbleBatchBufSize(t *testing.T) {
	require.Equal(t, batchrepr.HeaderLen, pebbleBatchBufSize(nil, false))

	key := []byte("key")
	val := []byte("value")
	setOp := batchOp{storeKey: "s", key: key, value: val, version: 1}
	sets := pebbleBatchBufSize([]batchOp{setOp}, false)
	require.Equal(t, batchrepr.HeaderLen+1+binary.MaxVarintLen32+mvccStoreKeyLen("s", key, 1)+binary.MaxVarintLen32+mvccValueLen(val, 0), sets)

	delOp := batchOp{storeKey: "s", key: key, version: 1, delete: true}
	deletes := pebbleBatchBufSize([]batchOp{delOp}, false)
	require.Equal(t, batchrepr.HeaderLen+1+binary.MaxVarintLen32+mvccStoreKeyLen("s", key, 1), deletes)

	withMeta := pebbleBatchBufSize(nil, true)
	require.Equal(t, batchrepr.HeaderLen+1+binary.MaxVarintLen32+len(latestVersionKey)+binary.MaxVarintLen32+VersionSize, withMeta)
}

func TestEncodeMVCCStoreKeyIntoMatchesEncode(t *testing.T) {
	key := []byte("abc")
	for _, descending := range []bool{true, false} {
		want := encodeMVCCStoreKey("store1", key, 42, descending)
		got := make([]byte, mvccStoreKeyLen("store1", key, 42))
		encodeMVCCStoreKeyInto(got, "store1", key, 42, descending)
		require.Equal(t, want, got)

		want = encodeMVCCStoreKey("", key, 0, descending)
		got = make([]byte, mvccStoreKeyLen("", key, 0))
		encodeMVCCStoreKeyInto(got, "", key, 0, descending)
		require.Equal(t, want, got)
	}
}

func TestEncodeMVCCValueIntoMatchesEncode(t *testing.T) {
	val := []byte("value")
	for _, descending := range []bool{true, false} {
		want := MVCCEncode(val, 0, descending)
		got := make([]byte, mvccValueLen(val, 0))
		encodeMVCCValueInto(got, val, 0, descending)
		require.Equal(t, want, got)

		want = MVCCEncode([]byte(tombstoneVal), 7, descending)
		got = make([]byte, mvccValueLen([]byte(tombstoneVal), 7))
		encodeMVCCValueInto(got, []byte(tombstoneVal), 7, descending)
		require.Equal(t, want, got)
	}
}

func TestSortBatchOpsMatchesEncodedCompare(t *testing.T) {
	for _, descending := range []bool{true, false} {
		ops := []batchOp{
			{storeKey: "b", key: []byte("z"), version: 1},
			{storeKey: "a", key: []byte("m"), version: 3},
			{storeKey: "a", key: []byte("m"), version: 1},
			{storeKey: "a", key: []byte("a"), version: 2},
		}
		sortBatchOps(ops, descending)

		encoded := make([][]byte, len(ops))
		for i, op := range ops {
			encoded[i] = encodeMVCCStoreKey(op.storeKey, op.key, op.version, descending)
		}
		for i := 1; i < len(encoded); i++ {
			require.LessOrEqual(t, MVCCComparer.Compare(encoded[i-1], encoded[i]), 0)
		}
	}
}

// SortChangesetPairs is only worth calling if it produces the order
// sortBatchOps checks for, so the two are pinned together here.
func TestSortChangesetPairsMatchesSortBatchOps(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	pairs := make([]*proto.KVPair, 0, 500)
	for i := 0; i < 500; i++ {
		key := make([]byte, 1+rng.Intn(40))
		rng.Read(key)
		pairs = append(pairs, &proto.KVPair{Key: key, Value: []byte("v")})
	}
	SortChangesetPairs(pairs)

	for _, descending := range []bool{true, false} {
		ops := make([]batchOp, 0, len(pairs))
		for _, p := range pairs {
			ops = append(ops, batchOp{storeKey: "evm", key: p.Key, value: p.Value, version: 7})
		}
		require.True(t, slices.IsSortedFunc(ops, func(a, b batchOp) int {
			return batchOpOrder(a, b, descending)
		}), "descending=%v: presorted pairs are not in sortBatchOps order", descending)
	}
}

func TestSortBatchOpsLeavesSortedInputUntouched(t *testing.T) {
	ops := []batchOp{
		{storeKey: "a", key: []byte("a"), version: 1},
		{storeKey: "a", key: []byte("b"), version: 1},
		{storeKey: "b", key: []byte("a"), version: 1},
	}
	want := slices.Clone(ops)
	sortBatchOps(ops, true)
	require.Equal(t, want, ops)
}

func TestBatchOpOrderVersionDirection(t *testing.T) {
	newer := batchOp{storeKey: "s", key: []byte("k"), version: 9}
	older := batchOp{storeKey: "s", key: []byte("k"), version: 2}

	// Descending mode stores newer versions first, ascending mode last.
	require.Negative(t, batchOpOrder(newer, older, true))
	require.Positive(t, batchOpOrder(older, newer, true))
	require.Negative(t, batchOpOrder(older, newer, false))
	require.Positive(t, batchOpOrder(newer, older, false))
	require.Zero(t, batchOpOrder(newer, newer, true))
}

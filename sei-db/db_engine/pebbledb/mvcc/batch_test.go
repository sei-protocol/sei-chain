package mvcc

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"slices"
	"testing"

	"github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/vfs"
	"github.com/stretchr/testify/require"
)

// kvCount is how many pairs one BenchmarkBatchWrite iteration writes, so its
// reported allocs/op and B/op are per batch of this many keys.
const kvCount = 10_000

type kv struct{ key, value []byte }

// evmKVs builds EVM-shaped pairs: a 0x03-tagged 20-byte address plus 32-byte
// slot for the key, and 32 bytes of value. Key and value lengths drive the
// encoder, so this is the shape the EVM state store actually writes.
func evmKVs(n int, seed int64) []kv {
	rng := rand.New(rand.NewSource(seed))
	out := make([]kv, n)
	for i := range out {
		k := make([]byte, 53)
		k[0] = 3
		rng.Read(k[1:])
		v := make([]byte, 32)
		rng.Read(v)
		out[i] = kv{k, v}
	}
	return out
}

func newMemDB(tb testing.TB) *pebble.DB {
	tb.Helper()
	db, err := pebble.Open("test", &pebble.Options{FS: vfs.NewMem()})
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() {
		if err := db.Close(); err != nil {
			tb.Error(err)
		}
	})
	return db
}

// TestStorePrefix pins the two prefix builders against literal bytes, since
// both compose "s/k:<store>/" by hand rather than through a format string.
func TestStorePrefix(t *testing.T) {
	require.Equal(t, []byte("s/k:evm/"), storePrefix("evm"))
	require.Equal(t, []byte("s/k:evm/key"), prependStoreKey("evm", []byte("key")))
	require.Equal(t, []byte("key"), prependStoreKey("", []byte("key")))
}

// TestEncodeMVCC pins the encoder from both ends.
func TestEncodeMVCC(t *testing.T) {
	tests := []struct {
		name     string
		storeKey string
		body     []byte
		version  int64
		wantUser []byte
	}{
		{"storePrefixedKey", "store1", []byte("abc"), 42, []byte("s/k:store1/abc")},
		{"bareKey", "", []byte("abc"), 42, []byte("abc")},
		{"versionZeroWritesNoVersion", "s", []byte("k"), 0, []byte("s/k:s/k")},
		{"value", "", []byte("value"), 0, []byte("value")},
		{"tombstonedValue", "", []byte(tombstoneVal), 7, []byte(tombstoneVal)},
	}

	for _, tt := range tests {
		for _, descending := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/descending=%v", tt.name, descending), func(t *testing.T) {
				want := MVCCEncode(prependStoreKey(tt.storeKey, tt.body), tt.version, descending)

				size := mvccEncodedLen(tt.storeKey, tt.body, tt.version)
				require.Len(t, want, size, "mvccEncodedLen must be the exact encoded length")
				got := make([]byte, size)
				encodeMVCCInto(got, tt.storeKey, tt.body, tt.version, descending)
				require.Equal(t, want, got)

				user, ver, ok := SplitMVCCKey(got)
				require.True(t, ok)
				require.Equal(t, tt.wantUser, user)

				if tt.version == 0 {
					require.Empty(t, ver, "version 0 is encoded as a bare sentinel")
					return
				}
				decode := decodeUint64Descending
				if !descending {
					decode = decodeUint64Ascending
				}
				v, err := decode(ver)
				require.NoError(t, err)
				require.Equal(t, tt.version, v)
			})
		}
	}
}

// TestSortBatchOps checks the ordering writeBatchOps relies on: ops are sorted
// on their decomposed fields, which has to agree with comparing the encoded
// keys Pebble will see, in both version directions.
func TestSortBatchOps(t *testing.T) {
	for _, descending := range []bool{true, false} {
		t.Run(fmt.Sprintf("descending=%v", descending), func(t *testing.T) {
			ops := []batchOp{
				{storeKey: "b", key: []byte("z"), version: 1},
				{storeKey: "a", key: []byte("m"), version: 3},
				{storeKey: "a", key: []byte("m"), version: 1},
				{storeKey: "a", key: []byte("a"), version: 2},
			}
			sortBatchOps(ops, descending)

			for i := 1; i < len(ops); i++ {
				prev := MVCCEncode(prependStoreKey(ops[i-1].storeKey, ops[i-1].key), ops[i-1].version, descending)
				curr := MVCCEncode(prependStoreKey(ops[i].storeKey, ops[i].key), ops[i].version, descending)
				require.Negative(t, MVCCComparer.Compare(prev, curr), "ops[%d] must sort before ops[%d]", i-1, i)
			}

			sorted := slices.Clone(ops)
			sortBatchOps(ops, descending)
			require.Equal(t, sorted, ops, "sorting an already sorted batch must not reorder it")
		})
	}
}

// TestBatchWriteRoundTrip is the end-to-end check on the deferred write path:
// what Set, Delete and HardDelete queued has to reach Pebble under the same
// bytes the allocating encoder would have produced.
func TestBatchWriteRoundTrip(t *testing.T) {
	db := newMemDB(t)

	// Queued from buffers the caller then reuses, which is what a changeset does:
	// Set has to have taken its own copy.
	key, value := []byte("live"), []byte("v1")

	b, err := NewBatch(db, 7, 3, true, "test")
	require.NoError(t, err)
	require.NoError(t, b.Set("evm", key, value))
	require.NoError(t, b.Delete("evm", []byte("gone")))
	require.NoError(t, b.HardDelete("evm", []byte("never")))
	require.Equal(t, 3, b.Size())

	copy(key, "DEAD")
	copy(value, "XX")
	require.NoError(t, b.Write())

	get := func(key []byte) []byte {
		t.Helper()
		v, closer, err := db.Get(key)
		require.NoError(t, err)
		out := slices.Clone(v)
		require.NoError(t, closer.Close())
		return out
	}
	encoded := func(key []byte) []byte {
		return MVCCEncode(prependStoreKey("evm", key), 7, true)
	}

	require.Equal(t, MVCCEncode([]byte("v1"), 0, true), get(encoded([]byte("live"))))
	require.Equal(t, MVCCEncode([]byte(tombstoneVal), 7, true), get(encoded([]byte("gone"))))

	_, _, err = db.Get(encoded([]byte("never")))
	require.ErrorIs(t, err, pebble.ErrNotFound, "HardDelete must leave no record")

	// The latest-version marker is stamped as a plain key, not an MVCC one.
	var version [VersionSize]byte
	binary.LittleEndian.PutUint64(version[:], 7)
	require.Equal(t, version[:], get([]byte(latestVersionKey)))
}

// batchSink keeps the constructed batch alive so escape analysis cannot
// optimise the constructor away.
var batchSink *Batch

// TestBatchAllocs pins per-call what BenchmarkBatchWrite reports in aggregate,
// so this catches performance regressions.
func TestBatchAllocs(t *testing.T) {
	t.Run("NewBatch", func(t *testing.T) {
		allocs := testing.AllocsPerRun(1000, func() {
			b, err := NewBatch(nil, 1, 0, true, "test")
			require.NoError(t, err)
			batchSink = b
		})
		require.LessOrEqual(t, allocs, 1.0, "an unsized batch is the Batch struct and nothing else")

		allocs = testing.AllocsPerRun(1000, func() {
			b, err := NewBatch(nil, 1, 64, true, "test")
			require.NoError(t, err)
			batchSink = b
		})
		require.LessOrEqual(t, allocs, 2.0, "a sized batch adds the ops reservation and nothing else")
	})

	t.Run("Set", func(t *testing.T) {
		b, err := NewBatch(nil, 1, 8, true, "test")
		require.NoError(t, err)

		key, val := []byte("key"), []byte("value")
		allocs := testing.AllocsPerRun(1000, func() {
			b.Reset()
			require.NoError(t, b.Set("store", key, val))
		})
		require.LessOrEqual(t, allocs, 2.0, "want the cloned key and value and nothing else")
	})

	t.Run("sizedBatchNeverRegrows", func(t *testing.T) {
		const size = 1000
		b, err := NewBatch(nil, 1, size, true, "test")
		require.NoError(t, err)
		reserved := cap(b.ops)
		require.GreaterOrEqual(t, reserved, size)

		for i := 0; i < size; i++ {
			require.NoError(t, b.Set("store", []byte("key"), []byte("value")))
		}
		require.Equal(t, reserved, cap(b.ops), "a batch sized for %d must absorb %d appends without reallocating", size, size)
	})
}

// BenchmarkBatchWrite measures the whole queue-then-write path over one batch of
// kvCount EVM-shaped pairs. allocs/op and B/op are the numbers that matter: the
// deferred encoder keeps only the caller's key and value per pair, where
// composing the encoded key and value on the heap first did not.
func BenchmarkBatchWrite(b *testing.B) {
	pairs := evmKVs(kvCount, 1)
	db := newMemDB(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		batch, err := NewBatch(db, 1, len(pairs), true, "bench")
		if err != nil {
			b.Fatal(err)
		}
		for _, p := range pairs {
			if err := batch.Set("evm", p.key, p.value); err != nil {
				b.Fatal(err)
			}
		}
		if err := batch.Write(); err != nil {
			b.Fatal(err)
		}
	}
}

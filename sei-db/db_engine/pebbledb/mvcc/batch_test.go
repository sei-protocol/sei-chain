package mvcc

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"slices"
	"testing"

	"github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/vfs"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
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

// TestChangesetBatchSize checks that a batch sized by changesetBatchSize takes
// every pair of its changesets without growing its buffer, and that an unsized
// one does grow, so the check is not vacuous.
func TestChangesetBatchSize(t *testing.T) {
	const version = 9
	var pairs []*proto.KVPair
	for i, p := range evmKVs(1_000, 3) {
		value := p.value
		if i%4 == 0 {
			value = nil // a delete, written as a tombstone
		}
		pairs = append(pairs, &proto.KVPair{Key: p.key, Value: value})
	}
	changesets := []*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: pairs[:100]}},
		{Name: "evm", Changeset: proto.ChangeSet{Pairs: pairs[100:]}},
	}

	fillAllocs := func(t *testing.T, bufSize int) float64 {
		t.Helper()
		db := newMemDB(t)
		const runs = 5
		batches := make([]*Batch, runs+1) // AllocsPerRun makes one warm-up call
		for i := range batches {
			b, err := NewBatch(db, version, bufSize, false, "test")
			require.NoError(t, err)
			batches[i] = b
			t.Cleanup(func() { require.NoError(t, b.Close()) })
		}
		next := 0
		return testing.AllocsPerRun(runs, func() {
			b := batches[next]
			next++
			for _, cs := range changesets {
				for _, pair := range cs.Changeset.Pairs {
					if pair.Value == nil {
						require.NoError(t, b.Delete(cs.Name, pair.Key))
					} else {
						require.NoError(t, b.Set(cs.Name, pair.Key, pair.Value))
					}
				}
			}
			var versionBz [VersionSize]byte
			require.NoError(t, b.pb.Set([]byte(latestVersionKey), versionBz[:], nil))
		})
	}

	require.Zero(t, fillAllocs(t, changesetBatchSize(changesets, version)))
	require.Positive(t, fillAllocs(t, 0))
}

// TestBatchSingleUse checks that a written or closed Batch refuses further use
// rather than touching the released pebble.Batch.
func TestBatchSingleUse(t *testing.T) {
	assertReleased := func(t *testing.T, b *Batch) {
		t.Helper()
		require.Zero(t, b.Size())
		require.ErrorIs(t, b.Set("evm", []byte("k"), []byte("v")), errBatchClosed)
		require.ErrorIs(t, b.Delete("evm", []byte("k")), errBatchClosed)
		require.ErrorIs(t, b.HardDelete("evm", []byte("k")), errBatchClosed)
		require.ErrorIs(t, b.Write(), errBatchClosed)
		require.NoError(t, b.Close(), "Close must be idempotent")
	}

	t.Run("after Write", func(t *testing.T) {
		db := newMemDB(t)
		b, err := NewBatch(db, 1, 0, false, "test")
		require.NoError(t, err)
		require.NoError(t, b.Set("evm", []byte("k"), []byte("v")))
		require.NoError(t, b.Write())
		assertReleased(t, b)
	})

	t.Run("after Close", func(t *testing.T) {
		db := newMemDB(t)
		b, err := NewBatch(db, 1, 0, false, "test")
		require.NoError(t, err)
		require.NoError(t, b.Set("evm", []byte("k"), []byte("v")))
		require.NoError(t, b.Close())
		assertReleased(t, b)

		_, _, err = db.Get(MVCCEncode(prependStoreKey("evm", []byte("k")), 1, false))
		require.ErrorIs(t, err, pebble.ErrNotFound, "Close must not commit")
	})
}

// TestApplyChangesetSyncLeavesInputUnchanged checks that applying a changeset
// does not reorder or replace its pairs, which the caller shares with the
// commit store while the state store applies them in the background.
func TestApplyChangesetSyncLeavesInputUnchanged(t *testing.T) {
	db := newTestDB(t, false)
	pairs := []*proto.KVPair{
		{Key: []byte("z"), Value: []byte("1")},
		{Key: []byte("a"), Value: []byte("2")},
		{Key: []byte("m")},
	}
	before := slices.Clone(pairs)

	require.NoError(t, db.ApplyChangesetSync(1, []*proto.NamedChangeSet{
		{Name: "store", Changeset: proto.ChangeSet{Pairs: pairs}},
	}))
	require.Equal(t, before, pairs)
}

// TestBatchWriteRoundTrip is the end-to-end check on the deferred write path:
// what Set, Delete and HardDelete queued has to reach Pebble under the same
// bytes the allocating encoder would have produced.
func TestBatchWriteRoundTrip(t *testing.T) {
	db := newMemDB(t)

	// Queued from buffers the caller then reuses, which is what a changeset does:
	// Set has to have taken its own copy.
	key, value := []byte("live"), []byte("v1")

	b, err := NewBatch(db, 7, 0, true, "test")
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

// TestBatchAllocs pins per-call what BenchmarkBatchWrite reports in aggregate:
// Set encodes straight into the underlying pebble.Batch and allocates nothing
// of its own.
func TestBatchAllocs(t *testing.T) {
	t.Run("Set", func(t *testing.T) {
		db := newMemDB(t)
		b, err := NewBatch(db, 1, 0, true, "test")
		require.NoError(t, err)

		key, val := []byte("key"), []byte("value")
		allocs := testing.AllocsPerRun(1000, func() {
			b.Reset()
			require.NoError(t, b.Set("store", key, val))
		})
		require.LessOrEqual(t, allocs, 0.0, "Set must encode into the pebble batch's own buffer with no allocation of its own")
	})
}

// BenchmarkBatchWrite measures the whole queue-then-write path over one batch of
// kvCount EVM-shaped pairs. allocs/op and B/op are the numbers that matter: Set
// encodes straight into the pebble.Batch, so the only allocations left are
// Pebble's own (buffer growth, memtable arena), not this package's.
func BenchmarkBatchWrite(b *testing.B) {
	pairs := evmKVs(kvCount, 1)
	db := newMemDB(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		batch, err := NewBatch(db, 1, 0, true, "bench")
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

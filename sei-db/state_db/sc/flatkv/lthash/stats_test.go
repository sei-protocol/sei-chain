package lthash

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
)

func TestModuleStatsMarshalRoundTrip(t *testing.T) {
	cases := []ModuleStats{
		{},
		{KeyCount: 1, Bytes: 42},
		{KeyCount: 1 << 40, Bytes: 1 << 50},
	}
	for _, want := range cases {
		b := want.Marshal()
		require.Len(t, b, moduleStatsEncodedLen)
		got, err := UnmarshalModuleStats(b)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
}

func TestUnmarshalModuleStatsBadLength(t *testing.T) {
	for _, n := range []int{0, 8, 15, 17, 32} {
		_, err := UnmarshalModuleStats(make([]byte, n))
		require.Error(t, err, "length %d should be rejected", n)
	}
}

func TestModuleStatsAdd(t *testing.T) {
	a := ModuleStats{KeyCount: 3, Bytes: 100}
	b := ModuleStats{KeyCount: -1, Bytes: -30}
	require.Equal(t, ModuleStats{KeyCount: 2, Bytes: 70}, a.Add(b))
	// Add must not mutate the receiver.
	require.Equal(t, ModuleStats{KeyCount: 3, Bytes: 100}, a)
}

// TestFoldChunkStats locks down the per-key accounting rule: add increments the
// count and adds key+value bytes, update keeps the count and adjusts by the
// value-size delta, and delete decrements the count and subtracts key+old-value
// bytes. Delete-of-absent is a no-op.
func TestFoldChunkStats(t *testing.T) {
	key := []byte("some/physical/key")

	tests := []struct {
		name     string
		pair     KeyMutation
		wantKeys int64
		wantByte int64
	}{
		{
			name:     "add",
			pair:     KeyMutation{Key: key, Value: []byte("newvalue")},
			wantKeys: 1,
			wantByte: int64(len(key)) + int64(len("newvalue")),
		},
		{
			name:     "update grows",
			pair:     KeyMutation{Key: key, Value: []byte("longer-value"), LastValue: []byte("short")},
			wantKeys: 0,
			wantByte: int64(len("longer-value")) - int64(len("short")),
		},
		{
			name:     "update shrinks",
			pair:     KeyMutation{Key: key, Value: []byte("v"), LastValue: []byte("wasbigger")},
			wantKeys: 0,
			wantByte: int64(len("v")) - int64(len("wasbigger")),
		},
		{
			name:     "delete",
			pair:     KeyMutation{Key: key, LastValue: []byte("oldvalue"), Delete: true},
			wantKeys: -1,
			wantByte: -(int64(len(key)) + int64(len("oldvalue"))),
		},
		{
			name:     "delete absent is no-op",
			pair:     KeyMutation{Key: key, Delete: true},
			wantKeys: 0,
			wantByte: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := hashChunk([]KeyMutation{tc.pair})
			require.Equal(t, tc.wantKeys, d.KeyCount)
			require.Equal(t, tc.wantByte, d.Bytes)
		})
	}
}

// TestComputeModuleHashInfosStatsParallel exercises the pooled path (> chunk size)
// and checks the aggregated per-module stats equal a straightforward serial
// tally, proving the chunk-and-merge does not lose or double-count.
func TestComputeModuleHashInfosStatsParallel(t *testing.T) {
	const dbName = "d"
	moduleOf := func([]byte) (string, error) { return "m", nil }
	pool := threading.NewFixedPool("test", 4, 4)
	defer pool.Close()
	cfg := DefaultConfig()
	n := int(cfg.ChunkSize)*3 + 7 // spans several chunks, not a chunk multiple
	mutations := make([]KeyMutation, n)
	var wantKeys, wantBytes int64
	for i := range mutations {
		key := []byte(fmt.Sprintf("m/key-%05d", i))
		val := []byte(fmt.Sprintf("value-%d", i))
		mutations[i] = KeyMutation{Key: key, Value: val}
		wantKeys++
		wantBytes += int64(len(key)) + int64(len(val))
	}

	deltas, err := ComputeModuleHashInfos(
		pool, moduleOf, []DatabaseMutations{{DBName: dbName, Mutations: mutations}}, cfg.ChunkSize)
	require.NoError(t, err)
	require.Len(t, deltas, 1)

	d := deltas[ModuleKey{DBName: dbName, Module: "m"}]
	require.NotNil(t, d)
	require.Equal(t, wantKeys, d.KeyCount)
	require.Equal(t, wantBytes, d.Bytes)
}

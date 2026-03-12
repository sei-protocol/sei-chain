package pebblecache

import (
	"fmt"
	"math"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// --- NewShardManager ---

func TestNewShardManagerValidPowersOfTwo(t *testing.T) {
	for exp := 0; exp < 20; exp++ {
		n := uint64(1) << exp
		sm, err := newShardManager(n)
		require.NoError(t, err, "numShards=%d", n)
		require.NotNil(t, sm, "numShards=%d", n)
	}
}

func TestNewShardManagerZeroReturnsError(t *testing.T) {
	sm, err := newShardManager(0)
	require.ErrorIs(t, err, ErrNumShardsNotPowerOfTwo)
	require.Nil(t, sm)
}

func TestNewShardManagerNonPowersOfTwoReturnError(t *testing.T) {
	bad := []uint64{3, 5, 6, 7, 9, 10, 12, 15, 17, 100, 255, 1023}
	for _, n := range bad {
		sm, err := newShardManager(n)
		require.ErrorIs(t, err, ErrNumShardsNotPowerOfTwo, "numShards=%d", n)
		require.Nil(t, sm, "numShards=%d", n)
	}
}

func TestNewShardManagerMaxUint64ReturnsError(t *testing.T) {
	sm, err := newShardManager(math.MaxUint64)
	require.ErrorIs(t, err, ErrNumShardsNotPowerOfTwo)
	require.Nil(t, sm)
}

func TestNewShardManagerLargePowerOfTwo(t *testing.T) {
	n := uint64(1) << 40
	sm, err := newShardManager(n)
	require.NoError(t, err)
	require.NotNil(t, sm)
}

// --- Shard: basic behaviour ---

func TestShardReturnsBoundedIndex(t *testing.T) {
	for _, numShards := range []uint64{1, 2, 4, 16, 256, 1024} {
		sm, err := newShardManager(numShards)
		require.NoError(t, err)

		for i := 0; i < 500; i++ {
			key := []byte(fmt.Sprintf("key-%d", i))
			idx := sm.Shard(key)
			require.Less(t, idx, numShards, "numShards=%d key=%s", numShards, key)
		}
	}
}

func TestShardDeterministic(t *testing.T) {
	sm, err := newShardManager(16)
	require.NoError(t, err)

	key := []byte("deterministic-test-key")
	first := sm.Shard(key)
	for i := 0; i < 100; i++ {
		require.Equal(t, first, sm.Shard(key))
	}
}

func TestShardSingleShardAlwaysReturnsZero(t *testing.T) {
	sm, err := newShardManager(1)
	require.NoError(t, err)

	keys := [][]byte{
		{},
		{0x00},
		{0xFF},
		[]byte("anything"),
		[]byte("another key entirely"),
	}
	for _, k := range keys {
		require.Equal(t, uint64(0), sm.Shard(k), "key=%q", k)
	}
}

func TestShardEmptyKey(t *testing.T) {
	sm, err := newShardManager(8)
	require.NoError(t, err)

	idx := sm.Shard([]byte{})
	require.Less(t, idx, uint64(8))

	// Deterministic
	require.Equal(t, idx, sm.Shard([]byte{}))
}

func TestShardNilKey(t *testing.T) {
	sm, err := newShardManager(4)
	require.NoError(t, err)

	idx := sm.Shard(nil)
	require.Less(t, idx, uint64(4))
	require.Equal(t, idx, sm.Shard(nil))
}

func TestShardBinaryKeys(t *testing.T) {
	sm, err := newShardManager(16)
	require.NoError(t, err)

	k1 := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01}
	k2 := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02}

	idx1 := sm.Shard(k1)
	idx2 := sm.Shard(k2)
	require.Less(t, idx1, uint64(16))
	require.Less(t, idx2, uint64(16))
}

func TestShardCallerMutationDoesNotAffectFutureResults(t *testing.T) {
	sm, err := newShardManager(16)
	require.NoError(t, err)

	key := []byte("mutable")
	first := sm.Shard(key)

	key[0] = 'X'
	second := sm.Shard([]byte("mutable"))
	require.Equal(t, first, second)
}

// --- Distribution ---

func TestShardDistribution(t *testing.T) {
	const numShards = 16
	const numKeys = 10_000
	sm, err := newShardManager(numShards)
	require.NoError(t, err)

	counts := make([]int, numShards)
	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("addr-%06d", i))
		counts[sm.Shard(key)]++
	}

	expected := float64(numKeys) / float64(numShards)
	for shard, count := range counts {
		ratio := float64(count) / expected
		require.Greater(t, ratio, 0.5, "shard %d is severely underrepresented (%d)", shard, count)
		require.Less(t, ratio, 1.5, "shard %d is severely overrepresented (%d)", shard, count)
	}
}

// --- Distinct managers ---

func TestDifferentManagersHaveDifferentSeeds(t *testing.T) {
	sm1, err := newShardManager(256)
	require.NoError(t, err)
	sm2, err := newShardManager(256)
	require.NoError(t, err)

	// With distinct random seeds, at least some keys should hash differently.
	diffCount := 0
	for i := 0; i < 200; i++ {
		key := []byte(fmt.Sprintf("seed-test-%d", i))
		if sm1.Shard(key) != sm2.Shard(key) {
			diffCount++
		}
	}
	require.Greater(t, diffCount, 0, "two managers with independent seeds should differ on at least one key")
}

// --- Concurrency ---

func TestShardConcurrentAccess(t *testing.T) {
	sm, err := newShardManager(64)
	require.NoError(t, err)

	const goroutines = 32
	const iters = 1000

	key := []byte("concurrent-key")
	expected := sm.Shard(key)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				got := sm.Shard(key)
				if got != expected {
					t.Errorf("concurrent Shard returned %d, want %d", got, expected)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestShardConcurrentDifferentKeys(t *testing.T) {
	sm, err := newShardManager(32)
	require.NoError(t, err)

	const goroutines = 16
	const keysPerGoroutine = 500

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		g := g
		go func() {
			defer wg.Done()
			for i := 0; i < keysPerGoroutine; i++ {
				key := []byte(fmt.Sprintf("g%d-k%d", g, i))
				idx := sm.Shard(key)
				if idx >= 32 {
					t.Errorf("Shard(%q) = %d, want < 32", key, idx)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// --- Mask correctness ---

func TestShardMaskMatchesNumShards(t *testing.T) {
	for exp := 0; exp < 16; exp++ {
		numShards := uint64(1) << exp
		sm, err := newShardManager(numShards)
		require.NoError(t, err)
		require.Equal(t, numShards-1, sm.mask, "numShards=%d", numShards)
	}
}

// --- 20-byte ETH-style addresses ---

func TestShardWith20ByteAddresses(t *testing.T) {
	sm, err := newShardManager(16)
	require.NoError(t, err)

	addr := make([]byte, 20)
	for i := 0; i < 20; i++ {
		addr[i] = byte(i + 1)
	}

	idx := sm.Shard(addr)
	require.Less(t, idx, uint64(16))
	require.Equal(t, idx, sm.Shard(addr))
}

func TestShardSingleByteKey(t *testing.T) {
	sm, err := newShardManager(4)
	require.NoError(t, err)

	for b := 0; b < 256; b++ {
		idx := sm.Shard([]byte{byte(b)})
		require.Less(t, idx, uint64(4), "byte=%d", b)
	}
}

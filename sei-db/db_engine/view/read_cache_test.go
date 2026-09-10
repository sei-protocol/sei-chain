package view

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

// A cacheEntry holds its valueChan for exactly as long as a read is in flight for it. Once the
// entry reaches a terminal state — whether by the read completing or by a retire landing on it —
// the channel is detached, so it never keeps a channel, or the result buffered in it, alive.

// TestResolveDeliversThroughBoundChannel exercises the real resolve()/readPool path (no gating,
// no simulated interleaving) to confirm injectValue's bound-channel parameter still wires
// correctly end to end for an ordinary read.
func TestResolveDeliversThroughBoundChannel(t *testing.T) {
	db := newTestDB(map[string][]byte{"k": []byte("v")})
	manager := newTestManagerWithDB(t, db, 1, 1<<20)

	v, found, err := manager.Get([]byte("k"), true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("v"), v)
}

// TestValueChannelAttachedOnlyWhileScheduled runs a long randomized sequence of reads,
// retirements, and evictions against a small keyspace and checks, after every step, that every
// entry holds a valueChan exactly when its status is statusScheduled.
//
// AdHocPool.Submit spawns a goroutine, so a fuzz loop that went through resolve() end to end would
// always block until the read had already completed and could never observe a statusScheduled
// entry — there would be no window to race a retire against. Driving LookupWLocked/injectValue
// directly instead makes that interleaving deterministic and reproducible without any goroutines,
// gates, or timing dependency.
func TestValueChannelAttachedOnlyWhileScheduled(t *testing.T) {
	db := newTestDB(nil)
	shard := newTestShard(t, 1<<30, db)

	rng := rand.New(rand.NewSource(12345))
	keys := []string{"a", "b", "c", "d"}
	pending := map[string]lookupOutcome{}

	randomValue := func() []byte {
		v := make([]byte, rng.Intn(8)+1)
		_, _ = rng.Read(v)
		return v
	}

	checkInvariant := func() {
		shard.lock.Lock()
		defer shard.lock.Unlock()

		var bytes uint64
		var count uint64
		for key, entry := range shard.cache.entries {
			require.Equal(t, entry.status == statusScheduled, entry.valueChan != nil,
				"entry %q has status %v with valueChan != nil == %v",
				key, entry.status, entry.valueChan != nil)
			bytes += entry.size
			if entry.size > 0 {
				count++
			}
		}

		// The size budget is tracked by the cache rather than derived from the entry map, so every
		// path that gives an entry a value, replaces one, or drops an entry has to keep the two in
		// step. A retire landing on a live value and an eviction are both such paths.
		require.Equal(t, bytes, shard.cache.trackedBytes, "trackedBytes disagrees with the entries")
		require.Equal(t, count, shard.cache.trackedCount, "trackedCount disagrees with the entries")
	}

	racedCompletions := 0
	const iterations = 1000
	for i := 0; i < iterations; i++ {
		key := keys[rng.Intn(len(keys))]

		shard.lock.Lock()
		switch rng.Intn(5) {
		case 0: // start a read
			outcome := shard.cache.LookupWLocked([]byte(key), true)
			if outcome.needsSchedule {
				pending[key] = outcome
			}
		case 1: // complete a previously started read, if one is pending
			if outcome, ok := pending[key]; ok {
				delete(pending, key)
				entry := outcome.entry
				raced := entry.status != statusScheduled
				shard.lock.Unlock()
				entry.injectValueUnlocked([]byte(key), outcome.valueChan, readResult{value: randomValue()})
				shard.lock.Lock()
				if raced {
					racedCompletions++
				}
			}
		case 2: // retire: set
			shard.cache.setRetiredWLocked([]byte(key), randomValue())
		case 3: // retire: delete
			shard.cache.deleteRetiredWLocked([]byte(key))
		case 4: // evict
			shard.cache.evictWLocked(shard.cache.hardCap())
		}
		shard.lock.Unlock()

		checkInvariant()
	}

	require.Greater(t, racedCompletions, 0,
		"fuzz run never exercised a retire landing on an in-flight read; the seed/op mix needs adjusting")
}

// TestMaintenanceEvictsBackToBudget checks that the once-per-block maintenance pass brings a cache
// that has been pushed past its budget back within it.
//
// Which keys survive is deliberately not asserted: eviction samples the entry map, so the victims
// depend on map iteration order. Correctness does not, since an evicted key is read from the backing
// DB again.
func TestMaintenanceEvictsBackToBudget(t *testing.T) {
	const maxSize = 1024
	const entrySize = 16 // 8-byte key + 8-byte value, with the test config's zero per-entry overhead

	shard := newTestShard(t, maxSize, newTestDB(nil))

	// Twice the budget, inserted as a retirement so every entry lands in a terminal state.
	retired := make(map[string][]byte)
	for i := 0; i < 2*maxSize/entrySize; i++ {
		retired[fmt.Sprintf("key%05d", i)] = []byte(fmt.Sprintf("val%05d", i))
	}
	shard.lock.Lock()
	shard.cache.PutRetiredWLocked(retired)
	overBudget, _ := shard.cache.SizeInfoRLocked()
	hardCap := shard.cache.hardCap()
	shard.lock.Unlock()

	// The insert path enforces only the hard cap, so the cache is expected to sit above its budget
	// until maintenance runs; a test that started under budget would prove nothing.
	require.Greater(t, overBudget, uint64(maxSize))
	require.LessOrEqual(t, overBudget, hardCap)

	shard.Commit()

	bytes, entries := shard.GetSizeInfo()
	require.LessOrEqual(t, bytes, uint64(maxSize))
	require.Equal(t, bytes, entries*entrySize)
}

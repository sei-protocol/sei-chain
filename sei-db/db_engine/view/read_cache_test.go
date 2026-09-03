package view

import (
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
// entry — there would be no window to race a retire against. Driving lookupLocked/injectValue
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
		for key, entry := range shard.cache.entries {
			require.Equal(t, entry.status == statusScheduled, entry.valueChan != nil,
				"entry %q has status %v with valueChan != nil == %v",
				key, entry.status, entry.valueChan != nil)
		}
	}

	racedCompletions := 0
	const iterations = 1000
	for i := 0; i < iterations; i++ {
		key := keys[rng.Intn(len(keys))]

		shard.lock.Lock()
		switch rng.Intn(5) {
		case 0: // start a read
			outcome := shard.cache.lookupLocked([]byte(key), true)
			if outcome.needsSchedule {
				pending[key] = outcome
			}
		case 1: // complete a previously started read, if one is pending
			if outcome, ok := pending[key]; ok {
				delete(pending, key)
				entry := outcome.entry
				raced := entry.status != statusScheduled
				shard.lock.Unlock()
				entry.injectValue([]byte(key), outcome.valueChan, readResult{value: randomValue()})
				shard.lock.Lock()
				if raced {
					racedCompletions++
				}
			}
		case 2: // retire: set
			shard.cache.setRetiredLocked([]byte(key), randomValue())
		case 3: // retire: delete
			shard.cache.deleteRetiredLocked([]byte(key))
		case 4: // evict
			shard.cache.evictLocked()
		}
		shard.lock.Unlock()

		checkInvariant()
	}

	require.Greater(t, racedCompletions, 0,
		"fuzz run never exercised a retire landing on an in-flight read; the seed/op mix needs adjusting")
}

package view

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// residentValueBytes sums the value bytes reachable from the cache's entries, counting both an
// entry's own value and any value a finished read left attached to it. The retention invariant on
// readCache requires this to stay within the accounted size, so a value the cache holds resident but
// does not weigh shows up here.
//
// The caller must hold the shard lock. A read still in flight is skipped rather than read, since its
// result is not published until its promise closes.
func residentValueBytes(c *readCache) uint64 {
	var total uint64
	for _, entry := range c.entries {
		total += uint64(len(entry.value))
		if p := entry.inflight; p != nil {
			select {
			case <-p.ready:
				total += uint64(len(p.result.value))
			default:
			}
		}
	}
	return total
}

// retainedPromises counts the entries that kept a read promise after settling. The retention
// invariant on readCache requires none, since a promise reaches the value its read produced.
//
// The caller must hold the shard lock.
func retainedPromises(c *readCache) int {
	count := 0
	for _, entry := range c.entries {
		if entry.status != statusScheduled && entry.inflight != nil {
			count++
		}
	}
	return count
}

// accountedBytes reports the size the cache charges against its budget.
//
// The caller must hold the shard lock.
func accountedBytes(c *readCache) uint64 {
	return c.gcQueue.GetTotalSize()
}

// TestCacheDeleteAfterMissDropsFetchedValue reproduces the governance-expiry retention (Immunefi
// 91447): a large value is faulted in on a cache miss, then the key is deleted and the tombstone
// retires into the cache. The cache accounts for the tombstone alone, so it must not still reach the
// fetched value.
func TestCacheDeleteAfterMissDropsFetchedValue(t *testing.T) {
	const key = "gov/proposal"
	large := bytes.Repeat([]byte("p"), 64*1024)
	s := newTestShard(t, 1<<20, newTestDB(map[string][]byte{key: large}))
	c := s.cache

	// Fault the value in through a miss, so it arrives over a read promise.
	value, found, err := s.Get([]byte(key), 1, true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, large, value)

	s.lock.Lock()
	require.Equal(t, uint64(len(key)+len(large)), accountedBytes(c), "a faulted-in value must be weighed")
	require.Equal(t, uint64(len(large)), residentValueBytes(c))
	s.lock.Unlock()

	// Governance deletes the proposal; the tombstone retires out of the MVCC layer into the cache.
	require.NoError(t, s.Delete([]byte(key)))
	require.Equal(t, uint64(2), s.Commit())
	require.NoError(t, s.DropVersions(1, 2))

	// The tombstone stays hot in the LRU, which is what kept the value alive before the fix.
	_, found, err = s.Get([]byte(key), 2, true)
	require.NoError(t, err)
	require.False(t, found)

	s.lock.Lock()
	defer s.lock.Unlock()
	require.Equal(t, uint64(len(key)), accountedBytes(c), "a tombstone weighs its key alone")
	require.Zero(t, residentValueBytes(c), "the deleted proposal's value must not stay resident")
}

// TestCacheRetiredOverwriteDropsFetchedValue is the overwrite twin of the delete case: a large value
// is faulted in, then a small write for the same key retires into the cache. The cache accounts for
// the small value, so it must not still reach the large one.
func TestCacheRetiredOverwriteDropsFetchedValue(t *testing.T) {
	const key = "gov/proposal"
	large := bytes.Repeat([]byte("p"), 64*1024)
	small := []byte("s")
	s := newTestShard(t, 1<<20, newTestDB(map[string][]byte{key: large}))
	c := s.cache

	_, found, err := s.Get([]byte(key), 1, true)
	require.NoError(t, err)
	require.True(t, found)

	require.NoError(t, s.Set([]byte(key), small))
	require.Equal(t, uint64(2), s.Commit())
	require.NoError(t, s.DropVersions(1, 2))

	s.lock.Lock()
	defer s.lock.Unlock()
	require.Equal(t, uint64(len(key)+len(small)), accountedBytes(c))
	require.Equal(t, uint64(len(small)), residentValueBytes(c), "the overwritten value must not stay resident")
}

// TestCacheStaleReadCompletionDoesNotRetainValue covers a delete that lands while the read of the
// same key is still in flight. The completing read must neither resurrect the key nor leave its
// pre-delete value attached to the settled entry.
func TestCacheStaleReadCompletionDoesNotRetainValue(t *testing.T) {
	const key = "gov/proposal"
	large := bytes.Repeat([]byte("p"), 32*1024)
	s := newTestShard(t, 1<<20, newTestDB(nil))
	c := s.cache

	s.lock.Lock()
	outcome := c.lookupLocked([]byte(key), true)
	require.True(t, outcome.needsSchedule, "the key must not already be cached")
	// The delete retires while that read is in flight.
	c.deleteRetiredLocked([]byte(key))
	s.lock.Unlock()

	// The read now completes carrying the pre-delete value.
	outcome.entry.injectValue([]byte(key), outcome.promise, readResult{value: large})

	// The waiter is still released, with the value the read actually produced.
	<-outcome.promise.ready
	require.Equal(t, large, outcome.promise.result.value)

	s.lock.Lock()
	defer s.lock.Unlock()
	require.Equal(t, statusDeleted, outcome.entry.status, "a stale read must not resurrect a deleted key")
	require.Equal(t, uint64(len(key)), accountedBytes(c))
	require.Zero(t, residentValueBytes(c), "the stale read's value must not stay resident")
}

// TestCacheSettleDetachesInflightRead pins the choke point: every terminal state an entry can reach
// from a completing read detaches the read's promise, so no terminal entry reaches a value beyond
// the one it weighs.
func TestCacheSettleDetachesInflightRead(t *testing.T) {
	value := bytes.Repeat([]byte("v"), 4096)
	readFailed := errors.New("read failed")

	for _, tc := range []struct {
		name       string
		result     readResult
		wantStatus valueStatus
		// The value bytes the entry is expected to keep, and to weigh.
		wantResident uint64
	}{
		{"available", readResult{value: value}, statusAvailable, uint64(len(value))},
		{"deleted", readResult{}, statusDeleted, 0},
		{"failed", readResult{err: readFailed}, statusFailed, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const key = "k"
			s := newTestShard(t, 1<<20, newTestDB(nil))
			c := s.cache

			s.lock.Lock()
			outcome := c.lookupLocked([]byte(key), true)
			s.lock.Unlock()

			outcome.entry.injectValue([]byte(key), outcome.promise, tc.result)

			s.lock.Lock()
			defer s.lock.Unlock()
			require.Equal(t, tc.wantStatus, outcome.entry.status)
			require.Zero(t, retainedPromises(c), "a settled entry must not keep its read promise")
			require.Equal(t, tc.wantResident, residentValueBytes(c))
		})
	}
}

// TestCacheBatchSettleDetachesInflightReads is TestCacheSettleDetachesInflightRead for the batch
// path, which settles entries through its own loop rather than injectValue.
func TestCacheBatchSettleDetachesInflightReads(t *testing.T) {
	large := bytes.Repeat([]byte("p"), 32*1024)
	seed := map[string][]byte{"present": large}
	s := newTestShard(t, 1<<20, newTestDB(seed))
	c := s.cache

	results, err := s.BatchGet([][]byte{[]byte("present"), []byte("absent")}, 1)
	require.NoError(t, err)
	require.Equal(t, large, results["present"])
	require.NotContains(t, results, "absent")

	// The batch path settles its entries asynchronously, so wait for the accounting to land.
	want := uint64(len("present") + len(large) + len("absent"))
	require.Eventually(t, func() bool {
		s.lock.Lock()
		defer s.lock.Unlock()
		return accountedBytes(c) == want
	}, 2*time.Second, 2*time.Millisecond, "batch reads were not accounted")

	s.lock.Lock()
	defer s.lock.Unlock()
	require.Zero(t, retainedPromises(c), "a settled entry must not keep its read promise")
	require.Equal(t, uint64(len(large)), residentValueBytes(c))
}

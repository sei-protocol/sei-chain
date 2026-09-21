package pebbledb

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// shutdownDeadline bounds every wait in this file. A regression here is a goroutine that never
// wakes, so the tests have to fail on a deadline rather than hang the package.
const shutdownDeadline = 10 * time.Second

// awaitWithin fails the test if fn has not returned by the deadline.
func awaitWithin(t *testing.T, what string, fn func()) {
	t.Helper()

	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()

	select {
	case <-done:
	case <-time.After(shutdownDeadline):
		t.Fatalf("%s did not finish within %s", what, shutdownDeadline)
	}
}

// Closing with a commit still in flight must release whoever is waiting on it. Whether that batch
// is written or abandoned is the shutdown's business; leaving its caller parked forever is not.
func TestCloseReleasesACommitNobodyWaitedFor(t *testing.T) {
	cfg := DefaultTestConfig(t)
	db, err := Open(t.Context(), &cfg, threading.NewAdHocPool())
	require.NoError(t, err)

	batch := db.NewBatch()
	require.NoError(t, batch.SetAll(map[string][]byte{"key": []byte("value")}))
	handle, err := batch.Commit(types.WriteOptions{Sync: false})
	require.NoError(t, err)

	awaitWithin(t, "Close", func() { require.NoError(t, db.Close()) })
	awaitWithin(t, "Wait", func() { _ = handle.Wait() })
}

// Using a batch after the database is closed is illegal, and has to be survivable rather than
// correct: the contract promises only that it neither deadlocks nor panics. Every entry point is
// exercised, including one on a batch made after the close.
func TestUsingABatchAfterCloseNeitherDeadlocksNorPanics(t *testing.T) {
	cfg := DefaultTestConfig(t)
	db, err := Open(t.Context(), &cfg, threading.NewAdHocPool())
	require.NoError(t, err)

	existing := db.NewBatch()
	require.NoError(t, existing.SetAll(map[string][]byte{"before": []byte("v")}))
	require.NoError(t, db.Close())

	awaitWithin(t, "using a batch made before the close", func() {
		require.NotPanics(t, func() {
			_ = existing.Set([]byte("after"), []byte("v"))
			_ = existing.Delete([]byte("gone"))
			_ = existing.SetAll(map[string][]byte{"more": []byte("v")})
			_ = existing.Len()
			if handle, commitErr := existing.Commit(types.WriteOptions{Sync: false}); commitErr == nil {
				_ = handle.Wait()
			}
		})
	})

	awaitWithin(t, "using a batch made after the close", func() {
		require.NotPanics(t, func() {
			late := db.NewBatch()
			_ = late.SetAll(map[string][]byte{"late": []byte("v")})
			if handle, commitErr := late.Commit(types.WriteOptions{Sync: false}); commitErr == nil {
				_ = handle.Wait()
			}
		})
	})
}

// Close is safe against committers that have not stopped. Every one of them must return, and every
// handle any of them received must complete, however the two interleave.
func TestCommittersRacingCloseAreAllReleased(t *testing.T) {
	cfg := DefaultTestConfig(t)
	// A queue this shallow means committers park on a full queue, which is the state shutdown has
	// to release them from.
	cfg.CommitQueueSize = 1
	db, err := Open(t.Context(), &cfg, threading.NewAdHocPool())
	require.NoError(t, err)

	const committers = 8
	const perCommitter = 32

	var handles sync.Map
	var committing sync.WaitGroup
	committing.Add(committers)
	for committer := range committers {
		go func() {
			defer committing.Done()
			for i := range perCommitter {
				batch := db.NewBatch()
				key := fmt.Sprintf("committer-%d-%d", committer, i)
				if err := batch.SetAll(map[string][]byte{key: []byte("v")}); err != nil {
					return
				}
				handle, err := batch.Commit(types.WriteOptions{Sync: false})
				if err != nil {
					// Refused because the database is closing. Nothing to wait on.
					return
				}
				handles.Store(handle, struct{}{})
			}
		}()
	}

	awaitWithin(t, "Close", func() { require.NoError(t, db.Close()) })
	awaitWithin(t, "the committers", committing.Wait)

	awaitWithin(t, "every handle", func() {
		handles.Range(func(handle any, _ any) bool {
			_ = handle.(types.CommitHandle).Wait()
			return true
		})
	})
}

// An orderly shutdown loses nothing: a commit the caller waited on is on disk after Close, and is
// still there when the database is reopened.
func TestOrderlyCloseLosesNothing(t *testing.T) {
	cfg := DefaultTestConfig(t)
	db, err := Open(t.Context(), &cfg, threading.NewAdHocPool())
	require.NoError(t, err)

	batch := db.NewBatch()
	require.NoError(t, batch.SetAll(map[string][]byte{"durable": []byte("value")}))
	require.NoError(t, types.CommitAndWait(batch, types.WriteOptions{Sync: true}))
	require.NoError(t, db.Close())

	reopened, err := Open(context.Background(), &cfg, threading.NewAdHocPool())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })

	value, err := reopened.Get([]byte("durable"))
	require.NoError(t, err)
	require.Equal(t, "value", string(value))
}

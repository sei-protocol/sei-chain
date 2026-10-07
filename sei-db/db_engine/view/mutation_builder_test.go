package view

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
)

// gatedPool holds every submitted task until release is called, then runs each on its own goroutine.
type gatedPool struct {
	mu       sync.Mutex
	released bool
	queued   []func()
}

var _ threading.Pool = (*gatedPool)(nil)

func (p *gatedPool) Submit(task func()) {
	p.mu.Lock()
	if !p.released {
		p.queued = append(p.queued, task)
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	go task()
}

func (p *gatedPool) Close() {}

// release runs every held task, and every task submitted from now on.
func (p *gatedPool) release() {
	p.mu.Lock()
	p.released = true
	queued := p.queued
	p.queued = nil
	p.mu.Unlock()
	for _, task := range queued {
		go task()
	}
}

// newGatedSortManager builds a manager whose sort pool holds every task until the returned pool is
// released. The pool is released during cleanup, before the manager is closed.
func newGatedSortManager(t *testing.T, db *testDB, shardCount uint64) (ViewManager, *gatedPool) {
	t.Helper()
	pool := threading.NewAdHocPool()
	sortPool := &gatedPool{}
	manager, err := NewViewManager(newTestConfig(shardCount, 1<<20), db, pool, pool, sortPool)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = manager.Close()
		pool.Close()
		_ = db.Close()
	})
	t.Cleanup(sortPool.release)
	return manager, sortPool
}

// A version's mutations come back in ascending key order across every shard, each carrying the value its
// key held in the version before: the database's for the first version, the previous version's after.
func TestMutationsAreSortedAcrossShardsWithPreviousValues(t *testing.T) {
	seed := make(map[string][]byte)
	for i := 0; i < 50; i += 2 {
		seed[fmt.Sprintf("key%03d", i)] = []byte(fmt.Sprintf("db%d", i))
	}
	manager, _ := newTestManager(t, seed, 8, 1<<20)

	for i := 0; i < 50; i++ {
		require.NoError(t, manager.Set([]byte(fmt.Sprintf("key%03d", i)), []byte(fmt.Sprintf("v1-%d", i))))
	}
	first, err := manager.Commit()
	require.NoError(t, err)

	for i := 0; i < 50; i += 3 {
		key := []byte(fmt.Sprintf("key%03d", i))
		if i%2 == 0 {
			require.NoError(t, manager.Delete(key))
		} else {
			require.NoError(t, manager.Set(key, []byte(fmt.Sprintf("v2-%d", i))))
		}
	}
	second, err := manager.Commit()
	require.NoError(t, err)

	firstMutations, err := first.Mutations()
	require.NoError(t, err)
	require.Len(t, firstMutations, 50)
	requireStrictlyAscending(t, firstMutations)
	for _, m := range firstMutations {
		if previous, seeded := seed[m.Key()]; seeded {
			require.Equal(t, previous, m.Previous(), "key %s", m.Key())
		} else {
			require.Nil(t, m.Previous(), "key %s was absent before the first version", m.Key())
		}
	}

	secondMutations, err := second.Mutations()
	require.NoError(t, err)
	require.Len(t, secondMutations, 17)
	requireStrictlyAscending(t, secondMutations)
	for _, m := range secondMutations {
		var i int
		_, err := fmt.Sscanf(m.Key(), "key%03d", &i)
		require.NoError(t, err)
		require.Equal(t, []byte(fmt.Sprintf("v1-%d", i)), m.Previous(), "key %s", m.Key())
		if i%2 == 0 {
			require.Nil(t, m.Value(), "key %s was deleted", m.Key())
		}
	}

	finalizeAndRelease(t, first)
	finalizeAndRelease(t, second)
}

func requireStrictlyAscending(t *testing.T, mutations []gigatypes.Mutation) {
	t.Helper()
	require.True(t, sort.SliceIsSorted(mutations, func(i, j int) bool {
		return strings.Compare(mutations[i].Key(), mutations[j].Key()) < 0
	}))
	for i := 1; i < len(mutations); i++ {
		require.NotEqual(t, mutations[i-1].Key(), mutations[i].Key(), "a key appears twice")
	}
}

// Materializing a version reads the version before, so that version stays tracked, even once it is
// flushed and released, until its successor has been materialized.
func TestVersionWaitsForItsSuccessorBeforeRetiring(t *testing.T) {
	manager, _ := newTestManager(t, nil, 4, 1<<20)

	require.NoError(t, manager.Set([]byte("k"), []byte("v1")))
	first, err := manager.Commit()
	require.NoError(t, err)
	finalizeAwaitFlushAndRelease(t, first)

	require.Never(t, func() bool { return !isTracked(manager, 1) }, 100*time.Millisecond, 5*time.Millisecond,
		"a version must not retire before its successor is materialized")

	require.NoError(t, manager.Set([]byte("k"), []byte("v2")))
	second, err := manager.Commit()
	require.NoError(t, err)
	mutations, err := second.Mutations()
	require.NoError(t, err)
	require.Equal(t, []byte("v1"), mutations[0].Previous())
	finalizeAndRelease(t, second)

	awaitRetired(t, manager, 1)
}

// A flush waits for the version's mutations to be published before it writes anything.
func TestFlushWaitsForTheVersionsMutations(t *testing.T) {
	db := newTestDB(map[string][]byte{"k": []byte("db")})
	manager, sortPool := newGatedSortManager(t, db, 4)

	require.NoError(t, manager.Set([]byte("k"), []byte("v1")))
	view, err := manager.Commit()
	require.NoError(t, err)
	require.NoError(t, view.Finalize(hashWrites(testHash)))

	require.Never(t, func() bool { return db.commitCount.Load() > 0 }, 100*time.Millisecond, 5*time.Millisecond,
		"nothing may be flushed before the version's mutations are published")

	sortPool.release()
	awaitFlushed(t, view, 2*time.Second)
	require.NoError(t, view.Release())

	value, found := db.get("k")
	require.True(t, found)
	require.Equal(t, []byte("v1"), value)
}

// Close waits for a materialization still in flight, so that its database reads finish before the
// database is closed, and a clean close stays clean.
func TestCloseWaitsForAnInFlightMaterialization(t *testing.T) {
	db := newTestDB(nil)
	manager, sortPool := newGatedSortManager(t, db, 4)

	require.NoError(t, manager.Set([]byte("k"), []byte("v1")))
	view, err := manager.Commit()
	require.NoError(t, err)
	view.Abandon()

	closed := make(chan error, 1)
	go func() {
		closed <- manager.Close()
	}()
	require.Never(t, func() bool { return len(closed) > 0 }, 100*time.Millisecond, 5*time.Millisecond,
		"Close must wait for the materialization")

	sortPool.release()
	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return once the materialization could finish")
	}
	require.Positive(t, db.getCalls.Load(), "the materialization reads the key's previous value")
	require.Zero(t, db.getsAfterClose.Load(), "nothing may read the database after Close")
}

// A previous value that cannot be read bricks the manager, and a caller waiting on the version's
// mutations is released with an error rather than left waiting.
func TestFailedPreviousValueReadBricksTheManager(t *testing.T) {
	db := newTestDB(nil)
	readErr := errors.New("disk on fire")
	db.getErrKeys = map[string]error{"k": readErr}
	manager := newTestManagerWithDB(t, db, 4, 1<<20)

	require.NoError(t, manager.Set([]byte("k"), []byte("v1")))
	view, err := manager.Commit()
	require.NoError(t, err)
	defer view.Abandon()

	_, err = view.Mutations()
	require.ErrorIs(t, err, readErr)

	_, err = manager.Commit()
	require.ErrorIs(t, err, readErr, "the manager is bricked")
}

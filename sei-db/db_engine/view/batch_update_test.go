package view

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// markUpdater folds a value by appending mark to whatever the key already held, so a result says
// which value it was folded onto rather than merely that a fold happened. A nil prior folds from
// "<none>", which distinguishes a key the store held nothing for from one holding an empty value.
type markUpdater struct {
	mark byte
}

func (u markUpdater) NewValueFor(_ string, priorValue []byte) ([]byte, error) {
	if priorValue == nil {
		return append([]byte("<none>"), u.mark), nil
	}
	return append(append([]byte{}, priorValue...), u.mark), nil
}

// parkedUpdater holds every fold until it is released, which is what lets a test tell staging apart
// from folding.
type parkedUpdater struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newParkedUpdater() *parkedUpdater {
	return &parkedUpdater{started: make(chan struct{}), release: make(chan struct{})}
}

func (u *parkedUpdater) NewValueFor(_ string, priorValue []byte) ([]byte, error) {
	u.once.Do(func() { close(u.started) })
	<-u.release
	return append(append([]byte{}, priorValue...), '+'), nil
}

// failingUpdater fails every fold, standing in for a corrupted stored value.
type failingUpdater struct{}

func (failingUpdater) NewValueFor(_ string, _ []byte) ([]byte, error) {
	return nil, fmt.Errorf("fold refused this value")
}

// BatchUpdate must return without folding anything. The fold here parks until released, so a
// BatchUpdate that performed it on the calling thread could never return and this test would hang
// rather than pass — reaching the assertions at all is the proof.
func TestBatchUpdateReturnsBeforeFolding(t *testing.T) {
	manager, _ := newTestManager(t, map[string][]byte{"k": []byte("old")}, 4, 1<<20)
	updater := newParkedUpdater()

	require.NoError(t, manager.BatchUpdate([]string{"k"}, updater))

	// A read of a staged key must wait for its value rather than miss the write or serve the value it
	// replaced. Observing "did not return" needs a deadline; the fold is parked, so any wait fails
	// the same way.
	type readResult struct {
		value []byte
		found bool
		err   error
	}
	reads := make(chan readResult, 1)
	go func() {
		value, found, err := manager.Get([]byte("k"), true)
		reads <- readResult{value: value, found: found, err: err}
	}()

	<-updater.started
	select {
	case got := <-reads:
		t.Fatalf("a read of a staged key returned %q before its value was available", got.value)
	case <-time.After(50 * time.Millisecond):
	}

	close(updater.release)
	got := <-reads
	require.NoError(t, got.err)
	require.True(t, got.found)
	require.Equal(t, []byte("old+"), got.value, "the read must serve the folded value")
}

// A staged key has to read as its new value on every read surface, not just the single-key one.
func TestBatchUpdateStagedValueIsVisibleToEveryReadPath(t *testing.T) {
	seed := map[string][]byte{"a": []byte("1"), "b": []byte("2")}
	manager, _ := newTestManager(t, seed, 4, 1<<20)

	require.NoError(t, manager.BatchUpdate([]string{"a", "b"}, markUpdater{mark: '+'}))

	value, found, err := manager.Get([]byte("a"), true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("1+"), value)

	batch, err := manager.BatchGet([][]byte{[]byte("a"), []byte("b")})
	require.NoError(t, err)
	require.Equal(t, []byte("1+"), batch["a"])
	require.Equal(t, []byte("2+"), batch["b"])

	it, err := manager.Iterator(nil)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()
	seen := map[string][]byte{}
	for ; it.Valid(); it.Next() {
		seen[string(it.Key())] = append([]byte{}, it.Value()...)
	}
	require.NoError(t, it.Error())
	require.Equal(t, []byte("1+"), seen["a"], "an iterator must not copy an unfolded value")
	require.Equal(t, []byte("2+"), seen["b"])
}

// Two folds staged for one key within a single version must apply in the order they were staged: the
// second folds onto the first's result, not onto the value both of them started from.
func TestBatchUpdateChainsFoldsWithinAVersion(t *testing.T) {
	manager, _ := newTestManager(t, map[string][]byte{"k": []byte("a")}, 4, 1<<20)

	require.NoError(t, manager.BatchUpdate([]string{"k"}, markUpdater{mark: '1'}))
	require.NoError(t, manager.BatchUpdate([]string{"k"}, markUpdater{mark: '2'}))

	value, found, err := manager.Get([]byte("k"), true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("a12"), value)
}

// The same key folded in consecutive versions must chain the same way, with each version's fold
// applying to the previous version's result.
func TestBatchUpdateChainsFoldsAcrossVersions(t *testing.T) {
	manager, _ := newTestManager(t, map[string][]byte{"k": []byte("a")}, 4, 1<<20)

	var held []View
	defer func() {
		for _, v := range held {
			require.NoError(t, v.Release())
		}
	}()

	for _, mark := range []byte{'1', '2', '3'} {
		require.NoError(t, manager.BatchUpdate([]string{"k"}, markUpdater{mark: mark}))
		sealed, err := manager.Commit()
		require.NoError(t, err)
		require.NoError(t, sealed.Finalize(hashWrites(testHash)))
		held = append(held, sealed)
	}

	value, found, err := manager.Get([]byte("k"), true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("a123"), value)

	// Each sealed version's diff must carry that version's own folded value, since the diff is what
	// hashing and flushing read.
	for i, want := range [][]byte{[]byte("a1"), []byte("a12"), []byte("a123")} {
		diff, err := held[i].GetDiff()
		require.NoError(t, err)
		require.Equal(t, want, diff["k"], "version %d's diff", i+1)
	}
}

// A key whose fold deletes it must read as absent and reach the diff as a nil value, which is how a
// delete is carried to the flush.
func TestBatchUpdateFoldCanDelete(t *testing.T) {
	manager, _ := newTestManager(t, map[string][]byte{"k": []byte("doomed")}, 4, 1<<20)

	require.NoError(t, manager.BatchUpdate([]string{"k"}, deletingUpdater{}))

	_, found, err := manager.Get([]byte("k"), true)
	require.NoError(t, err)
	require.False(t, found, "a key whose fold returned nil must read as absent")

	sealed, err := manager.Commit()
	require.NoError(t, err)
	require.NoError(t, sealed.Finalize(hashWrites(testHash)))
	defer func() { require.NoError(t, sealed.Release()) }()

	diff, err := sealed.GetDiff()
	require.NoError(t, err)
	value, present := diff["k"]
	require.True(t, present, "a delete must appear in the diff")
	require.Nil(t, value, "a delete is carried as a nil value")
}

type deletingUpdater struct{}

func (deletingUpdater) NewValueFor(_ string, _ []byte) ([]byte, error) { return nil, nil }

// A fold that fails has to be reported to everything that would otherwise read its value as good: a
// reader, and the diff that hashing and flushing consume. It must also brick the manager, because a
// version missing one of its writes can never be hashed correctly.
func TestBatchUpdateFoldFailureReachesObservers(t *testing.T) {
	manager, _ := newTestManager(t, map[string][]byte{"k": []byte("old")}, 4, 1<<20)

	require.NoError(t, manager.BatchUpdate([]string{"k"}, failingUpdater{}))

	_, _, err := manager.Get([]byte("k"), true)
	require.Error(t, err, "a reader must not be handed the value a failed fold never produced")
	require.Contains(t, err.Error(), "fold refused this value")

	// The diff consumers have to fail too rather than hash a version that is missing this key.
	_, err = manager.Commit()
	require.Error(t, err, "a failed fold must brick the manager")
}

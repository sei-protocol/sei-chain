package keeper

import (
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeSnapshot is a minimal TraceSnapshot that records copies and releases so we can assert lifecycle.
type fakeSnapshot struct {
	released int32
	copies   int32
	id       int64
}

func (f *fakeSnapshot) Copy() *fakeSnapshot {
	atomic.AddInt32(&f.copies, 1)
	return &fakeSnapshot{id: f.id}
}

func (f *fakeSnapshot) ReleaseSnapshotRefs() error {
	atomic.StoreInt32(&f.released, 1)
	return nil
}

func (f *fakeSnapshot) IsReleased() bool { return atomic.LoadInt32(&f.released) == 1 }

func TestTraceSnapshotStorePutGet(t *testing.T) {
	s := NewTraceSnapshotStore[*fakeSnapshot](8)
	c := &fakeSnapshot{id: 100}
	s.Put(100, c)
	require.Same(t, c, s.Get(100))
	require.Nil(t, s.Get(99))
}

func TestTraceSnapshotStoreEvictsOlderThanWindow(t *testing.T) {
	s := NewTraceSnapshotStore[*fakeSnapshot](3)
	snapshots := make([]*fakeSnapshot, 6)
	for i := range snapshots {
		snapshots[i] = &fakeSnapshot{id: int64(100 + i)}
		s.Put(int64(100+i), snapshots[i])
	}
	// window=3 keeps heights in (105-3, 105] = {103, 104, 105}.
	for _, h := range []int64{103, 104, 105} {
		require.NotNil(t, s.Get(h), "height %d should be retained", h)
	}
	for _, h := range []int64{100, 101, 102} {
		require.Nil(t, s.Get(h), "height %d should be evicted", h)
	}
	for i := 0; i < 3; i++ {
		require.True(t, snapshots[i].IsReleased(), "evicted entry %d should be released by Put", 100+i)
	}
}

func TestTraceSnapshotStoreReplaceReleasesOld(t *testing.T) {
	s := NewTraceSnapshotStore[*fakeSnapshot](8)
	old := &fakeSnapshot{id: 200}
	s.Put(200, old)
	newer := &fakeSnapshot{id: 200}
	s.Put(200, newer)
	require.True(t, old.IsReleased())
	require.False(t, newer.IsReleased())
	require.Same(t, newer, s.Get(200))
}

func TestTraceSnapshotStoreLeaseReturnsOwnedCopy(t *testing.T) {
	s := NewTraceSnapshotStore[*fakeSnapshot](8)
	orig := &fakeSnapshot{id: 100}
	s.Put(100, orig)
	leased, release := s.Lease(100)
	require.NotNil(t, leased)
	require.False(t, orig.IsReleased())
	require.Equal(t, int32(1), atomic.LoadInt32(&orig.copies))
	require.NotSame(t, orig, leased)

	release()
	require.False(t, orig.IsReleased(), "releasing lease must not release stored snapshot")
	require.True(t, leased.IsReleased())
}

func TestTraceSnapshotStoreCloseDropsRefs(t *testing.T) {
	s := NewTraceSnapshotStore[*fakeSnapshot](8)
	snapshots := []*fakeSnapshot{{id: 1}, {id: 2}, {id: 3}}
	for i, c := range snapshots {
		s.Put(int64(i+1), c)
	}
	s.Close()
	for i, c := range snapshots {
		require.True(t, c.IsReleased(), "Close must release retained snapshot")
		require.Nil(t, s.Get(int64(i+1)))
	}
}

func TestTraceSnapshotStoreNilSafe(t *testing.T) {
	var s *TraceSnapshotStore[*fakeSnapshot]
	require.Nil(t, s.Get(1))
	leased, release := s.Lease(1)
	require.Nil(t, leased)
	release()
	s.Put(1, &fakeSnapshot{id: 1}) // no panic
	s.Close()                      // no panic
}

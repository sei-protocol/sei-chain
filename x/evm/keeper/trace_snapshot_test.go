package keeper

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/stretchr/testify/require"
)

// fakeCommitter is a stub sctypes.Committer that lets tests assert the
// store's lifecycle calls (Close in particular). Only the methods the
// snapshot store touches are non-trivial; the rest panic to make a
// surprise call obvious in test output.
type fakeCommitter struct {
	mu     sync.Mutex
	closed int32
	id     int64
}

func (f *fakeCommitter) Close() error           { atomic.StoreInt32(&f.closed, 1); return nil }
func (f *fakeCommitter) IsClosed() bool         { return atomic.LoadInt32(&f.closed) == 1 }
func (f *fakeCommitter) Version() int64         { return f.id }
func (f *fakeCommitter) Initialize(_ []string)  { panic("unused") }
func (f *fakeCommitter) Commit() (int64, error) { panic("unused") }
func (f *fakeCommitter) GetLatestVersion() (int64, error) {
	panic("unused")
}
func (f *fakeCommitter) GetEarliestVersion() (int64, error) {
	panic("unused")
}
func (f *fakeCommitter) ApplyChangeSets(_ []*proto.NamedChangeSet) error { panic("unused") }
func (f *fakeCommitter) ApplyUpgrades(_ []*proto.TreeNameUpgrade) error  { panic("unused") }
func (f *fakeCommitter) WorkingCommitInfo() *proto.CommitInfo            { panic("unused") }
func (f *fakeCommitter) LastCommitInfo() *proto.CommitInfo               { panic("unused") }
func (f *fakeCommitter) LoadVersion(_ int64, _ bool) (sctypes.Committer, error) {
	panic("unused")
}
func (f *fakeCommitter) Rollback(_ int64) error          { panic("unused") }
func (f *fakeCommitter) SetInitialVersion(_ int64) error { panic("unused") }
func (f *fakeCommitter) GetChildStoreByName(_ string) sctypes.CommitKVStore {
	panic("unused")
}
func (f *fakeCommitter) Copy() sctypes.Committer { panic("unused") }
func (f *fakeCommitter) Importer(_ int64) (sctypes.Importer, error) {
	panic("unused")
}
func (f *fakeCommitter) Exporter(_ int64) (sctypes.Exporter, error) {
	panic("unused")
}

func TestTraceSnapshotStorePutGet(t *testing.T) {
	s := NewTraceSnapshotStore(8)
	c := &fakeCommitter{id: 100}
	s.Put(100, c)
	require.True(t, s.Has(100))
	require.Same(t, sctypes.Committer(c), s.Get(100))
	require.Equal(t, 1, s.Len())
	require.Nil(t, s.Get(99))
}

func TestTraceSnapshotStoreEvictsOlderThanWindow(t *testing.T) {
	s := NewTraceSnapshotStore(3)
	committers := make([]*fakeCommitter, 6)
	for i := range committers {
		committers[i] = &fakeCommitter{id: int64(100 + i)}
		s.Put(int64(100+i), committers[i])
	}
	// window=3 means we keep heights in (105-3, 105] = {103, 104, 105}.
	require.Equal(t, 3, s.Len())
	require.True(t, s.Has(103))
	require.True(t, s.Has(104))
	require.True(t, s.Has(105))
	require.False(t, s.Has(102))
	// Evicted entries should have been Closed.
	require.True(t, committers[0].IsClosed(), "100 should be closed")
	require.True(t, committers[1].IsClosed(), "101 should be closed")
	require.True(t, committers[2].IsClosed(), "102 should be closed")
	require.False(t, committers[5].IsClosed(), "105 should still be open")
}

func TestTraceSnapshotStoreReplaceClosesOld(t *testing.T) {
	s := NewTraceSnapshotStore(8)
	old := &fakeCommitter{id: 200}
	s.Put(200, old)
	newer := &fakeCommitter{id: 200}
	s.Put(200, newer)
	require.True(t, old.IsClosed(), "old should be closed when replaced at same height")
	require.False(t, newer.IsClosed(), "newer is still active")
	require.Same(t, sctypes.Committer(newer), s.Get(200))
}

func TestTraceSnapshotStoreCloseAll(t *testing.T) {
	s := NewTraceSnapshotStore(8)
	cs := []*fakeCommitter{{id: 1}, {id: 2}, {id: 3}}
	for i, c := range cs {
		s.Put(int64(i+1), c)
	}
	s.Close()
	require.Equal(t, 0, s.Len())
	for _, c := range cs {
		require.True(t, c.IsClosed())
	}
}

func TestTraceSnapshotStoreNilSafe(t *testing.T) {
	var s *TraceSnapshotStore
	require.Nil(t, s.Get(1))
	require.False(t, s.Has(1))
	require.Equal(t, 0, s.Len())
	s.Put(1, &fakeCommitter{id: 1}) // no panic
	s.Close()                       // no panic
}

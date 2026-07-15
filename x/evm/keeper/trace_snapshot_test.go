package keeper

import (
	"sync/atomic"
	"testing"

	ics23 "github.com/confio/ics23/go"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

var _ sctypes.Committer = (*fakeCommitter)(nil)

// fakeCommitter is a minimal Committer stub that records Close so we can
// assert lifecycle. Methods the snapshot store doesn't touch panic to make
// surprise calls visible.
type fakeCommitter struct {
	closed int32
	copies int32
	id     int64
}

func (f *fakeCommitter) Close() error                             { atomic.StoreInt32(&f.closed, 1); return nil }
func (f *fakeCommitter) IsClosed() bool                           { return atomic.LoadInt32(&f.closed) == 1 }
func (f *fakeCommitter) Version() int64                           { return f.id }
func (f *fakeCommitter) Initialize(_ []string) error              { panic("unused") }
func (f *fakeCommitter) Commit() (int64, error)                   { panic("unused") }
func (f *fakeCommitter) GetLatestVersion() (int64, error)         { panic("unused") }
func (f *fakeCommitter) Get(string, []byte) ([]byte, bool, error) { panic("unused") }
func (f *fakeCommitter) SetWriteMode(sctypes.WriteMode) error     { panic("unused") }
func (f *fakeCommitter) SetMigrationBatchSize(int) error          { panic("unused") }
func (f *fakeCommitter) Has(string, []byte) (bool, error)         { panic("unused") }
func (f *fakeCommitter) Iterator(string, []byte, []byte, bool) (dbm.Iterator, error) {
	panic("unused")
}
func (f *fakeCommitter) GetProof(string, []byte) (*ics23.CommitmentProof, error) {
	panic("unused")
}
func (f *fakeCommitter) ApplyChangeSets(_ []*proto.NamedChangeSet) error { panic("unused") }
func (f *fakeCommitter) ApplyUpgrades(_ []*proto.TreeNameUpgrade) error  { panic("unused") }
func (f *fakeCommitter) WorkingCommitInfo() *proto.CommitInfo            { panic("unused") }
func (f *fakeCommitter) LastCommitInfo() *proto.CommitInfo               { panic("unused") }
func (f *fakeCommitter) LoadVersion(int64, bool) (sctypes.Committer, error) {
	panic("unused")
}
func (f *fakeCommitter) Rollback(int64) error                             { panic("unused") }
func (f *fakeCommitter) SetInitialVersion(int64) error                    { panic("unused") }
func (f *fakeCommitter) GetChildStoreByName(string) sctypes.CommitKVStore { panic("unused") }
func (f *fakeCommitter) Copy() sctypes.Committer {
	atomic.AddInt32(&f.copies, 1)
	return &fakeCommitter{id: f.id}
}
func (f *fakeCommitter) Importer(int64) (sctypes.Importer, error) { panic("unused") }
func (f *fakeCommitter) Exporter(int64) (sctypes.Exporter, error) { panic("unused") }

func TestTraceSnapshotStorePutGet(t *testing.T) {
	s := NewTraceSnapshotStore(8)
	c := &fakeCommitter{id: 100}
	s.Put(100, c)
	require.Same(t, sctypes.Committer(c), s.Get(100))
	require.Nil(t, s.Get(99))
}

func TestTraceSnapshotStoreEvictsOlderThanWindow(t *testing.T) {
	s := NewTraceSnapshotStore(3)
	committers := make([]*fakeCommitter, 6)
	for i := range committers {
		committers[i] = &fakeCommitter{id: int64(100 + i)}
		s.Put(int64(100+i), committers[i])
	}
	// window=3 keeps heights in (105-3, 105] = {103, 104, 105}.
	for _, h := range []int64{103, 104, 105} {
		require.NotNil(t, s.Get(h), "height %d should be retained", h)
	}
	for _, h := range []int64{100, 101, 102} {
		require.Nil(t, s.Get(h), "height %d should be evicted", h)
	}
	for i := 0; i < 3; i++ {
		require.True(t, committers[i].IsClosed(), "evicted entry %d should be closed by Put", 100+i)
	}
}

func TestTraceSnapshotStoreReplaceClosesOld(t *testing.T) {
	s := NewTraceSnapshotStore(8)
	old := &fakeCommitter{id: 200}
	s.Put(200, old)
	newer := &fakeCommitter{id: 200}
	s.Put(200, newer)
	require.True(t, old.IsClosed())
	require.False(t, newer.IsClosed())
	require.Same(t, sctypes.Committer(newer), s.Get(200))
}

func TestTraceSnapshotStoreLeaseReturnsOwnedCopy(t *testing.T) {
	s := NewTraceSnapshotStore(8)
	orig := &fakeCommitter{id: 100}
	s.Put(100, orig)
	leased, release := s.Lease(100)
	require.NotNil(t, leased)
	require.False(t, orig.IsClosed())
	require.Equal(t, int32(1), atomic.LoadInt32(&orig.copies))
	require.NotSame(t, orig, leased)

	release()
	require.False(t, orig.IsClosed(), "releasing lease must not close stored snapshot")
	require.True(t, leased.(*fakeCommitter).IsClosed())
}

func TestTraceSnapshotStoreCloseDropsRefs(t *testing.T) {
	s := NewTraceSnapshotStore(8)
	cs := []*fakeCommitter{{id: 1}, {id: 2}, {id: 3}}
	for i, c := range cs {
		s.Put(int64(i+1), c)
	}
	s.Close()
	for i, c := range cs {
		require.True(t, c.IsClosed(), "Close must release retained snapshot")
		require.Nil(t, s.Get(int64(i+1)))
	}
}

func TestTraceSnapshotStoreNilSafe(t *testing.T) {
	var s *TraceSnapshotStore
	require.Nil(t, s.Get(1))
	leased, release := s.Lease(1)
	require.Nil(t, leased)
	release()
	s.Put(1, &fakeCommitter{id: 1}) // no panic
	s.Close()                       // no panic
}

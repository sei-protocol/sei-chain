package memiavl

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

// Regression: a Copy()'d tree must remain readable across a memiavl
// snapshot rewrite + reload. Before refcounting *Snapshot, the rewrite
// path called snapshot.Close() (munmap) while a held trace-baker copy
// was still pointing into it — crashing reads in cmpbody.
func TestTreeCopyOutlivesSnapshotRewrite(t *testing.T) {
	db, err := OpenDB(0, Options{
		Config:          Config{SnapshotKeepRecent: 0},
		Dir:             t.TempDir(),
		CreateIfMissing: true,
		InitialStores:   []string{"test"},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	cs := []*proto.NamedChangeSet{{
		Name: "test",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("hello"), Value: []byte("world")},
			{Key: []byte("hello1"), Value: []byte("world1")},
		}},
	}}
	require.NoError(t, db.ApplyChangeSets(cs))
	_, err = db.Commit()
	require.NoError(t, err)

	held := db.Copy()
	defer func() { _ = held.ReleaseSnapshotRefs() }()

	require.NoError(t, db.RewriteSnapshot(context.Background()))
	require.NoError(t, db.Reload())

	cs2 := []*proto.NamedChangeSet{{
		Name: "test",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("hello"), Value: []byte("OVERWRITTEN")},
		}},
	}}
	require.NoError(t, db.ApplyChangeSets(cs2))
	_, err = db.Commit()
	require.NoError(t, err)

	tree := held.TreeByName("test")
	require.NotNil(t, tree)
	require.Equal(t, "world", string(tree.Get([]byte("hello"))))
	require.Equal(t, "world1", string(tree.Get([]byte("hello1"))))
}

func TestMultipleCopiesIndependentLifecycle(t *testing.T) {
	db, err := OpenDB(0, Options{
		Config:          Config{SnapshotKeepRecent: 0},
		Dir:             t.TempDir(),
		CreateIfMissing: true,
		InitialStores:   []string{"test"},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	require.NoError(t, db.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name: "test",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("k"), Value: []byte("v")},
		}},
	}}))
	_, err = db.Commit()
	require.NoError(t, err)

	copyA := db.Copy()
	copyB := db.Copy()

	require.NoError(t, db.RewriteSnapshot(context.Background()))
	require.NoError(t, db.Reload())

	require.NoError(t, copyA.ReleaseSnapshotRefs())

	tree := copyB.TreeByName("test")
	require.NotNil(t, tree)
	require.Equal(t, "v", string(tree.Get([]byte("k"))))

	require.NoError(t, copyB.ReleaseSnapshotRefs())
}

func TestTreeCopyConcurrentRewriteReload(t *testing.T) {
	db, err := OpenDB(0, Options{
		Config:          Config{SnapshotKeepRecent: 0},
		Dir:             t.TempDir(),
		CreateIfMissing: true,
		InitialStores:   []string{"test"},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var pairs []*proto.KVPair
	for i := 0; i < 32; i++ {
		pairs = append(pairs, &proto.KVPair{
			Key:   []byte(fmt.Sprintf("key-%02d", i)),
			Value: []byte(fmt.Sprintf("value-%02d", i)),
		})
	}
	require.NoError(t, db.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name:      "test",
		Changeset: proto.ChangeSet{Pairs: pairs},
	}}))
	_, err = db.Commit()
	require.NoError(t, err)

	errCh := make(chan error, 32)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 40; j++ {
				held := db.Copy()
				tree := held.TreeByName("test")
				if tree == nil {
					errCh <- fmt.Errorf("missing copied tree")
					_ = held.ReleaseSnapshotRefs()
					return
				}
				time.Sleep(time.Millisecond)
				if got := tree.Get([]byte("key-00")); len(got) == 0 {
					errCh <- fmt.Errorf("missing copied value")
					_ = held.ReleaseSnapshotRefs()
					return
				}
				if err := held.ReleaseSnapshotRefs(); err != nil {
					errCh <- err
					return
				}
			}
		}()
	}

	for i := 0; i < 10; i++ {
		require.NoError(t, db.RewriteSnapshot(context.Background()))
		require.NoError(t, db.Reload())
		require.NoError(t, db.ApplyChangeSets([]*proto.NamedChangeSet{{
			Name: "test",
			Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte(fmt.Sprintf("round-%02d", i)), Value: []byte("ok")},
			}},
		}}))
		_, err = db.Commit()
		require.NoError(t, err)
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
}

func TestSnapshotDoubleCloseReturnsError(t *testing.T) {
	snapshot := NewEmptySnapshot(1)
	require.NoError(t, snapshot.Close())
	require.Error(t, snapshot.Close())
	require.Panics(t, snapshot.Acquire)
}

// snapshotBackedDB returns a DB whose "test" tree is backed by a mapped snapshot
// holding pairs, which is the state an iterator must be opened over to exercise
// the mmap: an iterator over unpersisted MemNodes reads the heap instead.
func snapshotBackedDB(t *testing.T, pairs []*proto.KVPair) *DB {
	t.Helper()
	db, err := OpenDB(0, Options{
		Config:          Config{SnapshotKeepRecent: 0},
		Dir:             t.TempDir(),
		CreateIfMissing: true,
		InitialStores:   []string{"test"},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	require.NoError(t, db.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name:      "test",
		Changeset: proto.ChangeSet{Pairs: pairs},
	}}))
	_, err = db.Commit()
	require.NoError(t, err)

	require.NoError(t, db.RewriteSnapshot(context.Background()))
	require.NoError(t, db.Reload())
	return db
}

// Regression: an iterator handed out by Tree.Iterator must stay readable across a
// snapshot rewrite and reload. Key and Value clone out of the snapshot's mmap
// after Tree.Iterator has already released the read lock, so before the iterator
// took a reference the reload's snapshot.Close() unmapped pages it was still
// reading. That killed the process with a fatal fault in runtime.memmove, which
// no recover can catch, and it was observed killing validators mid-block.
func TestTreeIteratorOutlivesSnapshotRewrite(t *testing.T) {
	db := snapshotBackedDB(t, []*proto.KVPair{
		{Key: []byte("k1"), Value: []byte("v1")},
		{Key: []byte("k2"), Value: []byte("v2")},
	})

	tree := db.TreeByName("test")
	require.NotNil(t, tree)
	// Production opens memiavl with ZeroCopy false, which is what routes Key and
	// Value through utils.Clone and so reads the mapping on every pair.
	tree.SetZeroCopy(false)
	iter := tree.Iterator(nil, nil, true)

	// Rotate underneath the open iterator: Reload's ReplaceWith closes the
	// snapshot the iterator is reading.
	require.NoError(t, db.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name: "test",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("k1"), Value: []byte("OVERWRITTEN")},
		}},
	}}))
	_, err := db.Commit()
	require.NoError(t, err)
	require.NoError(t, db.RewriteSnapshot(context.Background()))
	require.NoError(t, db.Reload())

	// The iterator reports the contents it was opened over, not the rewrite's.
	require.Equal(t, []pair{
		{key: []byte("k1"), value: []byte("v1")},
		{key: []byte("k2"), value: []byte("v2")},
	}, collectIter(iter))
	require.NoError(t, iter.Close())
}

// The reference an iterator takes has to come back on Close, or every iterator
// pins its snapshot's blob files mapped for the life of the process.
func TestTreeIteratorReleasesSnapshotOnClose(t *testing.T) {
	db := snapshotBackedDB(t, []*proto.KVPair{{Key: []byte("k"), Value: []byte("v")}})

	tree := db.TreeByName("test")
	require.NotNil(t, tree)
	held := tree.snapshot
	require.NotNil(t, held)
	before := held.refCount.Load()

	iter := tree.Iterator(nil, nil, true)
	require.Equal(t, before+1, held.refCount.Load(), "iterator did not take a reference")

	require.NoError(t, iter.Close())
	require.Equal(t, before, held.refCount.Load(), "Close did not return the reference")

	// Close is idempotent, so a caller that closes twice cannot drive the
	// refcount below what it took and unmap the snapshot under someone else.
	require.NoError(t, iter.Close())
	require.Equal(t, before, held.refCount.Load())
}

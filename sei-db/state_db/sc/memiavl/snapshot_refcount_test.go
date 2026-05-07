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

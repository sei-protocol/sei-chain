package memiavl

import (
	"context"
	"testing"

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
	defer func() { _ = held.Close() }()

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

	require.NoError(t, copyA.Close())

	tree := copyB.TreeByName("test")
	require.NotNil(t, tree)
	require.Equal(t, "v", string(tree.Get([]byte("k"))))

	require.NoError(t, copyB.Close())
}

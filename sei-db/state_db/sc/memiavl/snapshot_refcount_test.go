package memiavl

import (
	"context"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
	"github.com/stretchr/testify/require"
)

// Regression: a Copy()'d tree must remain readable across a memiavl
// snapshot rewrite + reload. Before refcounting *Snapshot, the rewrite
// path called snapshot.Close() (which munmap'd the file) while a held
// trace-baker copy was still pointing into it — crashing reads with
// SIGSEGV inside bytes.Compare.
func TestTreeCopyOutlivesSnapshotRewrite(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(0, Options{
		Config:          Config{SnapshotKeepRecent: 0},
		Dir:             dir,
		CreateIfMissing: true,
		InitialStores:   []string{"test"},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	cs := []*proto.NamedChangeSet{{
		Name: "test",
		Changeset: iavl.ChangeSet{Pairs: []*iavl.KVPair{
			{Key: []byte("hello"), Value: []byte("world")},
			{Key: []byte("hello1"), Value: []byte("world1")},
		}},
	}}
	require.NoError(t, db.ApplyChangeSets(cs))
	_, err = db.Commit()
	require.NoError(t, err)

	// Take a Copy that the trace baker would hold for the snapshot window.
	held := db.Copy()
	defer func() { _ = held.Close() }()

	// Force a snapshot rewrite, then drive memiavl through reload by
	// committing again — that path swaps the live snapshot and Closes the
	// old one. With refcounting, our held copy keeps the old snapshot
	// mapped; without it, this is exactly when it segfaults on the read
	// below.
	require.NoError(t, db.RewriteSnapshot(context.Background()))
	require.NoError(t, db.Reload())

	// Apply more changes so the live tree diverges, ensuring our held
	// copy is genuinely on the now-old version (not silently sharing
	// the same root).
	cs2 := []*proto.NamedChangeSet{{
		Name: "test",
		Changeset: iavl.ChangeSet{Pairs: []*iavl.KVPair{
			{Key: []byte("hello"), Value: []byte("OVERWRITTEN")},
		}},
	}}
	require.NoError(t, db.ApplyChangeSets(cs2))
	_, err = db.Commit()
	require.NoError(t, err)

	// Read from the held copy. Pre-fix this segfaults.
	tree := held.TreeByName("test")
	require.NotNil(t, tree)
	require.Equal(t, "world", string(tree.Get([]byte("hello"))))
	require.Equal(t, "world1", string(tree.Get([]byte("hello1"))))
}

// A second copy held alongside the first must also survive both the
// rewrite-reload of the live tree AND the close of its sibling. This
// covers the multi-window-snapshot case the trace baker actually runs.
func TestMultipleCopiesIndependentLifecycle(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(0, Options{
		Config:          Config{SnapshotKeepRecent: 0},
		Dir:             dir,
		CreateIfMissing: true,
		InitialStores:   []string{"test"},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	require.NoError(t, db.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name: "test",
		Changeset: iavl.ChangeSet{Pairs: []*iavl.KVPair{
			{Key: []byte("k"), Value: []byte("v")},
		}},
	}}))
	_, err = db.Commit()
	require.NoError(t, err)

	copyA := db.Copy()
	copyB := db.Copy()

	require.NoError(t, db.RewriteSnapshot(context.Background()))
	require.NoError(t, db.Reload())

	// Close the first copy; the second must keep working.
	require.NoError(t, copyA.Close())

	tree := copyB.TreeByName("test")
	require.NotNil(t, tree)
	require.Equal(t, "v", string(tree.Get([]byte("k"))))

	require.NoError(t, copyB.Close())
}

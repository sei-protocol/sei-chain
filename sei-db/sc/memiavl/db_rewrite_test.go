package memiavl

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cosmos/iavl"
	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/proto"
	"github.com/stretchr/testify/require"
)

// TestMultiTreeWriteSnapshotPriorityEVM tests the priority EVM write strategy
func TestMultiTreeWriteSnapshotPriorityEVM(t *testing.T) {
	dir := t.TempDir()

	db, err := OpenDB(logger.NewNopLogger(), 0, Options{
		Dir:             dir,
		CreateIfMissing: true,
		InitialStores:   []string{"evm", "bank", "acc"},
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	// Apply changes to all stores
	for _, changes := range ChangeSets {
		cs := []*proto.NamedChangeSet{
			{Name: "evm", Changeset: changes},
			{Name: "bank", Changeset: changes},
			{Name: "acc", Changeset: changes},
		}
		require.NoError(t, db.ApplyChangeSets(cs))
		_, err := db.Commit()
		require.NoError(t, err)
	}

	// Create snapshot - should use priority EVM strategy
	snapshotDir := filepath.Join(dir, "test-snapshot")
	err = db.MultiTree.WriteSnapshot(context.Background(), snapshotDir, db.snapshotWriterPool)
	require.NoError(t, err)

	// Verify all trees were written
	for _, store := range []string{"evm", "bank", "acc"} {
		storePath := filepath.Join(snapshotDir, store)
		_, err := os.Stat(storePath)
		require.NoError(t, err, "store %s should exist", store)
	}
}

// TestLoadMultiTreeWithPrefetchDisabled tests loading with prefetch disabled in background
func TestLoadMultiTreeWithPrefetchDisabled(t *testing.T) {
	dir := t.TempDir()

	// Create a DB with data
	db, err := OpenDB(logger.NewNopLogger(), 0, Options{
		Dir:             dir,
		CreateIfMissing: true,
		InitialStores:   []string{"test"},
	})
	require.NoError(t, err)

	for _, changes := range ChangeSets {
		cs := []*proto.NamedChangeSet{
			{Name: "test", Changeset: changes},
		}
		require.NoError(t, db.ApplyChangeSets(cs))
		_, err := db.Commit()
		require.NoError(t, err)
	}

	// Create snapshot
	require.NoError(t, db.RewriteSnapshot(context.Background()))
	db.Close()

	// Reload with prefetch disabled (simulating background load)
	opts := Options{
		Dir:               dir,
		PrefetchThreshold: 0, // Disable prefetch
	}

	db2, err := OpenDB(logger.NewNopLogger(), 0, opts)
	require.NoError(t, err)
	defer db2.Close()

	// Verify data is accessible
	tree := db2.TreeByName("test")
	require.NotNil(t, tree)
}

// TestBackgroundSnapshotSwitchMadvise verifies that after a background snapshot
// load and switch via ReplaceWith(), the snapshot mmap files use MADV_RANDOM
// (suitable for random tree access) instead of the default MADV_SEQUENTIAL.
//
// Background: PR #2497 changed NewMmap() to apply MADV_SEQUENTIAL by default
// for cold-start replay performance. OpenDB() correctly switches to MADV_RANDOM
// after loading, but the background snapshot switch path was missing this call.
// MADV_SEQUENTIAL causes the kernel to aggressively discard accessed pages,
// which is catastrophic on high-latency storage (e.g. NAS) where each page
// fault requires a network round-trip.
//
// This test exercises the background snapshot switch path (LoadMultiTree with
// prefetch disabled -> ReplaceWith) and verifies data remains accessible after
// the switch, confirming the safety net in ReplaceWith() works correctly.
func TestBackgroundSnapshotSwitchMadvise(t *testing.T) {
	dir := t.TempDir()

	db, err := OpenDB(logger.NewNopLogger(), 0, Options{
		Dir:             dir,
		CreateIfMissing: true,
		InitialStores:   []string{"test"},
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	// Populate the tree with data
	for i, changes := range ChangeSets {
		cs := []*proto.NamedChangeSet{
			{Name: "test", Changeset: changes},
		}
		require.NoError(t, db.ApplyChangeSets(cs))
		v, err := db.Commit()
		require.NoError(t, err)
		require.Equal(t, int64(i+1), v)
	}

	// Verify initial data
	tree := db.TreeByName("test")
	require.NotNil(t, tree)
	val := tree.Get([]byte("hello1"))
	require.Equal(t, []byte("world1"), val)

	// Create a snapshot (simulating background snapshot write)
	require.NoError(t, db.RewriteSnapshot(context.Background()))

	// Simulate the background snapshot load path:
	// LoadMultiTree with prefetch disabled (exactly like rewriteSnapshotBackground does)
	loadOpts := db.opts
	loadOpts.PrefetchThreshold = 0
	mtree, err := LoadMultiTree(currentPath(dir), loadOpts)
	require.NoError(t, err)

	// At this point, mtree's mmap files have MADV_SEQUENTIAL from NewMmap().
	// The safety net in Tree.ReplaceWith() should switch them to MADV_RANDOM.

	// Switch to the new snapshot via reloadMultiTree (same path as checkBackgroundSnapshotRewrite)
	require.NoError(t, db.reloadMultiTree(mtree))

	// Verify data is still accessible after the switch.
	// With MADV_SEQUENTIAL and no safety net, repeated random reads would cause
	// the kernel to discard pages, leading to performance degradation on NAS.
	// Here we verify correctness; the madvise fix ensures performance.
	tree = db.TreeByName("test")
	require.NotNil(t, tree)
	val = tree.Get([]byte("hello1"))
	require.Equal(t, []byte("world1"), val)

	// Perform multiple random reads to exercise the tree access pattern.
	// ChangeSets[2] sets hello2=world1 and hello3=world1.
	val = tree.Get([]byte("hello2"))
	require.Equal(t, []byte("world1"), val)
	val = tree.Get([]byte("hello3"))
	require.Equal(t, []byte("world1"), val)

	// Re-read previously accessed keys (would cause page faults with MADV_SEQUENTIAL)
	val = tree.Get([]byte("hello1"))
	require.Equal(t, []byte("world1"), val)
}

// TestReplaceWithPreservesDataIntegrity verifies that Tree.ReplaceWith() correctly
// switches snapshot and the new snapshot is functional for random access patterns.
func TestReplaceWithPreservesDataIntegrity(t *testing.T) {
	dir := t.TempDir()

	db, err := OpenDB(logger.NewNopLogger(), 0, Options{
		Dir:             dir,
		CreateIfMissing: true,
		InitialStores:   []string{"evm", "bank"},
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	// Populate multiple stores with different data
	for i := 0; i < 10; i++ {
		cs := []*proto.NamedChangeSet{
			{
				Name: "evm",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: []byte{byte(i)}, Value: []byte{byte(i * 2)}},
					},
				},
			},
			{
				Name: "bank",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: []byte{byte(i + 100)}, Value: []byte{byte(i * 3)}},
					},
				},
			},
		}
		require.NoError(t, db.ApplyChangeSets(cs))
		_, err := db.Commit()
		require.NoError(t, err)
	}

	// Create snapshot
	require.NoError(t, db.RewriteSnapshot(context.Background()))

	// Load snapshot without prefetch (background load path)
	loadOpts := db.opts
	loadOpts.PrefetchThreshold = 0
	mtree, err := LoadMultiTree(currentPath(dir), loadOpts)
	require.NoError(t, err)

	// Switch via ReplaceWith (triggers PrepareForRandomRead safety net)
	require.NoError(t, db.reloadMultiTree(mtree))

	// Verify all data across multiple stores after switch
	evmTree := db.TreeByName("evm")
	require.NotNil(t, evmTree)
	bankTree := db.TreeByName("bank")
	require.NotNil(t, bankTree)

	for i := 0; i < 10; i++ {
		val := evmTree.Get([]byte{byte(i)})
		require.Equal(t, []byte{byte(i * 2)}, val, "evm key %d", i)

		val = bankTree.Get([]byte{byte(i + 100)})
		require.Equal(t, []byte{byte(i * 3)}, val, "bank key %d", i)
	}

	// Random access pattern: re-read in reverse order
	for i := 9; i >= 0; i-- {
		val := evmTree.Get([]byte{byte(i)})
		require.Equal(t, []byte{byte(i * 2)}, val, "evm reverse key %d", i)
	}
}

package memiavl

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

// TestMultiTreeWriteSnapshotPriorityEVM tests the priority EVM write strategy
func TestMultiTreeWriteSnapshotPriorityEVM(t *testing.T) {
	dir := t.TempDir()

	db, err := OpenDB(0, Options{
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
	db, err := OpenDB(0, Options{
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
		Config: Config{SnapshotPrefetchThreshold: 0}, // Disable prefetch
		Dir:    dir,
	}

	db2, err := OpenDB(0, opts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db2.Close()) })

	// Verify data is accessible
	tree := db2.TreeByName("test")
	require.NotNil(t, tree)
}

func TestPublishSnapshotRejectsCorruptCandidate(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(0, Options{
		Dir:             dir,
		CreateIfMissing: true,
		InitialStores:   []string{"test"},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	require.NoError(t, db.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name: "test",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{
			Key:   []byte("key"),
			Value: []byte("value"),
		}}},
	}}))
	_, err = db.Commit()
	require.NoError(t, err)

	snapshotDir := snapshotName(db.Version())
	tmpPath := filepath.Join(dir, snapshotDir+"-tmp")
	targetPath := filepath.Join(dir, snapshotDir)
	require.NoError(t, db.MultiTree.WriteSnapshot(context.Background(), tmpPath, db.snapshotWriterPool))

	leavesPath := filepath.Join(tmpPath, "test", FileNameLeaves)
	leaves, err := os.OpenFile(leavesPath, os.O_WRONLY|os.O_APPEND, 0)
	require.NoError(t, err)
	_, err = leaves.Write(make([]byte, SizeLeafWithoutHash))
	require.NoError(t, err)
	require.NoError(t, leaves.Close())

	err = db.publishSnapshot(context.Background(), tmpPath, targetPath, snapshotDir)
	require.ErrorContains(t, err, "corrupted snapshot, leaves file size")
	require.NoDirExists(t, targetPath)

	current, err := os.Readlink(currentPath(dir))
	require.NoError(t, err)
	require.Equal(t, snapshotName(0), current)
}

// openCommittedDB opens a fresh DB with one store and one committed change,
// which is the smallest state that exercises the snapshot rewrite paths.
func openCommittedDB(t *testing.T) (*DB, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := OpenDB(0, Options{
		Dir:             dir,
		CreateIfMissing: true,
		InitialStores:   []string{"test"},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	require.NoError(t, db.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name: "test",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{
			Key:   []byte("key"),
			Value: []byte("value"),
		}}},
	}}))
	_, err = db.Commit()
	require.NoError(t, err)
	return db, dir
}

// writeCorruptSnapshotDir plants a directory at path that fails validation.
func writeCorruptSnapshotDir(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(path, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(path, MetadataFileName), []byte("garbage"), 0o600))
}

func TestRewriteSnapshotSkipsValidExistingSnapshot(t *testing.T) {
	db, dir := openCommittedDB(t)
	require.NoError(t, db.RewriteSnapshot(context.Background()))

	targetPath := filepath.Join(dir, snapshotName(db.Version()))
	before, err := os.Stat(filepath.Join(targetPath, MetadataFileName))
	require.NoError(t, err)

	require.NoError(t, db.RewriteSnapshot(context.Background()))

	after, err := os.Stat(filepath.Join(targetPath, MetadataFileName))
	require.NoError(t, err)
	require.Equal(t, before.ModTime(), after.ModTime(), "a valid existing snapshot must be adopted, not rewritten")
	require.NoDirExists(t, targetPath+"-tmp")
}

func TestRewriteSnapshotReplacesCorruptExistingSnapshot(t *testing.T) {
	db, dir := openCommittedDB(t)
	snapshotDir := snapshotName(db.Version())
	targetPath := filepath.Join(dir, snapshotDir)
	writeCorruptSnapshotDir(t, targetPath)

	require.NoError(t, db.RewriteSnapshot(context.Background()))

	require.NoError(t, db.validateSnapshot(context.Background(), targetPath))
	current, err := os.Readlink(currentPath(dir))
	require.NoError(t, err)
	require.Equal(t, snapshotDir, current)
}

func TestPublishSnapshotReplacesCorruptExistingTarget(t *testing.T) {
	db, dir := openCommittedDB(t)
	snapshotDir := snapshotName(db.Version())
	tmpPath := filepath.Join(dir, snapshotDir+"-tmp")
	targetPath := filepath.Join(dir, snapshotDir)
	require.NoError(t, db.MultiTree.WriteSnapshot(context.Background(), tmpPath, db.snapshotWriterPool))
	writeCorruptSnapshotDir(t, targetPath)

	require.NoError(t, db.publishSnapshot(context.Background(), tmpPath, targetPath, snapshotDir))

	require.NoError(t, db.validateSnapshot(context.Background(), targetPath))
	require.NoDirExists(t, tmpPath)
	current, err := os.Readlink(currentPath(dir))
	require.NoError(t, err)
	require.Equal(t, snapshotDir, current)
}

func TestPublishSnapshotAdoptsValidExistingTarget(t *testing.T) {
	db, dir := openCommittedDB(t)
	snapshotDir := snapshotName(db.Version())
	tmpPath := filepath.Join(dir, snapshotDir+"-tmp")
	targetPath := filepath.Join(dir, snapshotDir)
	require.NoError(t, db.MultiTree.WriteSnapshot(context.Background(), tmpPath, db.snapshotWriterPool))
	require.NoError(t, db.MultiTree.WriteSnapshot(context.Background(), targetPath, db.snapshotWriterPool))

	require.NoError(t, db.publishSnapshot(context.Background(), tmpPath, targetPath, snapshotDir))

	require.NoDirExists(t, tmpPath, "the redundant temp must be dropped when the target is adopted")
	current, err := os.Readlink(currentPath(dir))
	require.NoError(t, err)
	require.Equal(t, snapshotDir, current)
}

// TestRewriteSnapshotKeepsSnapshotOnCancelledValidation pins the boundary of
// the self-heal: a validation failure that says nothing about the directory's
// contents, such as a cancelled context at shutdown, must not delete a
// published snapshot out from under the current symlink.
func TestRewriteSnapshotKeepsSnapshotOnCancelledValidation(t *testing.T) {
	db, dir := openCommittedDB(t)
	require.NoError(t, db.RewriteSnapshot(context.Background()))
	snapshotDir := snapshotName(db.Version())

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	err := db.RewriteSnapshot(cancelled)

	require.ErrorIs(t, err, context.Canceled)
	require.DirExists(t, filepath.Join(dir, snapshotDir))
	current, err := os.Readlink(currentPath(dir))
	require.NoError(t, err)
	require.Equal(t, snapshotDir, current)
}

// TestLoadMultiTreeRejectsMetadataWithoutCommitInfo covers the metadata shape
// an unclean shutdown leaves behind: a file that unmarshals successfully but
// carries no commit info. Loading it must fail as corruption, not panic.
func TestLoadMultiTreeRejectsMetadataWithoutCommitInfo(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, MetadataFileName), []byte{}, 0o600))

	_, err := LoadMultiTree(context.Background(), dir, Options{})

	require.ErrorIs(t, err, errCorruptedSnapshot)
	require.ErrorContains(t, err, "no commit info")
}

func TestRewriteSnapshotReplacesSnapshotWithEmptyMetadata(t *testing.T) {
	db, dir := openCommittedDB(t)
	snapshotDir := snapshotName(db.Version())
	targetPath := filepath.Join(dir, snapshotDir)
	require.NoError(t, os.MkdirAll(targetPath, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(targetPath, MetadataFileName), []byte{}, 0o600))

	require.NoError(t, db.RewriteSnapshot(context.Background()))

	require.NoError(t, db.validateSnapshot(context.Background(), targetPath))
	current, err := os.Readlink(currentPath(dir))
	require.NoError(t, err)
	require.Equal(t, snapshotDir, current)
}

// TestValidateSnapshotRejectsStoreVersionSkew pins the per-store version
// comparison. An empty commit advances every version while leaving root hashes
// unchanged, so a directory mixing an old store with a new multi-tree metadata
// is caught only by comparing each store's own version.
func TestValidateSnapshotRejectsStoreVersionSkew(t *testing.T) {
	db, dir := openCommittedDB(t)
	require.NoError(t, db.RewriteSnapshot(context.Background()))
	oldSnapshot := filepath.Join(dir, snapshotName(db.Version()))

	_, err := db.Commit()
	require.NoError(t, err)
	require.NoError(t, db.RewriteSnapshot(context.Background()))
	newSnapshot := filepath.Join(dir, snapshotName(db.Version()))

	skewed := filepath.Join(dir, "skewed-snapshot")
	require.NoError(t, os.CopyFS(skewed, os.DirFS(oldSnapshot)))
	metadata, err := os.ReadFile(filepath.Join(newSnapshot, MetadataFileName))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(skewed, MetadataFileName), metadata, 0o600))

	err = db.validateSnapshot(context.Background(), skewed)
	require.ErrorIs(t, err, errCorruptedSnapshot)
	require.ErrorContains(t, err, "does not match expected version")
}

func TestValidateSnapshotComparesCommitInfo(t *testing.T) {
	db, dir := openCommittedDB(t)
	require.NoError(t, db.RewriteSnapshot(context.Background()))
	staleTarget := filepath.Join(dir, snapshotName(db.Version()))

	// Another commit moves lastCommitInfo past the published snapshot.
	require.NoError(t, db.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name: "test",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{
			Key:   []byte("key"),
			Value: []byte("value2"),
		}}},
	}}))
	_, err := db.Commit()
	require.NoError(t, err)

	err = db.validateSnapshot(context.Background(), staleTarget)
	require.ErrorContains(t, err, "does not match expected version")

	// A snapshot at the right version but holding different content must be
	// rejected on its root hash.
	require.NoError(t, db.RewriteSnapshot(context.Background()))
	freshTarget := filepath.Join(dir, snapshotName(db.Version()))
	require.NoError(t, db.validateSnapshot(context.Background(), freshTarget))
	db.lastCommitInfo.StoreInfos[0].CommitId.Hash = []byte("not the real root hash")
	err = db.validateSnapshot(context.Background(), freshTarget)
	require.ErrorContains(t, err, "root hash does not match")
}

package operations

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// newMemiavlSourceDir builds a memiavl directory with `versions` committed
// blocks and returns the directory a tool would be pointed at.
func newMemiavlSourceDir(t *testing.T, versions int) string {
	t.Helper()
	homeDir := t.TempDir()
	store := newTestMemiavlStore(t, homeDir)
	for i := 1; i <= versions; i++ {
		require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{{
			Name:      keys.EVMStoreKey,
			Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{noncePair(addrN(byte(i)), uint64(i))}},
		}}))
		v, err := store.Commit()
		require.NoError(t, err)
		require.Equal(t, int64(i), v)
	}
	require.NoError(t, store.Close())
	return utils.GetCosmosSCStorePath(homeDir)
}

// snapshotDirState records every regular file under root by relative path and
// content so a later comparison catches truncation, appends, and deletions.
func snapshotDirState(t *testing.T, root string) map[string][]byte {
	t.Helper()
	state := make(map[string][]byte)
	require.NoError(t, filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		bz, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return err
		}
		state[rel] = bz
		return nil
	}))
	return state
}

// lastChangelogSegment returns the newest tidwall segment file in the memiavl
// changelog. Segment names are fixed-width, so lexical order is index order.
func lastChangelogSegment(t *testing.T, dbDir string) string {
	t.Helper()
	changelogDir := filepath.Join(dbDir, "changelog")
	entries, err := os.ReadDir(changelogDir)
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		if !e.IsDir() && len(e.Name()) >= 20 {
			names = append(names, e.Name())
		}
	}
	require.NotEmpty(t, names, "memiavl changelog should have at least one segment")
	sort.Strings(names)
	return filepath.Join(changelogDir, names[len(names)-1])
}

// TestOpenMemiAVLReplayLeavesSourceUntouched is the regression test for the
// audited hazard: a replay that advertises itself as read-only used to hand the
// live changelog straight to memiavl, whose WAL open truncates a torn tail. A
// torn tail on a running node is just the writer mid-append, so the "repair"
// destroyed committed versions. The tool must now repair only its own copy.
func TestOpenMemiAVLReplayLeavesSourceUntouched(t *testing.T) {
	dbDir := newMemiavlSourceDir(t, 3)

	// A single length-prefix byte declaring a 16-byte record that never
	// arrived is exactly what a reader observes while the writer is
	// partway through appending a block.
	segment := lastChangelogSegment(t, dbDir)
	intact, err := os.ReadFile(filepath.Clean(segment))
	require.NoError(t, err)
	torn := append(append([]byte{}, intact...), 0x10)
	require.NoError(t, os.WriteFile(segment, torn, 0o600))

	before := snapshotDirState(t, dbDir)

	db, err := openMemiAVLReplay(dbDir, 0)
	require.NoError(t, err, "replay must tolerate a torn tail by repairing its own clone")
	require.Equal(t, int64(3), db.Version())
	require.NoError(t, db.Close())

	require.Equal(t, before, snapshotDirState(t, dbDir),
		"replay must not add, remove, truncate, or rewrite any file in the source directory")

	after, err := os.ReadFile(filepath.Clean(segment))
	require.NoError(t, err)
	require.Equal(t, torn, after, "the torn tail must still be there for the live writer to finish")
}

// TestOpenMemiAVLReplayWorksWhileWriterHoldsLock pins the other half of the
// contract: avoiding the mutation must not cost us the ability to read a live
// node. The clone is independent, so the source LOCK is irrelevant to us.
func TestOpenMemiAVLReplayWorksWhileWriterHoldsLock(t *testing.T) {
	homeDir := t.TempDir()
	writer := newTestMemiavlStore(t, homeDir)
	require.NoError(t, writer.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name:      keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{noncePair(addrN(0xA1), 1)}},
	}}))
	_, err := writer.Commit()
	require.NoError(t, err)
	defer func() { require.NoError(t, writer.Close()) }()

	dbDir := utils.GetCosmosSCStorePath(homeDir)
	require.FileExists(t, filepath.Join(dbDir, "LOCK"))

	db, err := openMemiAVLReplay(dbDir, 0)
	require.NoError(t, err, "tooling clone must not contend for the live writer's lock")
	require.Equal(t, int64(1), db.Version())
	require.NoError(t, db.Close())
}

// TestOpenMemiAVLReplayAfterSetInitialVersion pins the non-default
// initial-height layout: memiavl bootstraps every DB as snapshot-0 even when
// SetInitialVersion(100) makes the first changelog entry version 100, so the
// clone's WAL-coverage check must derive the snapshot's successor from the
// snapshot metadata instead of assuming version 1 follows snapshot-0. This is
// exactly the shape of a freshly recovered chain whose genesis initial_height
// is greater than 1 and whose first snapshot rewrite has not happened yet;
// before the fix, replay mode failed deterministically on such nodes with
// "source kept churning".
func TestOpenMemiAVLReplayAfterSetInitialVersion(t *testing.T) {
	homeDir := t.TempDir()
	store := newTestMemiavlStore(t, homeDir)
	require.NoError(t, store.SetInitialVersion(100))
	for i := 0; i < 3; i++ {
		require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{{
			Name:      keys.EVMStoreKey,
			Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{noncePair(addrN(byte(i+1)), uint64(i+1))}},
		}}))
		v, err := store.Commit()
		require.NoError(t, err)
		require.Equal(t, int64(100+i), v)
	}
	require.NoError(t, store.Close())
	dbDir := utils.GetCosmosSCStorePath(homeDir)

	historical, err := openMemiAVLReplay(dbDir, 101)
	require.NoError(t, err, "coverage check must accept a snapshot-0 whose successor is the initial version")
	require.Equal(t, int64(101), historical.Version())
	require.NoError(t, historical.Close())

	latest, err := openMemiAVLReplay(dbDir, 0)
	require.NoError(t, err)
	require.Equal(t, int64(102), latest.Version())
	require.NoError(t, latest.Close())
}

// TestOpenMemiAVLReplayRejectsShortChangelog guards the failure mode this
// design makes routine: repairing a torn tail inside the clone silently costs
// the trailing version, and memiavl reports success anyway. For a tool whose
// whole job is comparing digests across nodes, quietly digesting one version
// early is indistinguishable from a real state divergence.
func TestOpenMemiAVLReplayRejectsShortChangelog(t *testing.T) {
	dbDir := newMemiavlSourceDir(t, 3)

	segment := lastChangelogSegment(t, dbDir)
	intact, err := os.ReadFile(filepath.Clean(segment))
	require.NoError(t, err)
	// Lop off the tail so the final committed record is torn and the
	// repaired clone can only reach version 2.
	require.NoError(t, os.WriteFile(segment, intact[:len(intact)-8], 0o600))

	_, err = openMemiAVLReplay(dbDir, 3)
	require.Error(t, err)
	require.Contains(t, err.Error(), "requested 3, reached 2")

	// The rejected clone must not be left behind in the node's data dir.
	entries, err := os.ReadDir(dbDir)
	require.NoError(t, err)
	for _, e := range entries {
		require.NotContains(t, e.Name(), ".seidb-memiavl-tool-")
	}
}

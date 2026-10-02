package memiavl

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
)

const (
	changelogRangeTree       = "evm"
	changelogRangeOtherTree  = "other"
	changelogRangeSnapshot   = int64(3)
	changelogRangeTipVersion = int64(6)
)

// changelogRangeBlocks are the evm changesets of versions 1 to 6. The blocks after the snapshot
// at version 3 overwrite, delete, resurrect, set then delete, delete an absent key, and write a
// nil value.
var changelogRangeBlocks = [][]*proto.KVPair{
	{{Key: []byte("a"), Value: []byte("1")}, {Key: []byte("b"), Value: []byte("1")}},
	{{Key: []byte("c"), Value: []byte("2")}},
	{{Key: []byte("a"), Value: []byte("3")}},
	{{Key: []byte("b"), Delete: true}, {Key: []byte("d"), Value: []byte("4")}, {Key: []byte("e"), Value: []byte("4")}, {Key: []byte("e"), Delete: true}},
	{{Key: []byte("b"), Value: []byte("5")}, {Key: []byte("z"), Delete: true}, {Key: []byte("f")}},
	{{Key: []byte("a"), Value: []byte("6")}, {Key: []byte("d"), Delete: true}},
}

func openChangelogRangeDB(t *testing.T, dir string, initialStores ...string) *DB {
	t.Helper()
	db, err := OpenDB(0, Options{
		Config:          Config{SnapshotInterval: 1000},
		Dir:             dir,
		CreateIfMissing: true,
		InitialStores:   initialStores,
	})
	require.NoError(t, err)
	return db
}

func commitChangelogRangeBlock(t *testing.T, db *DB, changeSets ...*proto.NamedChangeSet) {
	t.Helper()
	require.NoError(t, db.ApplyChangeSets(append(changeSets, &proto.NamedChangeSet{
		Name:      changelogRangeOtherTree,
		Changeset: proto.ChangeSet{Pairs: mockKVPairs("x", "y")},
	})))
	_, err := db.Commit()
	require.NoError(t, err)
}

// buildChangelogRangeDB writes changelogRangeBlocks with a snapshot at changelogRangeSnapshot.
func buildChangelogRangeDB(t *testing.T) string {
	t.Helper()
	return buildChangelogRangeDBWithSnapshots(t, changelogRangeSnapshot)
}

// buildChangelogRangeDBWithSnapshots writes changelogRangeBlocks with a snapshot at each of
// snapshots.
func buildChangelogRangeDBWithSnapshots(t *testing.T, snapshots ...int64) string {
	t.Helper()
	dir := t.TempDir()
	db := openChangelogRangeDB(t, dir, changelogRangeTree, changelogRangeOtherTree)
	for i, pairs := range changelogRangeBlocks {
		commitChangelogRangeBlock(t, db, &proto.NamedChangeSet{Name: changelogRangeTree, Changeset: proto.ChangeSet{Pairs: pairs}})
		if slices.Contains(snapshots, int64(i+1)) {
			require.NoError(t, db.RewriteSnapshot(context.Background()))
		}
	}
	require.NoError(t, db.Close())
	return dir
}

func readTreeLeaves(t *testing.T, dir string, version int64) (map[string]string, int64) {
	t.Helper()
	db, err := OpenDB(version, Options{Dir: dir, ReadOnly: true, NoChangelogRepair: true})
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()
	leaves := make(map[string]string)
	iter := db.TreeByName(changelogRangeTree).Iterator(nil, nil, true)
	defer func() { require.NoError(t, iter.Close()) }()
	for ; iter.Valid(); iter.Next() {
		leaves[string(iter.Key())] = string(iter.Value())
	}
	return leaves, db.Version()
}

func TestReplayTreeChangelogMatchesOpenDB(t *testing.T) {
	dir := buildChangelogRangeDB(t)
	snapshotLeaves, _ := readTreeLeaves(t, dir, changelogRangeSnapshot)

	for _, target := range []int64{changelogRangeSnapshot, 4, 5, changelogRangeTipVersion, 0} {
		want, wantVersion := readTreeLeaves(t, dir, target)

		got := make(map[string]string, len(snapshotLeaves))
		for k, v := range snapshotLeaves {
			got[k] = v
		}
		r, err := ReplayTreeChangelog(dir, target, changelogRangeTree, func(_ int64, cs proto.ChangeSet) error {
			for _, pair := range cs.Pairs {
				if pair.Delete {
					delete(got, string(pair.Key))
				} else {
					got[string(pair.Key)] = string(pair.Value)
				}
			}
			return nil
		})
		require.NoError(t, err, "target %d", target)
		require.Equal(t, changelogRangeSnapshot, r.SnapshotVersion, "target %d", target)
		require.Equal(t, wantVersion, r.Version, "target %d", target)
		require.Equal(t, want, got, "target %d", target)
	}
}

func TestReplayTreeChangelogStartsFromTheSnapshotOpenDBLoads(t *testing.T) {
	dir := buildChangelogRangeDBWithSnapshots(t, 2, 4)
	for _, snapshot := range []int64{2, 4} {
		_, err := os.Stat(filepath.Join(dir, snapshotName(snapshot)))
		require.NoError(t, err, "snapshot %d", snapshot)
	}

	for target, wantSnapshot := range map[int64]int64{3: 2, 5: 4} {
		want, wantVersion := readTreeLeaves(t, dir, target)
		got, _ := readTreeLeaves(t, dir, wantSnapshot)
		r, err := ReplayTreeChangelog(dir, target, changelogRangeTree, func(_ int64, cs proto.ChangeSet) error {
			for _, pair := range cs.Pairs {
				if pair.Delete {
					delete(got, string(pair.Key))
				} else {
					got[string(pair.Key)] = string(pair.Value)
				}
			}
			return nil
		})
		require.NoError(t, err, "target %d", target)
		require.Equal(t, filepath.Join(dir, snapshotName(wantSnapshot)), r.SnapshotDir, "target %d", target)
		require.Equal(t, wantSnapshot, r.SnapshotVersion, "target %d", target)
		require.Equal(t, wantSnapshot+1, r.StartVersion, "target %d", target)
		require.Equal(t, wantVersion, r.Version, "target %d", target)
		require.Equal(t, want, got, "target %d", target)
	}
}

func TestReplayTreeChangelogPassesVersionsInOrder(t *testing.T) {
	dir := buildChangelogRangeDB(t)
	var versions []int64
	_, err := ReplayTreeChangelog(dir, 0, changelogRangeTree, func(version int64, _ proto.ChangeSet) error {
		versions = append(versions, version)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, []int64{4, 5, 6}, versions)
}

func TestReplayTreeChangelogRefusesChangelogEndingBelowTarget(t *testing.T) {
	dir := buildChangelogRangeDB(t)
	_, err := ReplayTreeChangelog(dir, changelogRangeTipVersion+1, changelogRangeTree, nopChangeSet)
	require.ErrorContains(t, err, "changelog ends below version 7")
}

func TestReplayTreeChangelogRefusesChangelogStartingAboveSnapshot(t *testing.T) {
	dir := buildChangelogRangeDB(t)
	stream, err := wal.NewChangelogWAL(utils.GetChangelogPath(dir), wal.Config{})
	require.NoError(t, err)
	require.NoError(t, stream.TruncateBefore(5))
	require.NoError(t, stream.Close())

	_, err = ReplayTreeChangelog(dir, changelogRangeTipVersion, changelogRangeTree, nopChangeSet)
	require.ErrorContains(t, err, "changelog starts above version 4")
	_, err = ReplayTreeChangelog(dir, changelogRangeSnapshot, changelogRangeTree, nopChangeSet)
	require.NoError(t, err, "a target at the snapshot needs no changelog")
}

func TestReplayTreeChangelogRefusesUpgradesOfTheTree(t *testing.T) {
	for name, tc := range map[string]struct {
		initialStores []string
		upgrade       *proto.TreeNameUpgrade
	}{
		"add":         {[]string{changelogRangeOtherTree}, &proto.TreeNameUpgrade{Name: changelogRangeTree}},
		"delete":      {[]string{changelogRangeTree, changelogRangeOtherTree}, &proto.TreeNameUpgrade{Name: changelogRangeTree, Delete: true}},
		"rename-from": {[]string{changelogRangeTree, changelogRangeOtherTree}, &proto.TreeNameUpgrade{Name: "renamed", RenameFrom: changelogRangeTree}},
		"rename-to":   {[]string{"source", changelogRangeOtherTree}, &proto.TreeNameUpgrade{Name: changelogRangeTree, RenameFrom: "source"}},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			db := openChangelogRangeDB(t, dir, tc.initialStores...)
			commitChangelogRangeBlock(t, db)
			require.NoError(t, db.RewriteSnapshot(context.Background()))
			require.NoError(t, db.ApplyUpgrades([]*proto.TreeNameUpgrade{tc.upgrade}))
			commitChangelogRangeBlock(t, db)
			require.NoError(t, db.Close())

			_, err := ReplayTreeChangelog(dir, 2, changelogRangeTree, nopChangeSet)
			require.ErrorContains(t, err, `changelog version 2 upgrades tree "evm"`)
		})
	}
}

func TestReplayTreeChangelogAcceptsEmptyChangelogWithNothingToReplay(t *testing.T) {
	dir := buildChangelogRangeDB(t)
	require.NoError(t, os.RemoveAll(utils.GetChangelogPath(dir)))

	for _, target := range []int64{changelogRangeSnapshot, 0} {
		calls := 0
		r, err := ReplayTreeChangelog(dir, target, changelogRangeTree, func(int64, proto.ChangeSet) error {
			calls++
			return nil
		})
		require.NoError(t, err, "target %d", target)
		require.Equal(t, changelogRangeSnapshot, r.Version, "target %d", target)
		require.Zero(t, calls, "target %d", target)
	}
	_, err := ReplayTreeChangelog(dir, changelogRangeSnapshot+1, changelogRangeTree, nopChangeSet)
	require.ErrorContains(t, err, "changelog ends below version 4: the changelog is empty")
}

func nopChangeSet(int64, proto.ChangeSet) error { return nil }

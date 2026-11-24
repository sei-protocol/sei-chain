package mvcc

import (
	"bytes"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
)

const mergeTestStore = "receipt"

// newMergeTestDB opens an empty DB in a new directory, in the descending
// encoding or, when descending is false, as a legacy ascending DB.
func newMergeTestDB(t *testing.T, descending bool) (*Database, string) {
	t.Helper()
	dir := t.TempDir()
	if !descending {
		// A latest version without the descending marker is how OpenDB
		// recognises a legacy ascending DB.
		raw, err := pebble.Open(dir, &pebble.Options{Comparer: MVCCComparer})
		require.NoError(t, err)
		require.NoError(t, raw.Set([]byte(latestVersionKey), make([]byte, VersionSize), pebble.Sync))
		require.NoError(t, raw.Close())
	}
	db := openMergeTestDB(t, dir)
	require.Equal(t, descending, db.descending)
	return db, dir
}

// openMergeTestDB opens the DB at dir, to be closed when the test ends.
func openMergeTestDB(t *testing.T, dir string) *Database {
	t.Helper()
	cfg := config.DefaultStateStoreConfig()
	cfg.Backend = "pebbledb"
	store, err := OpenDB(dir, cfg)
	require.NoError(t, err)
	db := store.(*Database)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// sstTestKeys is the number of keys newSSTBackedDB writes.
const sstTestKeys = 1000

// sstTestKey returns the i-th key newSSTBackedDB writes.
func sstTestKey(i int) string {
	return fmt.Sprintf("key-%05d", i)
}

// newSSTBackedDB returns the directory of a closed DB, in the descending
// encoding or, when descending is false, as a legacy ascending DB, holding
// versions 1 to 3 of sstTestKeys keys under mergeTestStore, each value naming
// its key and version, flushed out of the memtable into SST files.
func newSSTBackedDB(t *testing.T, descending bool) string {
	t.Helper()
	db, dir := newMergeTestDB(t, descending)
	for v := int64(1); v <= 3; v++ {
		pairs := make([]*proto.KVPair, 0, sstTestKeys)
		for i := range sstTestKeys {
			key := sstTestKey(i)
			pairs = append(pairs, &proto.KVPair{Key: []byte(key), Value: []byte(fmt.Sprintf("%s@%d", key, v))})
		}
		require.NoError(t, db.ApplyChangesetSync(v, []*proto.NamedChangeSet{{
			Name:      mergeTestStore,
			Changeset: proto.ChangeSet{Pairs: pairs},
		}}))
	}
	require.NoError(t, db.storage.Flush())
	require.NoError(t, db.Close())
	ssts, err := filepath.Glob(filepath.Join(dir, "*.sst"))
	require.NoError(t, err)
	require.NotEmpty(t, ssts)
	return dir
}

func deleteVersion(t *testing.T, db *Database, store string, v int64, key []byte) {
	t.Helper()
	require.NoError(t, db.ApplyChangesetSync(v, []*proto.NamedChangeSet{{
		Name:      store,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: key}}},
	}}))
}

// writeChangelogEntry appends an entry at version to the changelog of the
// closed DB at dir.
func writeChangelogEntry(t *testing.T, dir string, version int64) {
	t.Helper()
	changelog, err := wal.NewChangelogWAL(utils.GetChangelogPath(dir), wal.Config{})
	require.NoError(t, err)
	require.NoError(t, changelog.Write(proto.ChangelogEntry{Version: version}))
	require.NoError(t, changelog.Close())
}

func requireGet(t *testing.T, db *Database, version int64, key string, want []byte) {
	t.Helper()
	got, err := db.Get(mergeTestStore, version, []byte(key))
	require.NoError(t, err)
	require.Equal(t, want, got, "%s at version %d", key, version)
}

// A merge copies every source version, deletions included, into the target in
// the target's encoding, and widens the target's version markers to span both.
func TestMergeCopiesEveryVersion(t *testing.T) {
	for _, tc := range []struct{ sourceDescending, targetDescending bool }{
		{false, false}, {false, true}, {true, false}, {true, true},
	} {
		name := fmt.Sprintf("source descending %v, target descending %v", tc.sourceDescending, tc.targetDescending)
		t.Run(name, func(t *testing.T) {
			source, sourceDir := newMergeTestDB(t, tc.sourceDescending)
			applyVersion(t, source, mergeTestStore, 10, []byte("a"), []byte("a@10"))
			applyVersion(t, source, mergeTestStore, 11, []byte("b"), []byte("b@11"))
			applyVersion(t, source, mergeTestStore, 12, []byte("a"), []byte("a@12"))
			deleteVersion(t, source, mergeTestStore, 13, []byte("b"))
			require.NoError(t, source.SetEarliestVersion(5, true))
			require.NoError(t, source.Close())

			target, targetDir := newMergeTestDB(t, tc.targetDescending)
			applyVersion(t, target, mergeTestStore, 100, []byte("c"), []byte("c@100"))
			applyVersion(t, target, mergeTestStore, 101, []byte("a"), []byte("a@101"))
			require.NoError(t, target.SetEarliestVersion(14, true))
			require.NoError(t, target.Close())

			report, err := Merge(targetDir, sourceDir, MergeOptions{StoreKeys: []string{mergeTestStore}, VerifyEvery: 1})
			require.NoError(t, err)
			require.Equal(t, int64(4), report.Entries)
			require.Equal(t, int64(4), report.Written)
			require.Equal(t, int64(4), report.Verified)
			require.Equal(t, []string{mergeTestStore}, report.Source.StoreKeys)

			merged := openMergeTestDB(t, targetDir)
			require.Equal(t, tc.targetDescending, merged.descending, "the target keeps its encoding")
			require.Equal(t, int64(5), merged.GetEarliestVersion())
			require.Equal(t, int64(101), merged.GetLatestVersion())
			requireGet(t, merged, 10, "a", []byte("a@10"))
			requireGet(t, merged, 11, "a", []byte("a@10"))
			requireGet(t, merged, 12, "a", []byte("a@12"))
			requireGet(t, merged, 100, "a", []byte("a@12"))
			requireGet(t, merged, 101, "a", []byte("a@101"))
			requireGet(t, merged, 12, "b", []byte("b@11"))
			requireGet(t, merged, 13, "b", nil)
			requireGet(t, merged, 101, "b", nil)
			requireGet(t, merged, 99, "c", nil)
			requireGet(t, merged, 100, "c", []byte("c@100"))

			unchanged := openMergeTestDB(t, sourceDir)
			require.Equal(t, int64(13), unchanged.GetLatestVersion())
			requireGet(t, unchanged, 101, "a", []byte("a@12"))
		})
	}
}

// Read through SST files, where pebble reuses its key buffers as the iterator
// moves, a merge still writes every entry as the source holds it and reads each
// one back, from a source in either encoding.
func TestMergeReadsBackEntriesFromSSTFiles(t *testing.T) {
	for _, sourceDescending := range []bool{false, true} {
		t.Run(fmt.Sprintf("source descending %v", sourceDescending), func(t *testing.T) {
			sourceDir := newSSTBackedDB(t, sourceDescending)
			target, targetDir := newMergeTestDB(t, true)
			applyVersion(t, target, mergeTestStore, 100, []byte("c"), []byte("c@100"))
			require.NoError(t, target.SetEarliestVersion(4, true))
			require.NoError(t, target.Close())

			report, err := Merge(targetDir, sourceDir, MergeOptions{VerifyEvery: 1})
			require.NoError(t, err)
			require.Equal(t, int64(3*sstTestKeys), report.Written)
			require.Equal(t, report.Written, report.Verified)

			merged := openMergeTestDB(t, targetDir)
			for i := range sstTestKeys {
				key := sstTestKey(i)
				for v := int64(1); v <= 3; v++ {
					requireGet(t, merged, v, key, []byte(fmt.Sprintf("%s@%d", key, v)))
				}
			}
		})
	}
}

// Entries beyond one batch are all committed.
func TestMergeCommitsEveryBatch(t *testing.T) {
	const valueSize = 1 << 20
	const versions = 2 * mergeBatchBytes / valueSize
	source, sourceDir := newMergeTestDB(t, true)
	value := bytes.Repeat([]byte{0xab}, valueSize)
	for v := int64(1); v <= versions; v++ {
		applyVersion(t, source, mergeTestStore, v, []byte("a"), value)
	}
	require.NoError(t, source.Close())
	target, targetDir := newMergeTestDB(t, true)
	applyVersion(t, target, mergeTestStore, 100, []byte("b"), []byte("b"))
	require.NoError(t, target.SetEarliestVersion(versions+1, true))
	require.NoError(t, target.Close())

	report, err := Merge(targetDir, sourceDir, MergeOptions{VerifyEvery: 1})
	require.NoError(t, err)
	require.Equal(t, int64(versions), report.Written)
	require.Equal(t, int64(versions), report.Verified)
}

// A target holding no versions takes the source's version markers.
func TestMergeIntoTargetWithoutVersions(t *testing.T) {
	source, sourceDir := newMergeTestDB(t, true)
	applyVersion(t, source, mergeTestStore, 10, []byte("a"), []byte("a@10"))
	require.NoError(t, source.SetEarliestVersion(8, true))
	require.NoError(t, source.Close())
	target, targetDir := newMergeTestDB(t, true)
	require.NoError(t, target.Close())

	_, err := Merge(targetDir, sourceDir, MergeOptions{})
	require.NoError(t, err)
	merged := openMergeTestDB(t, targetDir)
	require.Equal(t, int64(8), merged.GetEarliestVersion())
	require.Equal(t, int64(10), merged.GetLatestVersion())
	requireGet(t, merged, 10, "a", []byte("a@10"))
}

// Metadata other than the version markers, such as the range hashes and
// migration cursors older releases wrote, stays as each DB has it.
func TestMergeLeavesOtherMetadata(t *testing.T) {
	source, sourceDir := newMergeTestDB(t, false)
	applyVersion(t, source, mergeTestStore, 10, []byte("a"), []byte("a@10"))
	require.NoError(t, source.storage.Set([]byte("s/_hash:receipt:1-10"), []byte("source"), pebble.Sync))
	require.NoError(t, source.Close())
	target, targetDir := newMergeTestDB(t, false)
	applyVersion(t, target, mergeTestStore, 100, []byte("c"), []byte("c@100"))
	require.NoError(t, target.SetEarliestVersion(11, true))
	require.NoError(t, target.storage.Set([]byte("s/_latestMigratedKey"), []byte("target"), pebble.Sync))
	require.NoError(t, target.Close())

	report, err := Merge(targetDir, sourceDir, MergeOptions{VerifyEvery: 1})
	require.NoError(t, err)
	require.Equal(t, int64(1), report.Entries)

	raw, err := pebble.Open(targetDir, &pebble.Options{Comparer: MVCCComparer})
	require.NoError(t, err)
	defer func() { _ = raw.Close() }()
	_, _, err = raw.Get([]byte("s/_hash:receipt:1-10"))
	require.ErrorIs(t, err, pebble.ErrNotFound)
	got, closer, err := raw.Get([]byte("s/_latestMigratedKey"))
	require.NoError(t, err)
	require.Equal(t, []byte("target"), got)
	require.NoError(t, closer.Close())
}

// Entries above the source's latest version marker, which no read of the
// source sees, are left out and counted, in either encoding.
func TestMergeLeavesOutEntriesAboveTheSourceLatest(t *testing.T) {
	for _, descending := range []bool{false, true} {
		t.Run(fmt.Sprintf("descending %v", descending), func(t *testing.T) {
			source, sourceDir := newMergeTestDB(t, descending)
			applyVersion(t, source, mergeTestStore, 10, []byte("a"), []byte("a@10"))
			applyVersion(t, source, mergeTestStore, 20, []byte("a"), []byte("a@20"))
			applyVersion(t, source, mergeTestStore, 20, []byte("b"), []byte("b@20"))
			require.NoError(t, source.SetLatestVersion(15))
			require.NoError(t, source.Close())
			target, targetDir := newMergeTestDB(t, true)
			applyVersion(t, target, mergeTestStore, 100, []byte("c"), []byte("c@100"))
			require.NoError(t, target.SetEarliestVersion(16, true))
			require.NoError(t, target.Close())

			report, err := Merge(targetDir, sourceDir, MergeOptions{VerifyEvery: 1})
			require.NoError(t, err)
			require.Equal(t, int64(3), report.Entries)
			require.Equal(t, int64(2), report.AboveLatest)
			require.Equal(t, int64(1), report.Written)
			require.Equal(t, int64(1), report.Verified)

			merged := openMergeTestDB(t, targetDir)
			requireGet(t, merged, 100, "a", []byte("a@10"))
			requireGet(t, merged, 100, "b", nil)
		})
	}
}

// Overlapping version ranges are refused unless allowed, and an allowed merge
// leaves every entry the target already holds as the target holds it.
func TestMergeOverlap(t *testing.T) {
	newPair := func(t *testing.T) (targetDir, sourceDir string) {
		source, sourceDir := newMergeTestDB(t, true)
		applyVersion(t, source, mergeTestStore, 10, []byte("a"), []byte("source a@10"))
		applyVersion(t, source, mergeTestStore, 20, []byte("b"), []byte("b@20"))
		require.NoError(t, source.Close())
		target, targetDir := newMergeTestDB(t, true)
		applyVersion(t, target, mergeTestStore, 10, []byte("a"), []byte("target a@10"))
		applyVersion(t, target, mergeTestStore, 30, []byte("c"), []byte("c@30"))
		require.NoError(t, target.Close())
		return targetDir, sourceDir
	}

	t.Run("refused by default", func(t *testing.T) {
		targetDir, sourceDir := newPair(t)
		_, err := Merge(targetDir, sourceDir, MergeOptions{})
		require.ErrorContains(t, err, "source versions 0-20 overlap target versions 0-30")
		requireGet(t, openMergeTestDB(t, targetDir), 20, "b", nil)
	})

	t.Run("allowed", func(t *testing.T) {
		targetDir, sourceDir := newPair(t)
		report, err := Merge(targetDir, sourceDir, MergeOptions{AllowOverlap: true, VerifyEvery: 1})
		require.NoError(t, err)
		require.Equal(t, int64(2), report.Entries)
		require.Equal(t, int64(1), report.Written)
		require.Equal(t, int64(1), report.Held)
		require.Equal(t, int64(1), report.Conflicting)
		require.Equal(t, int64(1), report.Verified)

		merged := openMergeTestDB(t, targetDir)
		require.Equal(t, int64(30), merged.GetLatestVersion())
		requireGet(t, merged, 10, "a", []byte("target a@10"))
		requireGet(t, merged, 20, "b", []byte("b@20"))
	})
}

// newRangeDB returns the directory of a closed DB holding one entry at latest,
// with its earliest version marker set to earliest unless that is 0.
func newRangeDB(t *testing.T, earliest, latest int64) string {
	t.Helper()
	db, dir := newMergeTestDB(t, true)
	applyVersion(t, db, mergeTestStore, latest, []byte(fmt.Sprintf("key@%d", latest)), []byte("value"))
	if earliest > 0 {
		require.NoError(t, db.SetEarliestVersion(earliest, true))
	}
	require.NoError(t, db.Close())
	return dir
}

// Version ranges that leave versions between them which neither DB covers are
// refused unless allowed, whichever DB is the lower one, and an allowed merge
// spans those versions with the target's markers.
func TestMergeGap(t *testing.T) {
	t.Run("source below target", func(t *testing.T) {
		targetDir, sourceDir := newRangeDB(t, 20, 30), newRangeDB(t, 0, 10)
		report, err := Merge(targetDir, sourceDir, MergeOptions{})
		require.ErrorContains(t, err, "neither DB covers versions 11-19, between source versions 0-10 and target versions 20-30")
		require.Equal(t, VersionRange{From: 11, To: 19}, report.Gap)
		unchanged := openMergeTestDB(t, targetDir)
		require.Equal(t, int64(20), unchanged.GetEarliestVersion())
		requireGet(t, unchanged, 30, "key@10", nil)
	})

	t.Run("source above target", func(t *testing.T) {
		targetDir, sourceDir := newRangeDB(t, 0, 10), newRangeDB(t, 20, 30)
		_, err := Merge(targetDir, sourceDir, MergeOptions{})
		require.ErrorContains(t, err, "neither DB covers versions 11-19, between source versions 20-30 and target versions 0-10")
		require.Equal(t, int64(10), openMergeTestDB(t, targetDir).GetLatestVersion())
	})

	t.Run("refused by a dry run", func(t *testing.T) {
		targetDir, sourceDir := newRangeDB(t, 20, 30), newRangeDB(t, 0, 10)
		_, err := Merge(targetDir, sourceDir, MergeOptions{DryRun: true})
		require.ErrorContains(t, err, "neither DB covers versions 11-19")
	})

	t.Run("adjacent ranges", func(t *testing.T) {
		targetDir, sourceDir := newRangeDB(t, 11, 30), newRangeDB(t, 0, 10)
		report, err := Merge(targetDir, sourceDir, MergeOptions{})
		require.NoError(t, err)
		require.Zero(t, report.Gap)
		merged := openMergeTestDB(t, targetDir)
		require.Equal(t, int64(0), merged.GetEarliestVersion())
		require.Equal(t, int64(30), merged.GetLatestVersion())
	})

	t.Run("allowed", func(t *testing.T) {
		targetDir, sourceDir := newRangeDB(t, 20, 30), newRangeDB(t, 5, 10)
		report, err := Merge(targetDir, sourceDir, MergeOptions{AllowGap: true, VerifyEvery: 1})
		require.NoError(t, err)
		require.Equal(t, VersionRange{From: 11, To: 19}, report.Gap)
		require.Equal(t, int64(1), report.Verified)
		merged := openMergeTestDB(t, targetDir)
		require.Equal(t, int64(5), merged.GetEarliestVersion())
		require.Equal(t, int64(30), merged.GetLatestVersion())
		requireGet(t, merged, 15, "key@10", []byte("value"))
		requireGet(t, merged, 30, "key@30", []byte("value"))
	})
}

// A dry run makes every check and counts what it would write, writing nothing.
func TestMergeDryRunWritesNothing(t *testing.T) {
	source, sourceDir := newMergeTestDB(t, false)
	applyVersion(t, source, mergeTestStore, 10, []byte("a"), []byte("a@10"))
	require.NoError(t, source.Close())
	target, targetDir := newMergeTestDB(t, true)
	applyVersion(t, target, mergeTestStore, 100, []byte("c"), []byte("c@100"))
	require.NoError(t, target.SetEarliestVersion(11, true))
	require.NoError(t, target.Close())

	report, err := Merge(targetDir, sourceDir, MergeOptions{DryRun: true, VerifyEvery: 1})
	require.NoError(t, err)
	require.Equal(t, int64(1), report.Written)
	require.Equal(t, int64(0), report.Verified)
	require.Equal(t, int64(0), report.Earliest)
	require.Equal(t, int64(100), report.Latest)

	merged := openMergeTestDB(t, targetDir)
	require.Equal(t, int64(11), merged.GetEarliestVersion())
	require.Equal(t, int64(100), merged.GetLatestVersion())
	requireGet(t, merged, 100, "a", nil)
}

// A changelog the DB has applied is accepted, and one holding a version above
// the DB's latest is refused, since only opening the DB would apply it.
func TestMergeChecksTheChangelog(t *testing.T) {
	for _, tc := range []struct {
		name             string
		changelogVersion int64
		wantErr          string
	}{
		{name: "applied", changelogVersion: 10},
		{name: "ahead", changelogVersion: 11, wantErr: "changelog holds version 11 above latest version 10"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, sourceDir := newMergeTestDB(t, true)
			applyVersion(t, source, mergeTestStore, 10, []byte("a"), []byte("a@10"))
			require.NoError(t, source.Close())
			writeChangelogEntry(t, sourceDir, tc.changelogVersion)
			target, targetDir := newMergeTestDB(t, true)
			applyVersion(t, target, mergeTestStore, 100, []byte("c"), []byte("c@100"))
			require.NoError(t, target.SetEarliestVersion(11, true))
			require.NoError(t, target.Close())

			_, err := Merge(targetDir, sourceDir, MergeOptions{})
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
			requireGet(t, openMergeTestDB(t, targetDir), 100, "a", nil)
		})
	}
}

// A store outside StoreKeys, in either DB, refuses the merge.
func TestMergeRefusesUnacceptedStore(t *testing.T) {
	source, sourceDir := newMergeTestDB(t, true)
	applyVersion(t, source, mergeTestStore, 10, []byte("a"), []byte("a@10"))
	applyVersion(t, source, "bank", 11, []byte("k"), []byte("v"))
	require.NoError(t, source.Close())
	target, targetDir := newMergeTestDB(t, true)
	applyVersion(t, target, mergeTestStore, 100, []byte("c"), []byte("c@100"))
	require.NoError(t, target.Close())

	report, err := Merge(targetDir, sourceDir, MergeOptions{StoreKeys: []string{mergeTestStore}})
	require.ErrorContains(t, err, `holds store "bank"`)
	require.Equal(t, []string{"bank", mergeTestStore}, report.Source.StoreKeys)
	unchanged := openMergeTestDB(t, targetDir)
	requireGet(t, unchanged, 100, "a", nil)
	require.NoError(t, unchanged.Close())

	_, err = Merge(sourceDir, targetDir, MergeOptions{StoreKeys: []string{mergeTestStore}})
	require.ErrorContains(t, err, `holds store "bank"`)
}

func TestMergeRefusesOneDirectoryAsBoth(t *testing.T) {
	db, dir := newMergeTestDB(t, true)
	applyVersion(t, db, mergeTestStore, 10, []byte("a"), []byte("a@10"))
	require.NoError(t, db.Close())

	_, err := Merge(dir, dir, MergeOptions{})
	require.ErrorContains(t, err, "same directory")
}

func TestMergeRefusesSourceWithoutVersions(t *testing.T) {
	source, sourceDir := newMergeTestDB(t, true)
	require.NoError(t, source.Close())
	target, targetDir := newMergeTestDB(t, true)
	applyVersion(t, target, mergeTestStore, 100, []byte("c"), []byte("c@100"))
	require.NoError(t, target.Close())

	_, err := Merge(targetDir, sourceDir, MergeOptions{})
	require.ErrorContains(t, err, "source holds no versions")
}

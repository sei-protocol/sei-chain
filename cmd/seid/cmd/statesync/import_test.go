package statesync

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	ssarchive "github.com/sei-protocol/sei-chain/sei-db/statesync"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

func TestImportRoundTrip(t *testing.T) {
	p := newProducer(t)
	p.commit(t, 1)
	height := p.commit(t, 2)
	staged := p.stage(t, height)
	archive := archiveOf(t, staged, testChainID, height)
	tg := freshHome(t)

	require.NoError(t, runImport(t.Context(), tg, bytesSource(archive), io.Discard))

	requireRestored(t, tg, p, height)
	want, err := flatkv.SnapshotDirStats(staged, height)
	require.NoError(t, err)
	got, err := flatkv.SnapshotDirStats(filepath.Join(tg.layout.FlatKVDir, flatkv.SnapshotDirName(height)), height)
	require.NoError(t, err)
	require.Equal(t, want, got)
	requireRewritten(t, staged, filepath.Join(tg.layout.FlatKVDir, flatkv.SnapshotDirName(height)))
}

// requireRewritten asserts that no table file of the installed checkpoint is a copy of a table
// file of the archived checkpoint.
func requireRewritten(t *testing.T, archived, installed string) {
	t.Helper()
	archivedTables := map[string]bool{}
	for _, database := range flatkv.DataDBDirs() {
		for _, path := range tableFiles(t, filepath.Join(archived, database)) {
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			archivedTables[string(data)] = true
		}
	}
	for _, database := range flatkv.DataDBDirs() {
		for _, path := range tableFiles(t, filepath.Join(installed, database)) {
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			require.False(t, archivedTables[string(data)], "%s is a copy of an archived table", path)
		}
	}
}

func tableFiles(t *testing.T, dir string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.sst"))
	require.NoError(t, err)
	return paths
}

func TestImportFromABucket(t *testing.T) {
	p := newProducer(t)
	first := p.commit(t, 1)
	second := p.commit(t, 2)
	store := newMemStore()
	for _, height := range []int64{first, second} {
		archive := p.archive(t, testChainID, height)
		require.NoError(t, store.Put(t.Context(), ssarchive.ArchiveKey(testChainID, height), bytes.NewReader(archive), int64(len(archive)), ssarchive.Precondition{}))
	}
	_, err := ssarchive.AdvanceLatest(t.Context(), store, testChainID, second)
	require.NoError(t, err)

	t.Run("latest", func(t *testing.T) {
		tg := freshHome(t)
		src, err := bucketSource(t.Context(), store, testChainID, 0)
		require.NoError(t, err)
		require.NoError(t, runImport(t.Context(), tg, src, io.Discard))
		requireRestored(t, tg, p, second)
	})
	t.Run("height", func(t *testing.T) {
		tg := freshHome(t)
		src, err := bucketSource(t.Context(), store, testChainID, first)
		require.NoError(t, err)
		require.NoError(t, runImport(t.Context(), tg, src, io.Discard))
		requireRestored(t, tg, p, first)
	})
	t.Run("archive of another height", func(t *testing.T) {
		tg := freshHome(t)
		src, err := bucketSource(t.Context(), store, testChainID, first)
		require.NoError(t, err)
		src.height = second
		require.ErrorContains(t, runImport(t.Context(), tg, src, io.Discard), "want")
		requireUntouched(t, tg)
	})
	t.Run("no latest", func(t *testing.T) {
		_, err := bucketSource(t.Context(), newMemStore(), testChainID, 0)
		require.ErrorIs(t, err, ssarchive.ErrObjectNotFound)
	})
}

func TestImportRejectsAnotherChain(t *testing.T) {
	p := newProducer(t)
	height := p.commit(t, 1)
	tg := freshHome(t)

	err := runImport(t.Context(), tg, bytesSource(p.archive(t, "chain-b", height)), io.Discard)

	require.ErrorContains(t, err, "chain id")
	requireUntouched(t, tg)
}

func TestImportRejectsAChangedRow(t *testing.T) {
	p := newProducer(t)
	height := p.commit(t, 1)
	staged := p.stage(t, height)
	changeOneRow(t, staged)
	tg := freshHome(t)

	err := runImport(t.Context(), tg, bytesSource(archiveOf(t, staged, testChainID, height)), io.Discard)

	require.ErrorContains(t, err, "verify the rewritten checkpoint")
	requireUntouched(t, tg)
}

// changeOneRow changes the value of one data row of the checkpoint in dir, and leaves its
// metadata as it is.
func changeOneRow(t *testing.T, dir string) {
	t.Helper()
	for _, database := range flatkv.DataDBDirs() {
		db, err := pebble.Open(filepath.Join(dir, database), &pebble.Options{FormatMajorVersion: pebbledb.FormatMajorVersion})
		require.NoError(t, err)
		iter, err := db.NewIter(nil)
		require.NoError(t, err)
		var key, value []byte
		for iter.First(); iter.Valid(); iter.Next() {
			if !ktype.IsMetaKey(iter.Key()) {
				key = bytes.Clone(iter.Key())
				value = bytes.Clone(iter.Value())
				break
			}
		}
		require.NoError(t, iter.Close())
		if key != nil {
			value[len(value)-1] ^= 0xff
			require.NoError(t, db.Set(key, value, pebble.Sync))
			require.NoError(t, db.Close())
			return
		}
		require.NoError(t, db.Close())
	}
	t.Fatal("the checkpoint has no data row")
}

func TestImportRejectsAValidatorHome(t *testing.T) {
	tg := freshHome(t)
	tg.mode = tmcfg.ModeValidator

	require.ErrorContains(t, runImport(t.Context(), tg, bytesSource(nil), io.Discard), "validator")
}

func TestImportRejectsAHomeWithOtherData(t *testing.T) {
	p := newProducer(t)
	height := p.commit(t, 1)
	archive := p.archive(t, testChainID, height)

	t.Run("other file", func(t *testing.T) {
		tg := freshHome(t)
		other := filepath.Join(tg.layout.PersistentStateDir, "blockdb", "0", "data")
		require.NoError(t, os.MkdirAll(filepath.Dir(other), 0o750))
		require.NoError(t, os.WriteFile(other, []byte("x"), 0o600))

		require.ErrorContains(t, runImport(t.Context(), tg, bytesSource(archive), io.Discard), other)
	})
	t.Run("FlatKV data without the marker", func(t *testing.T) {
		tg := freshHome(t)
		s := openStore(t, tg.layout.FlatKVDir)
		require.NoError(t, s.Close())

		require.ErrorContains(t, runImport(t.Context(), tg, bytesSource(archive), io.Discard), tg.layout.FlatKVDir)
		require.NoFileExists(t, tg.layout.RestoreMarkerFile)
	})
	t.Run("an app-hash vault without the marker", func(t *testing.T) {
		tg := freshHome(t)
		require.NoError(t, os.MkdirAll(tg.layout.AppHashVaultDir, 0o750))

		require.ErrorContains(t, runImport(t.Context(), tg, bytesSource(archive), io.Discard), tg.layout.AppHashVaultDir)
		require.NoFileExists(t, tg.layout.RestoreMarkerFile)
	})
}

func TestImportRecoversFromAStopAtEachStep(t *testing.T) {
	p := newProducer(t)
	height := p.commit(t, 1)
	archive := p.archive(t, testChainID, height)
	for _, step := range []string{
		importStepChecked,
		importStepRewritten,
		importStepMarked,
		importStepProofInstalled,
		importStepAnchorStored,
		importStepInstalled,
	} {
		t.Run(step, func(t *testing.T) {
			tg := freshHome(t)
			stopAt := func(s string) error {
				if s == step {
					return errStopped
				}
				return nil
			}

			require.ErrorIs(t, importWith(t.Context(), tg, bytesSource(archive), io.Discard, stopAt), errStopped)
			require.NoError(t, runImport(t.Context(), tg, bytesSource(archive), io.Discard))

			requireRestored(t, tg, p, height)
		})
	}
}

func TestImportReplacesAnEarlierImport(t *testing.T) {
	p := newProducer(t)
	first := p.commit(t, 1)
	firstArchive := p.archive(t, testChainID, first)
	second := p.commit(t, 2)
	secondArchive := p.archive(t, testChainID, second)
	tg := freshHome(t)
	require.NoError(t, runImport(t.Context(), tg, bytesSource(firstArchive), io.Discard))
	stopAt := func(s string) error {
		if s == importStepFlatKVCleared {
			return errStopped
		}
		return nil
	}

	require.ErrorIs(t, importWith(t.Context(), tg, bytesSource(secondArchive), io.Discard, stopAt), errStopped)
	require.NoError(t, runImport(t.Context(), tg, bytesSource(secondArchive), io.Discard))

	requireRestored(t, tg, p, second)
}

func TestImportRejectsALockedHome(t *testing.T) {
	tg := freshHome(t)
	lock, err := flatkv.LockRoot(tg.layout.FlatKVDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, lock.Unlock()) }()

	require.ErrorContains(t, runImport(t.Context(), tg, bytesSource(nil), io.Discard), "is a node running")
}

func TestRequireSameFilesystem(t *testing.T) {
	psd := t.TempDir()
	flatKVDir := filepath.Join(psd, "data", "flatkv")
	require.NoError(t, os.MkdirAll(flatKVDir, 0o750))

	require.NoError(t, requireSameFilesystem(psd, flatKVDir))
	// /dev is a devfs or devtmpfs mount on macOS and Linux.
	require.ErrorContains(t, requireSameFilesystem(psd, "/dev"), "not on the filesystem of the FlatKV root")
}

func TestResolveTargetRefuses(t *testing.T) {
	_, err := resolveTargetFromConfig(&tmcfg.Config{}, configtest.AppOpts{"giga.state_sync.enabled": true})
	require.ErrorContains(t, err, "autobahn-config-file")

	_, err = resolveTargetFromConfig(&tmcfg.Config{AutobahnConfigFile: "autobahn.json"}, configtest.AppOpts{})
	require.ErrorIs(t, err, errDisabled)
}

func TestResolveSourceRejects(t *testing.T) {
	tg := freshHome(t)
	for name, tc := range map[string]struct {
		from   string
		height int64
	}{
		"height with from":  {from: "archive.tar", height: 3},
		"no bucket":         {},
		"s3 without key":    {from: "s3://bucket"},
		"s3 without bucket": {from: "s3:///key"},
		"s3 with empty key": {from: "s3://bucket/"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := resolveSource(t.Context(), tg, tc.from, tc.height, "")
			require.Error(t, err)
		})
	}
}

func TestResolveSourceReadsALocalFile(t *testing.T) {
	p := newProducer(t)
	height := p.commit(t, 1)
	path := filepath.Join(t.TempDir(), "archive.tar")
	require.NoError(t, os.WriteFile(path, p.archive(t, testChainID, height), 0o600))
	tg := freshHome(t)

	src, err := resolveSource(t.Context(), tg, path, 0, "")
	require.NoError(t, err)
	require.NoError(t, runImport(t.Context(), tg, src, io.Discard))

	requireRestored(t, tg, p, height)
}

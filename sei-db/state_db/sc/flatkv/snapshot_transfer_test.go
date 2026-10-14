package flatkv

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	commonerrors "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
)

// setupSnapshottingStore opens a store that snapshots every block and keeps keepRecent older snapshots.
func setupSnapshottingStore(t *testing.T, keepRecent uint32) *CommitStore {
	t.Helper()
	cfg := config.DefaultTestConfig(t)
	cfg.SnapshotInterval = 1
	cfg.SnapshotKeepRecent = keepRecent
	return setupTestStoreWithConfig(t, cfg)
}

// commitToEveryDB commits one block that writes a row to each of the four data databases.
func commitToEveryDB(t *testing.T, s *CommitStore, n byte) int64 {
	t.Helper()
	addr := addrN(n)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{
		namedCS(
			noncePair(addr, uint64(n)),
			codeHashPair(addr, codeHashN(n)),
			codePair(addr, []byte{0x60, n}),
			storagePair(addr, slotN(n), []byte{n}),
		),
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte{'b', n}, Value: []byte{n}},
		}}},
	}))
	return commitAndCheck(t, s)
}

// requireRowsFrom asserts that s holds the rows commitToEveryDB(n) wrote.
func requireRowsFrom(t *testing.T, s *CommitStore, n byte) {
	t.Helper()
	addr := addrN(n)
	got, ok := s.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]))
	require.True(t, ok, "nonce %d", n)
	require.Equal(t, nonceBytes(uint64(n)), got)
	got, ok = s.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyCode, addr[:]))
	require.True(t, ok, "code %d", n)
	require.Equal(t, []byte{0x60, n}, got)
	got, ok = s.Get(keys.EVMStoreKey, storagePair(addr, slotN(n), []byte{n}).Key)
	require.True(t, ok, "storage %d", n)
	require.Equal(t, padLeft32(n), got)
	got, ok = s.Get("bank", []byte{'b', n})
	require.True(t, ok, "misc %d", n)
	require.Equal(t, []byte{n}, got)
}

// regularFiles returns the paths of the regular files under dir, relative to it.
func regularFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	require.NoError(t, filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			files = append(files, rel)
		}
		return nil
	}))
	return files
}

func TestSnapshotDirNameRoundTrip(t *testing.T) {
	for _, v := range []int64{0, 1, 123456, 1<<62 + 7} {
		name := SnapshotDirName(v)
		require.Len(t, name, 29)
		got, err := ParseSnapshotDirName(name)
		require.NoError(t, err)
		require.Equal(t, v, got)
	}

	for _, name := range []string{
		"",
		"snapshot-",
		"snapshot-123",
		"snapshot-00000000000000000001-tmp",
		"snapshot-+0000000000000000001",
		"snapshot--0000000000000000001",
		"snapshot-0000000000000000000a",
		"snapshot-99999999999999999999",
		"snapshoT-00000000000000000001",
		"working",
	} {
		_, err := ParseSnapshotDirName(name)
		require.Error(t, err, "name %q", name)
	}
}

func TestDataDBDirsIsACopy(t *testing.T) {
	dirs := DataDBDirs()
	require.Equal(t, []string{"account", "code", "storage", "misc"}, dirs)
	dirs[0] = "changed"
	require.Equal(t, accountDBDir, DataDBDirs()[0])
}

func TestActiveSnapshotVersion(t *testing.T) {
	root := t.TempDir()
	_, err := ActiveSnapshotVersion(root)
	require.ErrorIs(t, err, os.ErrNotExist)

	require.NoError(t, os.Mkdir(filepath.Join(root, SnapshotDirName(7)), 0o750))
	require.NoError(t, updateCurrentSymlink(root, SnapshotDirName(7)))
	v, err := ActiveSnapshotVersion(root)
	require.NoError(t, err)
	require.Equal(t, int64(7), v)

	require.NoError(t, updateCurrentSymlink(root, "snapshot-+0000000000000000007"))
	_, err = ActiveSnapshotVersion(root)
	require.ErrorContains(t, err, "invalid snapshot directory name")
}

func TestSnapshotVersionsListsOnlyCompleteSnapshots(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{
		SnapshotDirName(3),
		SnapshotDirName(1),
		SnapshotDirName(2) + tmpSuffix,
		SnapshotDirName(4) + removingSuffix,
		"snapshot-+0000000000000000006",
		workingDirName,
		readOnlyDirPrefix + "123",
		changelogDir,
	} {
		require.NoError(t, os.Mkdir(filepath.Join(root, name), 0o750))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, SnapshotDirName(5)), nil, 0o600))

	versions, err := SnapshotVersions(root)
	require.NoError(t, err)
	require.Equal(t, []int64{1, 3}, versions)

	versions, err = SnapshotVersions(filepath.Join(root, "absent"))
	require.NoError(t, err)
	require.Empty(t, versions)
}

func TestPinSnapshotHardLinksEveryFileAndSkipsLock(t *testing.T) {
	s := setupSnapshottingStore(t, 10)
	defer func() { require.NoError(t, s.Close()) }()
	commitToEveryDB(t, s, 1)
	h := commitToEveryDB(t, s, 2)

	root := s.flatkvDir()
	src := filepath.Join(root, SnapshotDirName(h))
	require.NoError(t, os.WriteFile(filepath.Join(src, lockFileName), nil, 0o600))
	dst := filepath.Join(t.TempDir(), "pin")
	require.NoError(t, PinSnapshot(root, h, dst))

	srcFiles := regularFiles(t, src)
	var want []string
	for _, rel := range srcFiles {
		if filepath.Base(rel) != lockFileName {
			want = append(want, rel)
		}
	}
	require.NotEmpty(t, want)
	require.ElementsMatch(t, want, regularFiles(t, dst))
	for _, rel := range want {
		srcInfo, err := os.Stat(filepath.Join(src, rel))
		require.NoError(t, err)
		dstInfo, err := os.Stat(filepath.Join(dst, rel))
		require.NoError(t, err)
		require.True(t, os.SameFile(srcInfo, dstInfo), "%s is not a hard link", rel)
	}
	require.NoError(t, VerifySnapshotDir(dst, h))
}

func TestPinSnapshotSurvivesPruning(t *testing.T) {
	s := setupSnapshottingStore(t, 0)
	defer func() { require.NoError(t, s.Close()) }()
	h := commitToEveryDB(t, s, 1)

	root := s.flatkvDir()
	dst := filepath.Join(t.TempDir(), "pin")
	require.NoError(t, PinSnapshot(root, h, dst))

	commitToEveryDB(t, s, 2)
	commitToEveryDB(t, s, 3)
	require.NoDirExists(t, filepath.Join(root, SnapshotDirName(h)))
	require.NoError(t, VerifySnapshotDir(dst, h))
}

func TestPinSnapshotRefusals(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, SnapshotDirName(4))
	require.NoError(t, os.MkdirAll(filepath.Join(src, accountDBDir), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(src, accountDBDir, "000001.sst"), []byte("x"), 0o600))

	t.Run("absent snapshot", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "pin")
		err := PinSnapshot(root, 5, dst)
		require.ErrorIs(t, err, os.ErrNotExist)
		require.NoDirExists(t, dst)
	})

	t.Run("existing destination is kept", func(t *testing.T) {
		dst := t.TempDir()
		marker := filepath.Join(dst, "keep")
		require.NoError(t, os.WriteFile(marker, nil, 0o600))
		require.Error(t, PinSnapshot(root, 4, dst))
		require.FileExists(t, marker)
	})

	t.Run("destination inside the snapshot", func(t *testing.T) {
		into := filepath.Join(t.TempDir(), "into")
		require.NoError(t, os.Symlink(src, into))
		for _, dst := range []string{
			src,
			filepath.Join(src, "pin"),
			filepath.Join(src, accountDBDir, "pin"),
			filepath.Join(into, "pin"),
		} {
			require.ErrorContains(t, PinSnapshot(root, 4, dst), "is inside the snapshot", dst)
		}
		require.Equal(t, []string{filepath.Join(accountDBDir, "000001.sst")}, regularFiles(t, src))
		entries, err := os.ReadDir(src)
		require.NoError(t, err)
		require.Len(t, entries, 1, "the snapshot must not gain entries")
	})

	t.Run("symlink entry", func(t *testing.T) {
		link := filepath.Join(src, "link")
		require.NoError(t, os.Symlink("000001.sst", link))
		defer func() { require.NoError(t, os.Remove(link)) }()
		dst := filepath.Join(t.TempDir(), "pin")
		err := PinSnapshot(root, 4, dst)
		require.ErrorContains(t, err, "not a regular file or a directory")
		require.NoDirExists(t, dst)
	})
}

func TestPinSnapshotPrunedMidPin(t *testing.T) {
	s := setupSnapshottingStore(t, 10)
	defer func() { require.NoError(t, s.Close()) }()
	h := commitToEveryDB(t, s, 1)
	root := s.flatkvDir()
	src := filepath.Join(root, SnapshotDirName(h))

	dst := filepath.Join(t.TempDir(), "pin")
	pruned := false
	err := pinSnapshotWith(root, h, dst, func(oldname, newname string) error {
		if !pruned {
			pruned = true
			require.NoError(t, atomicRemoveDir(src))
		}
		return os.Link(oldname, newname)
	})
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NoDirExists(t, dst)
}

func TestPinSnapshotReplacedMidPin(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, SnapshotDirName(4))
	for _, db := range dataDBDirs {
		require.NoError(t, os.MkdirAll(filepath.Join(src, db), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(src, db, "000001.sst"), []byte(db), 0o600))
	}

	dst := filepath.Join(t.TempDir(), "pin")
	replaced := false
	err := pinSnapshotWith(root, 4, dst, func(oldname, newname string) error {
		if !replaced {
			replaced = true
			// What publishSnapshot does when it rewrites a version: a new directory takes the name.
			fresh := src + tmpSuffix
			for _, db := range dataDBDirs {
				require.NoError(t, os.MkdirAll(filepath.Join(fresh, db), 0o750))
				require.NoError(t, os.WriteFile(filepath.Join(fresh, db, "000002.sst"), []byte(db), 0o600))
				require.NoError(t, os.WriteFile(filepath.Join(fresh, db, "000001.sst"), []byte(db), 0o600))
			}
			require.NoError(t, atomicRemoveDir(src))
			require.NoError(t, os.Rename(fresh, src))
		}
		return os.Link(oldname, newname)
	})
	require.ErrorIs(t, err, os.ErrNotExist)
	require.ErrorContains(t, err, "replaced during the pin")
	require.NoDirExists(t, dst)
}

func TestVerifySnapshotDir(t *testing.T) {
	s := setupSnapshottingStore(t, 10)
	defer func() { require.NoError(t, s.Close()) }()
	commitToEveryDB(t, s, 1)
	h := commitToEveryDB(t, s, 2)
	dir := filepath.Join(t.TempDir(), "pin")
	require.NoError(t, PinSnapshot(s.flatkvDir(), h, dir))

	require.NoError(t, VerifySnapshotDir(dir, h))
	require.ErrorContains(t, VerifySnapshotDir(dir, h+1), "want")
	require.ErrorContains(t, VerifySnapshotDir(dir, h-1), "want")
	for _, name := range dataDBDirs {
		require.FileExists(t, filepath.Join(dir, name, lockFileName), "a read-only open leaves a LOCK file")
	}

	require.NoError(t, os.RemoveAll(filepath.Join(dir, codeDBDir)))
	err := VerifySnapshotDir(dir, h)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.ErrorContains(t, err, codeDBDir)
}

func TestVerifySnapshotDirRefusesSymlinks(t *testing.T) {
	s := setupSnapshottingStore(t, 10)
	h := commitToEveryDB(t, s, 1)
	parent := t.TempDir()
	pin := filepath.Join(parent, "pin")
	require.NoError(t, PinSnapshot(s.flatkvDir(), h, pin))
	require.NoError(t, s.Close())
	require.NoError(t, VerifySnapshotDir(pin, h))

	link := filepath.Join(parent, "link")
	require.NoError(t, os.Symlink("pin", link))
	require.ErrorContains(t, VerifySnapshotDir(link, h), "not a regular file or a directory")

	require.NoError(t, os.Rename(filepath.Join(pin, accountDBDir), filepath.Join(parent, accountDBDir)))
	require.NoError(t, os.Symlink(filepath.Join("..", accountDBDir), filepath.Join(pin, accountDBDir)))
	require.ErrorContains(t, VerifySnapshotDir(pin, h), "not a regular file or a directory")
}

func TestVerifySnapshotDirRefusesEmptyDatabases(t *testing.T) {
	dir := t.TempDir()
	for _, name := range dataDBDirs {
		require.NoError(t, os.Mkdir(filepath.Join(dir, name), 0o750))
	}
	require.ErrorContains(t, VerifySnapshotDir(dir, 0), "below 1")
	require.ErrorContains(t, VerifySnapshotDir(dir, 1), "want")
}

func TestLockRootExcludesAStore(t *testing.T) {
	s := setupSnapshottingStore(t, 10)
	root := s.flatkvDir()
	_, err := LockRoot(root)
	require.ErrorIs(t, err, commonerrors.ErrFileLockUnavailable)
	require.NoError(t, s.Close())

	lock, err := LockRoot(root)
	require.NoError(t, err)
	_, err = LockRoot(root)
	require.ErrorIs(t, err, commonerrors.ErrFileLockUnavailable)

	cfg := config.DefaultTestConfig(t)
	cfg.DataDir = root
	reopened, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.ErrorIs(t, reopened.LoadLatest(), commonerrors.ErrFileLockUnavailable)
	require.NoError(t, reopened.Close())

	require.NoError(t, lock.Unlock())
	lock, err = LockRoot(root)
	require.NoError(t, err)
	require.NoError(t, lock.Unlock())
}

func TestRootLockEmptyAndClear(t *testing.T) {
	root := filepath.Join(t.TempDir(), "flatkv")
	lock, err := LockRoot(root)
	require.NoError(t, err)
	defer func() { require.NoError(t, lock.Unlock()) }()

	empty, err := lock.Empty()
	require.NoError(t, err)
	require.True(t, empty, "a new root holds only its lock file")

	require.NoError(t, os.MkdirAll(filepath.Join(root, SnapshotDirName(3), accountDBDir), 0o750))
	require.NoError(t, os.Symlink(SnapshotDirName(3), filepath.Join(root, currentLink)))
	empty, err = lock.Empty()
	require.NoError(t, err)
	require.False(t, empty)

	require.NoError(t, lock.Clear())
	empty, err = lock.Empty()
	require.NoError(t, err)
	require.True(t, empty)
	require.FileExists(t, filepath.Join(root, lockFileName))
}

// TestRootLockClearStoppedLeavesALoadableRoot stops Clear at each removal, after it removes part of the
// path as a crash inside a recursive removal would. It checks that every directory left under its own
// name is whole, that a store opened on the root loads no version above the one it loaded before Clear
// and has that version's hash, and that a second Clear empties the root.
func TestRootLockClearStoppedLeavesALoadableRoot(t *testing.T) {
	errStop := errors.New("stopped")
	for _, tc := range []struct {
		name     string
		interval uint32
		// externalWAL opens the store with no WAL of its own, so it loads only what its files hold.
		externalWAL bool
		// rewindTo, when not 0, points current at that snapshot and keeps the snapshots above it, as a
		// rollback that stopped before it removed them leaves the root.
		rewindTo int64
	}{
		{name: "snapshot interval 1", interval: 1},
		// The only snapshot is version 0, so the working copy and the WAL hold every block.
		{name: "snapshot interval 10000", interval: 10000},
		{name: "current below a discarded snapshot", interval: 1, externalWAL: true, rewindTo: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			open := func(t *testing.T, cfg *config.Config) *CommitStore {
				t.Helper()
				var s *CommitStore
				var err error
				if tc.externalWAL {
					s, err = NewCommitStore(t.Context(), cfg, nil)
				} else {
					s, err = newCommitStoreWithWAL(t.Context(), cfg)
				}
				require.NoError(t, err)
				require.NoError(t, s.LoadLatest())
				return s
			}
			for stopAt := 0; ; stopAt++ {
				cfg := config.DefaultTestConfig(t)
				cfg.SnapshotInterval = tc.interval
				cfg.SnapshotKeepRecent = 10
				s := open(t, cfg)
				hashes := map[int64][]byte{0: rootHash(s)}
				for n := byte(1); n <= 3; n++ {
					v := commitToEveryDB(t, s, n)
					hashes[v] = rootHash(s)
				}
				root := s.flatkvDir()
				require.NoError(t, s.Close())
				if tc.rewindTo != 0 {
					require.NoError(t, repointAtSnapshot(root, tc.rewindTo))
				}
				before := open(t, cfg)
				loadedBefore := before.Version()
				require.NoError(t, before.Close())
				if tc.rewindTo != 0 {
					require.Equal(t, tc.rewindTo, loadedBefore)
					require.DirExists(t, filepath.Join(root, SnapshotDirName(tc.rewindTo+1)),
						"fixture precondition: a discarded snapshot sits above current")
				}

				whole := map[string][]string{}
				entries, err := os.ReadDir(root)
				require.NoError(t, err)
				for _, entry := range entries {
					if entry.IsDir() {
						whole[entry.Name()] = regularFiles(t, filepath.Join(root, entry.Name()))
					}
				}
				require.Contains(t, whole, workingDirName)
				if !tc.externalWAL {
					require.Contains(t, whole, changelogDir)
				}

				lock, err := LockRoot(root)
				require.NoError(t, err)
				calls := 0
				err = lock.clearWith(func(path string) error {
					defer func() { calls++ }()
					if calls != stopAt {
						return os.RemoveAll(path)
					}
					if info, err := os.Lstat(path); err == nil && info.IsDir() {
						if files := regularFiles(t, path); len(files) > 0 {
							require.NoError(t, os.Remove(filepath.Join(path, files[0])))
						}
					}
					return errStop
				})
				if err == nil {
					require.NoError(t, lock.Unlock())
					require.Greater(t, stopAt, 3, "every removal was stopped at least once")
					return
				}
				require.ErrorIs(t, err, errStop)

				entries, err = os.ReadDir(root)
				require.NoError(t, err)
				for _, entry := range entries {
					name := entry.Name()
					if entry.IsDir() && !strings.HasSuffix(name, removingSuffix) {
						require.Equal(t, whole[name], regularFiles(t, filepath.Join(root, name)),
							"stop %d: %s keeps its name and must be whole", stopAt, name)
					}
				}
				if target, err := os.Readlink(filepath.Join(root, currentLink)); err == nil {
					require.DirExists(t, filepath.Join(root, target), "stop %d: the current link has a target", stopAt)
				}
				require.NoError(t, lock.Unlock())

				reopened := open(t, cfg)
				v := reopened.Version()
				require.LessOrEqual(t, v, loadedBefore, "stop %d: no version above the one before Clear", stopAt)
				require.Equal(t, hashes[v], rootHash(reopened), "stop %d: version %d", stopAt, v)
				require.NoError(t, reopened.Close())

				lock, err = LockRoot(root)
				require.NoError(t, err)
				require.NoError(t, lock.Clear(), "stop %d: a second Clear completes", stopAt)
				empty, err := lock.Empty()
				require.NoError(t, err)
				require.True(t, empty, "stop %d", stopAt)
				require.NoError(t, lock.Unlock())
			}
		})
	}
}

func TestInstallSnapshotRoundTrip(t *testing.T) {
	src := setupSnapshottingStore(t, 10)
	commitToEveryDB(t, src, 1)
	h := commitToEveryDB(t, src, 2)
	wantHash := rootHash(src)
	pin := filepath.Join(t.TempDir(), "pin")
	require.NoError(t, PinSnapshot(src.flatkvDir(), h, pin))
	commitToEveryDB(t, src, 3)
	require.NoError(t, src.Close())

	cfg := config.DefaultTestConfig(t)
	lock, err := LockRoot(cfg.DataDir)
	require.NoError(t, err)
	require.NoError(t, lock.InstallSnapshot(pin, h))
	require.NoError(t, lock.Unlock())
	require.NoDirExists(t, pin)

	target, err := os.Readlink(filepath.Join(cfg.DataDir, currentLink))
	require.NoError(t, err)
	require.Equal(t, SnapshotDirName(h), target, "the current link is relative")

	dst, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, dst.LoadLatest())
	defer func() { require.NoError(t, dst.Close()) }()
	require.Equal(t, h, dst.Version())
	require.Equal(t, wantHash, rootHash(dst))
	requireRowsFrom(t, dst, 1)
	requireRowsFrom(t, dst, 2)
	_, ok := dst.Get("bank", []byte{'b', 3})
	require.False(t, ok, "a row written after the pin must not be installed")

	commitToEveryDB(t, dst, 4)
	require.Equal(t, h+1, dst.Version())
}

func TestInstallSnapshotRefusals(t *testing.T) {
	src := setupSnapshottingStore(t, 10)
	h := commitToEveryDB(t, src, 1)
	newPin := func(t *testing.T) string {
		t.Helper()
		pin := filepath.Join(t.TempDir(), "pin")
		require.NoError(t, PinSnapshot(src.flatkvDir(), h, pin))
		return pin
	}
	defer func() { require.NoError(t, src.Close()) }()

	for _, entry := range []string{workingDirName, changelogDir, SnapshotDirName(1)} {
		t.Run("root holds "+entry, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "flatkv")
			lock, err := LockRoot(root)
			require.NoError(t, err)
			defer func() { require.NoError(t, lock.Unlock()) }()
			require.NoError(t, os.Mkdir(filepath.Join(root, entry), 0o750))

			pin := newPin(t)
			require.ErrorContains(t, lock.InstallSnapshot(pin, h), "is not empty")
			require.DirExists(t, pin)
			_, err = os.Lstat(filepath.Join(root, currentLink))
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}

	t.Run("wrong version", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "flatkv")
		lock, err := LockRoot(root)
		require.NoError(t, err)
		defer func() { require.NoError(t, lock.Unlock()) }()

		pin := newPin(t)
		require.ErrorContains(t, lock.InstallSnapshot(pin, h+1), "want")
		empty, err := lock.Empty()
		require.NoError(t, err)
		require.True(t, empty)
	})

	t.Run("src is a relative symlink", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "flatkv")
		lock, err := LockRoot(root)
		require.NoError(t, err)
		defer func() { require.NoError(t, lock.Unlock()) }()

		pin := newPin(t)
		link := filepath.Join(filepath.Dir(pin), "link")
		require.NoError(t, os.Symlink(filepath.Base(pin), link))
		require.ErrorContains(t, lock.InstallSnapshot(link, h), "not a regular file or a directory")
		require.DirExists(t, pin)
		empty, err := lock.Empty()
		require.NoError(t, err)
		require.True(t, empty)
	})
}

func TestInstallSnapshotUndoesTheMoveOnFailure(t *testing.T) {
	src := setupSnapshottingStore(t, 10)
	h := commitToEveryDB(t, src, 1)
	defer func() { require.NoError(t, src.Close()) }()

	for _, tc := range []struct {
		name    string
		publish func(root, name string) error
	}{
		{"the link step fails", func(root, _ string) error {
			require.NoError(t, os.Symlink("x", filepath.Join(root, currentTmpLink)))
			return errors.New("link failed")
		}},
		{"the sync fails after the link", func(root, name string) error {
			require.NoError(t, updateCurrentSymlink(root, name))
			return errors.New("sync failed")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pin := filepath.Join(t.TempDir(), "pin")
			require.NoError(t, PinSnapshot(src.flatkvDir(), h, pin))
			root := filepath.Join(t.TempDir(), "flatkv")
			lock, err := LockRoot(root)
			require.NoError(t, err)
			defer func() { require.NoError(t, lock.Unlock()) }()

			err = lock.installSnapshotWith(pin, h, tc.publish)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "undo")
			require.NoError(t, VerifySnapshotDir(pin, h), "src is back in place")
			empty, err := lock.Empty()
			require.NoError(t, err)
			require.True(t, empty, "the root has no snapshot and no current link")

			require.NoError(t, lock.InstallSnapshot(pin, h), "a retry succeeds")
			v, err := ActiveSnapshotVersion(root)
			require.NoError(t, err)
			require.Equal(t, h, v)
		})
	}
}

// TestInstallSnapshotUndoStoppedAfterTheLinksLoadsTheSnapshot opens a store on the root an undo leaves
// when it stops after it removes the links and before it moves the snapshot back.
func TestInstallSnapshotUndoStoppedAfterTheLinksLoadsTheSnapshot(t *testing.T) {
	src := setupSnapshottingStore(t, 10)
	h := commitToEveryDB(t, src, 1)
	wantHash := rootHash(src)
	pin := filepath.Join(t.TempDir(), "pin")
	require.NoError(t, PinSnapshot(src.flatkvDir(), h, pin))
	require.NoError(t, src.Close())

	cfg := config.DefaultTestConfig(t)
	require.NoError(t, os.MkdirAll(cfg.DataDir, 0o750))
	require.NoError(t, os.Rename(pin, filepath.Join(cfg.DataDir, SnapshotDirName(h))))

	dst, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, dst.LoadLatest())
	defer func() { require.NoError(t, dst.Close()) }()
	require.Equal(t, h, dst.Version())
	require.Equal(t, wantHash, rootHash(dst))
}

func TestPinSnapshotSkipsOnlyLockFiles(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, SnapshotDirName(9))
	require.NoError(t, os.MkdirAll(filepath.Join(src, accountDBDir, "nested"), 0o750))
	for _, rel := range []string{
		filepath.Join(accountDBDir, "000001.sst"),
		filepath.Join(accountDBDir, "MANIFEST-000001"),
		filepath.Join(accountDBDir, lockFileName),
		filepath.Join(accountDBDir, "nested", "OPTIONS"),
		lockFileName,
	} {
		require.NoError(t, os.WriteFile(filepath.Join(src, rel), []byte(rel), 0o600))
	}

	dst := filepath.Join(t.TempDir(), "pin")
	require.NoError(t, PinSnapshot(root, 9, dst))
	got := regularFiles(t, dst)
	for _, rel := range got {
		require.False(t, strings.HasSuffix(rel, lockFileName), rel)
	}
	require.ElementsMatch(t, []string{
		filepath.Join(accountDBDir, "000001.sst"),
		filepath.Join(accountDBDir, "MANIFEST-000001"),
		filepath.Join(accountDBDir, "nested", "OPTIONS"),
	}, got)
}

package flatkv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/zbiljic/go-filelock"
)

// SnapshotDirName returns the directory name of the snapshot at version, for example
// "snapshot-00000000000000000100".
func SnapshotDirName(version int64) string {
	return snapshotName(version)
}

// ParseSnapshotDirName returns the version a snapshot directory name encodes. It accepts only
// "snapshot-" followed by exactly 20 decimal digits.
func ParseSnapshotDirName(name string) (int64, error) {
	if !isSnapshotName(name) || !isDecimal(name[len(snapshotPrefix):]) {
		return 0, fmt.Errorf("invalid snapshot directory name %q", name)
	}
	return parseSnapshotVersion(name)
}

func isDecimal(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

// DataDBDirs returns the names of the data databases a snapshot holds, one directory each.
func DataDBDirs() []string {
	return slices.Clone(dataDBDirs)
}

// ActiveSnapshotVersion returns the version of the snapshot the current link under root names. It reads
// only the link, so it is safe to call while a store is open on root. It returns an error wrapping
// os.ErrNotExist when root has no current link.
func ActiveSnapshotVersion(root string) (int64, error) {
	dir, _, err := currentSnapshotDir(root)
	if err != nil {
		return 0, fmt.Errorf("resolve the current snapshot under %q: %w", root, err)
	}
	version, err := ParseSnapshotDirName(filepath.Base(dir))
	if err != nil {
		return 0, fmt.Errorf("resolve the current snapshot under %q: %w", root, err)
	}
	return version, nil
}

// SnapshotVersions returns the versions of the complete snapshots under root in ascending order. Only
// directories that ParseSnapshotDirName accepts are listed, so a snapshot still being written or removed
// is not. It is safe to call while a store is open on root.
func SnapshotVersions(root string) ([]int64, error) {
	entries, err := os.ReadDir(root)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("list snapshots under %q: %w", root, err)
	}
	var versions []int64
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if v, err := ParseSnapshotDirName(entry.Name()); err == nil {
			versions = append(versions, v)
		}
	}
	slices.Sort(versions)
	return versions, nil
}

// PinSnapshot hard-links every file of the snapshot at version under root into dst, skipping LOCK
// files. dst must not exist, it must not be inside the snapshot, and it must be on the same filesystem
// as root. The pin keeps its content after the store prunes the snapshot, and it is safe to take while
// a store is open on root.
//
// It returns an error wrapping os.ErrNotExist when the snapshot is absent, or is pruned or replaced
// before the pin is complete. On any error it removes dst.
func PinSnapshot(root string, version int64, dst string) error {
	return pinSnapshotWith(root, version, dst, os.Link)
}

// pinSnapshotWith is PinSnapshot with the hard-link call supplied by the caller.
func pinSnapshotWith(root string, version int64, dst string, link func(oldname, newname string) error) (err error) {
	src := filepath.Join(root, snapshotName(version))
	before, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("pin snapshot %d: %w", version, err)
	}
	if !before.IsDir() {
		return fmt.Errorf("pin snapshot %d: %s is not a directory", version, src)
	}
	inside, err := pathWithin(src, dst)
	if err != nil {
		return fmt.Errorf("pin snapshot %d: resolve %s: %w", version, dst, err)
	}
	if inside {
		return fmt.Errorf("pin snapshot %d: %s is inside the snapshot", version, dst)
	}
	if err := os.Mkdir(dst, 0o750); err != nil {
		return fmt.Errorf("pin snapshot %d: create %s: %w", version, dst, err)
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dst)
		}
	}()

	if err := linkDirContents(src, dst, link); err != nil {
		return fmt.Errorf("pin snapshot %d: %w", version, err)
	}

	// A snapshot is replaced by renaming a new directory over its name, so a pin that read from two
	// directories under one name sees a different inode here.
	after, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("pin snapshot %d: %w", version, err)
	}
	if !os.SameFile(before, after) {
		return fmt.Errorf("pin snapshot %d: the snapshot was replaced during the pin: %w", version, os.ErrNotExist)
	}
	return nil
}

// pathWithin reports whether path is dir or lies under it, with symlinks resolved. dir and the parent of
// path must exist.
func pathWithin(dir, path string) (bool, error) {
	realDir, err := resolvePath(dir)
	if err != nil {
		return false, err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}
	realParent, err := resolvePath(filepath.Dir(absPath))
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(realDir, filepath.Join(realParent, filepath.Base(absPath)))
	if err != nil {
		return false, err
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))), nil
}

// resolvePath returns the absolute path of the existing path p with every symlink resolved.
func resolvePath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// linkDirContents hard-links every regular file under src into the existing directory dst with link,
// recreating subdirectories and skipping LOCK files. It refuses any entry that is not a regular file or a
// directory.
func linkDirContents(src, dst string, link func(oldname, newname string) error) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == lockFileName {
			continue
		}
		srcPath := filepath.Join(src, name)
		dstPath := filepath.Join(dst, name)
		switch mode := entry.Type(); {
		case mode.IsDir():
			if err := os.Mkdir(dstPath, 0o750); err != nil {
				return err
			}
			if err := linkDirContents(srcPath, dstPath, link); err != nil {
				return err
			}
		case mode.IsRegular():
			if err := link(srcPath, dstPath); err != nil {
				if errors.Is(err, syscall.EXDEV) {
					return fmt.Errorf("the pin must be on the same filesystem as the snapshot: %w", err)
				}
				return err
			}
		default:
			return fmt.Errorf("%s is not a regular file or a directory", srcPath)
		}
	}
	return nil
}

// VerifySnapshotDir returns nil when the snapshot directory dir holds every data database and each one
// records version as its committed version. version must be at least 1. It opens each database
// read-only, which leaves a Pebble LOCK file in the database directory.
func VerifySnapshotDir(dir string, version int64) error {
	// A database with no version record reads as 0, so version 0 would accept empty directories.
	if version < 1 {
		return fmt.Errorf("snapshot %s: version %d is below 1", dir, version)
	}
	for _, name := range dataDBDirs {
		dbDir := filepath.Join(dir, name)
		info, err := os.Stat(dbDir)
		if err != nil {
			return fmt.Errorf("snapshot %s: database %s: %w", dir, name, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("snapshot %s: database %s is not a directory", dir, name)
		}
		got, err := readCommittedVersion(dbDir)
		if err != nil {
			return fmt.Errorf("snapshot %s: %w", dir, err)
		}
		if got != version {
			return fmt.Errorf("snapshot %s: database %s is at version %d, want %d", dir, name, got, version)
		}
	}
	return nil
}

// RootLock is the writer lock on a FlatKV root, held without opening a store. While it is held, no store
// can open on the root.
type RootLock struct {
	root string
	lock filelock.TryLockerSafe
}

// LockRoot creates root if it is absent and takes its writer lock. It returns an error wrapping
// commonerrors.ErrFileLockUnavailable when a store or another RootLock holds the lock.
func LockRoot(root string) (*RootLock, error) {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("create flatkv root %q: %w", root, err)
	}
	lock, err := lockDir(root)
	if err != nil {
		return nil, fmt.Errorf("lock flatkv root %q: %w", root, err)
	}
	return &RootLock{root: root, lock: lock}, nil
}

// Unlock releases the writer lock and closes the lock file. The RootLock must not be used afterwards.
func (l *RootLock) Unlock() error {
	unlockErr := l.lock.Unlock()
	if err := errors.Join(unlockErr, l.lock.Destroy()); err != nil {
		return fmt.Errorf("unlock flatkv root %q: %w", l.root, err)
	}
	return nil
}

// Empty reports whether the root holds nothing but its lock file.
func (l *RootLock) Empty() (bool, error) {
	entries, err := os.ReadDir(l.root)
	if err != nil {
		return false, fmt.Errorf("read flatkv root %q: %w", l.root, err)
	}
	for _, entry := range entries {
		if entry.Name() != lockFileName {
			return false, nil
		}
	}
	return true, nil
}

// Clear removes everything under the root except its lock file.
func (l *RootLock) Clear() error {
	entries, err := os.ReadDir(l.root)
	if err != nil {
		return fmt.Errorf("read flatkv root %q: %w", l.root, err)
	}
	for _, entry := range entries {
		if entry.Name() == lockFileName {
			continue
		}
		if err := os.RemoveAll(filepath.Join(l.root, entry.Name())); err != nil {
			return fmt.Errorf("clear flatkv root %q: %w", l.root, err)
		}
	}
	return nil
}

// InstallSnapshot moves the snapshot directory src into the empty root as the snapshot at version and
// makes it the current snapshot. src must pass VerifySnapshotDir at version, must be on the same
// filesystem as the root, and must already be durable: InstallSnapshot syncs only the root directory.
// A store opened on the root afterwards loads at version. On error, src is in place and the root is
// empty, unless the error also reports that the undo failed.
func (l *RootLock) InstallSnapshot(src string, version int64) error {
	return l.installSnapshotWith(src, version, publishCurrent)
}

// installSnapshotWith is InstallSnapshot with the step that makes the moved snapshot current supplied by
// the caller.
func (l *RootLock) installSnapshotWith(src string, version int64, publish func(root, name string) error) error {
	if err := VerifySnapshotDir(src, version); err != nil {
		return fmt.Errorf("install snapshot %d: %w", version, err)
	}
	empty, err := l.Empty()
	if err != nil {
		return fmt.Errorf("install snapshot %d: %w", version, err)
	}
	if !empty {
		return fmt.Errorf("install snapshot %d: flatkv root %q is not empty", version, l.root)
	}

	name := snapshotName(version)
	installed := filepath.Join(l.root, name)
	if err := os.Rename(src, installed); err != nil {
		return fmt.Errorf("install snapshot %d: %w", version, err)
	}
	if err := publish(l.root, name); err != nil {
		if undoErr := l.undoInstall(installed, src); undoErr != nil {
			return fmt.Errorf("install snapshot %d: %w; undo the install: %w", version, err, undoErr)
		}
		return fmt.Errorf("install snapshot %d: %w", version, err)
	}
	return nil
}

// publishCurrent points the current link under root at the snapshot directory name and syncs root.
func publishCurrent(root, name string) error {
	if err := updateCurrentSymlink(root, name); err != nil {
		return err
	}
	return syncDir(root)
}

// undoInstall moves the installed snapshot back to src and removes the current links from the root.
// Without it, a store opened on the root would adopt the snapshot as an orphan and load it.
func (l *RootLock) undoInstall(installed, src string) error {
	// The move goes first: a crash after it leaves a current link to a missing directory, which fails
	// the next store open, where a snapshot left without a link would be adopted and loaded.
	if err := os.Rename(installed, src); err != nil {
		return err
	}
	for _, link := range []string{currentPath(l.root), filepath.Join(l.root, currentTmpLink)} {
		if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func syncDir(dir string) error {
	f, err := os.Open(dir) //nolint:gosec // dir is the FlatKV root the caller holds the lock on
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync %s: %w", dir, err)
	}
	return f.Close()
}

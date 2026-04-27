package operations

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
)

const (
	flatkvSnapshotPrefix = "snapshot-"
	flatkvSnapshotDirLen = len(flatkvSnapshotPrefix) + 20

	// maxCloneRetries bounds the number of retries when the source snapshot
	// is pruned mid-clone by a live writer (atomicRemoveDir race).
	maxCloneRetries = 3
)

// openedFlatKV wraps a temp-cloned FlatKV store used by tooling.
//
// The tools intentionally operate on a temp clone of the selected snapshot +
// WAL so they do not compete with a live node for the FlatKV writer lock.
type openedFlatKV struct {
	*flatkv.CommitStore
	tempDir string
}

func (o *openedFlatKV) Close() error {
	var err error
	if o.CommitStore != nil {
		err = o.CommitStore.Close()
	}
	if o.tempDir != "" {
		if rmErr := os.RemoveAll(o.tempDir); rmErr != nil {
			if err != nil {
				return fmt.Errorf("%w; cleanup temp dir: %w", err, rmErr)
			}
			return fmt.Errorf("cleanup temp dir: %w", rmErr)
		}
	}
	return err
}

// openFlatKVReadOnly opens FlatKV tooling state at the given height.
//
// Instead of opening the source directory directly (which would contend for
// FlatKV's writer lock on a live node), this clones the relevant snapshot and
// changelog into a temp directory and opens that isolated clone.
//
// Consistency on a live node:
//   - snapshot-N/ directories are immutable after creation (Pebble
//     Checkpoint + atomic Rename). Their contents never change; only
//     wholesale pruning via atomicRemoveDir can remove them.
//   - Every file we clone is therefore hard-linked (not byte-copied). A
//     hardlink preserves the inode even if the live node prunes the source
//     snapshot mid-operation, so the tool sees a stable snapshot until it
//     releases its temp dir.
//   - If the whole snapshot directory is renamed to "-removing" between our
//     os.ReadDir and os.Link calls, we surface ENOENT, re-select the
//     snapshot, and retry up to maxCloneRetries times.
//
// height=0 means latest version.
func openFlatKVReadOnly(dbDir string, height int64) (*openedFlatKV, error) {
	tempDir, err := prepareFlatKVToolingClone(dbDir, height)
	if err != nil {
		return nil, err
	}

	cfg := config.DefaultConfig()
	cfg.DataDir = tempDir

	store, err := flatkv.NewCommitStore(context.Background(), cfg)
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to create FlatKV store: %w", err)
	}

	if _, err := store.LoadVersion(height, false); err != nil {
		_ = store.Close()
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to open FlatKV at version %d: %w", height, err)
	}

	return &openedFlatKV{
		CommitStore: store,
		tempDir:     tempDir,
	}, nil
}

func prepareFlatKVToolingClone(dbDir string, height int64) (string, error) {
	return prepareFlatKVToolingCloneWith(dbDir, height, tryPrepareFlatKVToolingClone)
}

func prepareFlatKVToolingCloneWith(dbDir string, height int64, tryClone func(string, int64) (string, error)) (string, error) {
	var lastErr error
	for attempt := 0; attempt < maxCloneRetries; attempt++ {
		tempDir, err := tryClone(dbDir, height)
		if err == nil {
			return tempDir, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		lastErr = err
	}
	return "", fmt.Errorf("clone aborted after %d retries, source kept churning: %w", maxCloneRetries, lastErr)
}

func tryPrepareFlatKVToolingClone(dbDir string, height int64) (string, error) {
	snapshotName, err := selectFlatKVSnapshot(dbDir, height)
	if err != nil {
		return "", err
	}

	tempDir, err := os.MkdirTemp("", "seidb-flatkv-tool-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	cleanup := func(err error) (string, error) {
		_ = os.RemoveAll(tempDir)
		return "", err
	}

	srcSnapshotDir := filepath.Join(dbDir, snapshotName)
	dstSnapshotDir := filepath.Join(tempDir, snapshotName)
	if err := cloneDirRecursive(srcSnapshotDir, dstSnapshotDir); err != nil {
		return cleanup(fmt.Errorf("clone snapshot %s: %w", snapshotName, err))
	}

	if err := os.Symlink(snapshotName, filepath.Join(tempDir, "current")); err != nil {
		return cleanup(fmt.Errorf("create current symlink: %w", err))
	}

	srcChangelogDir := filepath.Join(dbDir, "changelog")
	if info, err := os.Stat(srcChangelogDir); err == nil && info.IsDir() {
		dstChangelogDir := filepath.Join(tempDir, "changelog")
		if err := cloneDirRecursive(srcChangelogDir, dstChangelogDir); err != nil {
			return cleanup(fmt.Errorf("clone changelog: %w", err))
		}
	}

	return tempDir, nil
}

func selectFlatKVSnapshot(dbDir string, height int64) (string, error) {
	if height == 0 {
		target, err := os.Readlink(filepath.Join(dbDir, "current"))
		if err != nil {
			return "", fmt.Errorf("read current symlink: %w", err)
		}
		if !isFlatKVSnapshotName(target) {
			return "", fmt.Errorf("current symlink points to invalid snapshot: %s", target)
		}
		return target, nil
	}

	snapshots := listFlatKVSnapshots(dbDir)
	for i := len(snapshots) - 1; i >= 0; i-- {
		if snapshots[i] <= height {
			return fmt.Sprintf("%s%020d", flatkvSnapshotPrefix, snapshots[i]), nil
		}
	}
	return "", fmt.Errorf("no snapshot found for target version %d", height)
}

func listFlatKVSnapshots(dir string) []int64 {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var versions []int64
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !isFlatKVSnapshotName(name) {
			continue
		}
		v, err := strconv.ParseInt(name[len(flatkvSnapshotPrefix):], 10, 64)
		if err != nil {
			continue
		}
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
	return versions
}

func isFlatKVSnapshotName(name string) bool {
	return strings.HasPrefix(name, flatkvSnapshotPrefix) && len(name) == flatkvSnapshotDirLen
}

// cloneDirRecursive clones a source directory into dst by hardlinking every
// regular file (and falling back to byte-copy only when os.Link fails with
// EXDEV, i.e. the tool temp dir is on a different filesystem).
//
// Hardlinking is safe because:
//   - snapshot-N files are immutable after Pebble Checkpoint + Rename.
//   - changelog WAL segments are append-only; readers tolerate truncated
//     tail records, and rotated segments retain their original inode.
//
// It also lets the tool survive a concurrent atomicRemoveDir on the source:
// once we have hardlinks, the inodes persist until we release the temp dir,
// even if the live node prunes the source snapshot mid-operation.
func cloneDirRecursive(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}
	if err := os.MkdirAll(dst, 0o750); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "LOCK" {
			continue
		}
		srcPath := filepath.Join(src, name)
		dstPath := filepath.Join(dst, name)

		if entry.IsDir() {
			if err := cloneDirRecursive(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}

		if err := linkOrCopy(srcPath, dstPath); err != nil {
			return err
		}
	}
	return nil
}

// linkOrCopy tries os.Link first, falling back to a byte-copy only when the
// link fails because src and dst are on different filesystems (EXDEV). Any
// other error (including ENOENT from a mid-clone prune) is returned as-is so
// prepareFlatKVToolingClone can retry.
func linkOrCopy(src, dst string) error {
	err := os.Link(src, dst)
	if err == nil {
		return nil
	}
	if !isCrossDeviceLinkError(err) {
		return err
	}
	return copyFile(src, dst)
}

func isCrossDeviceLinkError(err error) bool {
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return errors.Is(linkErr.Err, syscall.EXDEV)
	}
	return errors.Is(err, syscall.EXDEV)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // src is selected from a FlatKV snapshot/changelog clone tree.
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst) //nolint:gosec // dst is allocated inside the tool's temporary clone directory.
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

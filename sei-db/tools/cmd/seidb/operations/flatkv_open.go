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

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
)

const (
	flatkvSnapshotPrefix = "snapshot-"
	flatkvSnapshotDirLen = len(flatkvSnapshotPrefix) + 20

	// maxCloneRetries bounds the number of retries when the source snapshot
	// is pruned mid-clone by a live writer (atomicRemoveDir race) or when the
	// live writer truncates the WAL past our snapshot between snapshot and
	// changelog clone steps.
	maxCloneRetries = 3
)

// errSourceChurning marks transient races where the source FlatKV directory
// mutates (snapshot pruned, WAL truncated) between our reads. It is the
// sentinel that prepareFlatKVToolingCloneWith uses to decide whether to
// retry instead of bailing out.
var errSourceChurning = errors.New("flatkv source kept churning during clone")

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
//   - Snapshot files are hard-linked. A hardlink preserves the inode even if
//     the live node prunes the source snapshot mid-operation, so the tool sees
//     a stable snapshot until it releases its temp dir.
//   - Changelog files are byte-copied, not linked, because WAL recovery can
//     truncate a corrupted tail when the cloned store opens.
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
		if !isCloneRetryableError(err) {
			return "", err
		}
		lastErr = err
	}
	return "", fmt.Errorf("clone aborted after %d retries, source kept churning: %w", maxCloneRetries, lastErr)
}

// isCloneRetryableError reports whether err indicates a transient race with
// the live writer that we should retry: either the snapshot or a WAL segment
// vanished mid-read (ENOENT), or our post-clone validation observed the WAL
// being truncated past our snapshot version.
func isCloneRetryableError(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, errSourceChurning)
}

func tryPrepareFlatKVToolingClone(dbDir string, height int64) (string, error) {
	snapshotName, err := selectFlatKVSnapshot(dbDir, height)
	if err != nil {
		return "", err
	}
	snapshotVersion, err := strconv.ParseInt(snapshotName[len(flatkvSnapshotPrefix):], 10, 64)
	if err != nil {
		return "", fmt.Errorf("parse snapshot version from %q: %w", snapshotName, err)
	}

	// Place the temp clone as a sibling of dbDir so it shares a filesystem
	// with the source. os.MkdirTemp("", ...) follows $TMPDIR, which on many
	// Linux deployments is tmpfs; that breaks os.Link with EXDEV and would
	// force a multi-GB byte-copy of the snapshot into RAM.
	cloneRoot := filepath.Dir(dbDir)
	if err := os.MkdirAll(cloneRoot, 0o750); err != nil {
		return "", fmt.Errorf("ensure clone root %s: %w", cloneRoot, err)
	}
	tempDir, err := os.MkdirTemp(cloneRoot, "seidb-flatkv-tool-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir under %s: %w", cloneRoot, err)
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
	info, err := os.Stat(srcChangelogDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return cleanup(fmt.Errorf("stat changelog: %w", err))
	}
	if err == nil && !info.IsDir() {
		return cleanup(fmt.Errorf("changelog path is not a directory: %s", srcChangelogDir))
	}
	if err == nil {
		dstChangelogDir := filepath.Join(tempDir, "changelog")
		if err := copyDirRecursive(srcChangelogDir, dstChangelogDir); err != nil {
			return cleanup(fmt.Errorf("clone changelog: %w", err))
		}
		// Detect the snapshot/WAL race: a live writer can roll a new
		// snapshot between our snapshot clone and our changelog copy and
		// then truncateWAL up to that newer snapshot's version. If that
		// happened, the cloned WAL no longer covers snapshotVersion+1,
		// and a downstream catchup would silently jump over missing
		// versions. Surface it as a retryable error so the outer loop
		// re-selects the snapshot and tries again.
		if err := verifyClonedWALCovers(dstChangelogDir, snapshotVersion); err != nil {
			return cleanup(err)
		}
	}

	return tempDir, nil
}

// verifyClonedWALCovers opens the cloned WAL just long enough to ensure it
// either is empty, ends at or before snapshotVersion (no replay needed), or
// starts at or before snapshotVersion+1 (catchup can resume cleanly).
func verifyClonedWALCovers(dstChangelogDir string, snapshotVersion int64) error {
	walLog, err := wal.NewChangelogWAL(dstChangelogDir, wal.Config{})
	if err != nil {
		return fmt.Errorf("open cloned changelog for validation: %w", err)
	}
	defer func() { _ = walLog.Close() }()

	firstOff, err := walLog.FirstOffset()
	if err != nil {
		return fmt.Errorf("cloned changelog first offset: %w", err)
	}
	lastOff, err := walLog.LastOffset()
	if err != nil {
		return fmt.Errorf("cloned changelog last offset: %w", err)
	}
	if firstOff == 0 || lastOff == 0 || firstOff > lastOff {
		return nil
	}

	firstVer, err := readWALEntryVersion(walLog, firstOff)
	if err != nil {
		return fmt.Errorf("read first cloned changelog entry: %w", err)
	}
	lastVer, err := readWALEntryVersion(walLog, lastOff)
	if err != nil {
		return fmt.Errorf("read last cloned changelog entry: %w", err)
	}

	if lastVer <= snapshotVersion {
		return nil
	}
	if firstVer <= snapshotVersion+1 {
		return nil
	}
	return fmt.Errorf("%w: cloned WAL starts at version %d but snapshot is %d (truncated past snapshot mid-clone)",
		errSourceChurning, firstVer, snapshotVersion)
}

func readWALEntryVersion(walLog wal.ChangelogWAL, off uint64) (int64, error) {
	var ver int64
	err := walLog.Replay(off, off, func(_ uint64, entry proto.ChangelogEntry) error {
		ver = entry.Version
		return nil
	})
	return ver, err
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

// cloneDirRecursive clones an immutable snapshot directory into dst by
// hardlinking every regular file. EXDEV is treated as a fatal configuration
// error: snapshots can be many GB, and the previous behavior of falling back
// to a byte-copy on tmpfs (the historical $TMPDIR default) routinely OOM'd
// nodes and exhausted /tmp. Callers must ensure the tool clone dir lives on
// the same filesystem as the source FlatKV directory.
//
// Hardlinking is safe because:
//   - snapshot-N files are immutable after Pebble Checkpoint + Rename.
//
// It also lets the tool survive a concurrent atomicRemoveDir on the source:
// once we have hardlinks, the inodes persist until we release the temp dir,
// even if the live node prunes the source snapshot mid-operation.
func cloneDirRecursive(src, dst string) error {
	return cloneDirRecursiveWith(src, dst, linkOnly)
}

// copyDirRecursive clones a mutable directory by byte-copying every regular
// file. Changelog files must not share inodes with live WAL segments because
// WAL open/recovery may truncate a corrupted tail in the cloned store.
func copyDirRecursive(src, dst string) error {
	return cloneDirRecursiveWith(src, dst, copyFile)
}

func cloneDirRecursiveWith(src, dst string, cloneFile func(string, string) error) error {
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
			if err := cloneDirRecursiveWith(srcPath, dstPath, cloneFile); err != nil {
				return err
			}
			continue
		}

		if err := cloneFile(srcPath, dstPath); err != nil {
			return err
		}
	}
	return nil
}

// linkOnly hardlinks src to dst and refuses to silently byte-copy if the
// hardlink fails because of a cross-device boundary. Snapshots can be many
// GB; falling back to a copy is unsafe (slow, RAM-intensive, fills tmpfs)
// and is the bug this guard exists to surface. Any other error (including
// ENOENT from a mid-clone prune) is returned as-is so callers can retry.
func linkOnly(src, dst string) error {
	if err := os.Link(src, dst); err != nil {
		if isCrossDeviceLinkError(err) {
			return fmt.Errorf("hardlink %s -> %s failed across filesystems; "+
				"FlatKV tooling requires the temp clone to share a filesystem with the source: %w",
				src, dst, err)
		}
		return err
	}
	return nil
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

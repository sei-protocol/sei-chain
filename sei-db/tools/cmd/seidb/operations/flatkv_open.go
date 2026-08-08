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
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
)

const (
	flatkvSnapshotPrefix = "snapshot-"
	flatkvSnapshotDirLen = len(flatkvSnapshotPrefix) + 20

	// flatkvStateWALName matches the WAL instance name FlatKV opens its state WAL with; it only labels
	// metrics, and the offline GetRange used here does not emit any, but keep it consistent.
	flatkvStateWALName = "flatkv"

	// maxCloneRetries bounds the number of retries when the source snapshot
	// is pruned mid-clone by a live writer (atomicRemoveDir race) or when the
	// live writer truncates the WAL past our snapshot between snapshot and
	// changelog clone steps.
	maxCloneRetries = 3
)

// errSourceChurning marks transient races where the source directory mutates
// (snapshot pruned, WAL truncated) between our reads. It is the sentinel that
// retryToolingClone uses to decide whether to retry instead of bailing out.
var errSourceChurning = errors.New("source kept churning during clone")

// openedFlatKV wraps a temp-cloned FlatKV store used by tooling.
//
// The tools intentionally operate on a temp clone of the selected snapshot +
// WAL so they do not compete with a live node for the FlatKV writer lock.
type openedFlatKV struct {
	flatkv.Store
	clone *toolClone
}

func (o *openedFlatKV) Close() error {
	var err error
	if o.Store != nil {
		err = o.Store.Close()
	}
	if rmErr := o.clone.Remove(); rmErr != nil {
		if err != nil {
			return fmt.Errorf("%w; %w", err, rmErr)
		}
		return rmErr
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
// height=0 means the latest version, best-effort on a live node: the clone is
// a consistent committed prefix as of the copy instant, and a torn-tail
// repair inside the clone (surfaced as a warning) can land it one version
// behind the source tip. Tools print the version actually opened; treat that
// line as authoritative when comparing across nodes.
func openFlatKVReadOnly(dbDir string, height int64) (*openedFlatKV, error) {
	clone, err := prepareFlatKVToolingClone(dbDir, height)
	if err != nil {
		return nil, err
	}
	warnIfCloneRepaired(clone, "flatkv", height)

	cfg := config.DefaultConfig()
	cfg.DataDir = clone.dir

	stateWAL, err := flatkv.OpenStateWAL(cfg)
	if err != nil {
		_ = clone.Remove()
		return nil, fmt.Errorf("failed to open FlatKV state WAL: %w", err)
	}
	primary, err := flatkv.NewCommitStore(context.Background(), cfg, stateWAL)
	if err != nil {
		_ = stateWAL.Close()
		_ = clone.Remove()
		return nil, fmt.Errorf("failed to create FlatKV store: %w", err)
	}

	// The view is built from the clone's snapshot and WAL by primary, which is disposable once the replay
	// has finished: primary was never opened, so it holds no writer lock of its own and the lock it takes
	// lazily is handed to the view.
	view, err := primary.LoadVersionReadOnly(height)
	if err != nil {
		_ = primary.Close()
		_ = clone.Remove()
		return nil, fmt.Errorf("failed to open FlatKV at version %d: %w", height, err)
	}
	if err := primary.Close(); err != nil {
		_ = view.Close()
		_ = clone.Remove()
		return nil, fmt.Errorf("failed to close FlatKV clone writer: %w", err)
	}

	return &openedFlatKV{
		Store: view,
		clone: clone,
	}, nil
}

// warnIfCloneRepaired tells the operator that the cloned changelog had a torn
// tail (the byte-copy raced the live writer mid-append) and was repaired
// inside the clone. For an explicit --height the reached-version checks catch
// any resulting shortfall; for height 0 ("latest") there is no target to
// check against, so the printed version line is the only record of what was
// actually digested.
func warnIfCloneRepaired(clone *toolClone, backend string, height int64) {
	if !clone.walRepaired || height != 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "warning: cloned %s changelog had a torn tail (live writer mid-append) and was repaired in the clone; "+
		"the opened version may trail the source tip by one — trust the printed version line\n", backend)
}

func prepareFlatKVToolingClone(dbDir string, height int64) (*toolClone, error) {
	return retryToolingClone(dbDir, height, tryPrepareFlatKVToolingClone)
}

// retryToolingClone runs tryClone, retrying while the live writer keeps
// mutating the source out from under us. Shared by the FlatKV and memiavl
// tooling clones, which race the same writer in the same ways.
func retryToolingClone(dbDir string, height int64, tryClone func(string, int64) (*toolClone, error)) (*toolClone, error) {
	var lastErr error
	for attempt := 0; attempt < maxCloneRetries; attempt++ {
		clone, err := tryClone(dbDir, height)
		if err == nil {
			return clone, nil
		}
		if !isCloneRetryableError(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, fmt.Errorf("clone aborted after %d retries, source kept churning: %w", maxCloneRetries, lastErr)
}

// isCloneRetryableError reports whether err indicates a transient race with
// the live writer that we should retry: either the snapshot or a WAL segment
// vanished mid-read (ENOENT), or our post-clone validation observed the WAL
// being truncated past our snapshot version.
func isCloneRetryableError(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, errSourceChurning)
}

func tryPrepareFlatKVToolingClone(dbDir string, height int64) (*toolClone, error) {
	snapshotName, err := selectFlatKVSnapshot(dbDir, height)
	if err != nil {
		return nil, err
	}
	snapshotVersion, err := strconv.ParseInt(snapshotName[len(flatkvSnapshotPrefix):], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse snapshot version from %q: %w", snapshotName, err)
	}

	// The clone must sit inside dbDir so it is on the exact same mounted
	// filesystem as the source snapshots (dbDir is often its own mount
	// point, so a sibling directory is not enough and hardlinks would fail
	// across the boundary). selectFlatKVSnapshot already read dbDir, so it
	// is known to exist.
	clone, err := newToolClone(dbDir, ".seidb-flatkv-tool-")
	if err != nil {
		return nil, err
	}
	cleanup := func(err error) (*toolClone, error) {
		_ = clone.Remove()
		return nil, err
	}

	srcSnapshotDir := filepath.Join(dbDir, snapshotName)
	dstSnapshotDir := filepath.Join(clone.dir, snapshotName)
	if err := cloneDirRecursive(srcSnapshotDir, dstSnapshotDir); err != nil {
		return cleanup(fmt.Errorf("clone snapshot %s: %w", snapshotName, err))
	}

	if err := os.Symlink(snapshotName, filepath.Join(clone.dir, "current")); err != nil {
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
		dstChangelogDir := filepath.Join(clone.dir, "changelog")
		if err := copyDirRecursive(srcChangelogDir, dstChangelogDir); err != nil {
			return cleanup(fmt.Errorf("clone changelog: %w", err))
		}
		// Detect the snapshot/WAL race: a live writer can roll a new
		// snapshot between our snapshot clone and our changelog copy and
		// then truncateWAL up to that newer snapshot's version. If that
		// happened, the cloned WAL no longer covers the snapshot's
		// successor version, and a downstream catchup would silently jump
		// over missing versions. Surface it as a retryable error so the
		// outer loop re-selects the snapshot and tries again.
		//
		// FlatKV snapshots are always named with a real committed version —
		// SetInitialVersion(N) seeds committedVersion N-1 and writes
		// snapshot-<N-1> — so the successor is unconditionally
		// snapshotVersion+1 here (unlike memiavl, whose bootstrap
		// snapshot-0 hides a configurable initial version).
		sizeBefore := changelogByteSize(dstChangelogDir)
		if err := verifyClonedWALCovers(dstChangelogDir, snapshotVersion); err != nil {
			return cleanup(err)
		}
		clone.walRepaired = changelogByteSize(dstChangelogDir) < sizeBefore
	}

	return clone, nil
}

// verifyClonedWALCovers inspects the cloned WAL just long enough to ensure it
// either is empty, ends at or before snapshotVersion (no replay needed), or
// starts at or before snapshotVersion+1 (catchup can resume cleanly). The state
// WAL is keyed by block number, so its stored range is directly the version
// range; GetRange reads it offline without a live WAL instance.
func verifyClonedWALCovers(dstChangelogDir string, snapshotVersion int64) error {
	ok, firstVer, lastVer, err := statewal.GetRange(statewal.DefaultConfig(dstChangelogDir, flatkvStateWALName))
	if err != nil {
		return fmt.Errorf("read cloned changelog range: %w", err)
	}
	if !ok {
		return nil
	}
	if int64(lastVer) <= snapshotVersion { //nolint:gosec // version fits int64
		return nil
	}
	if int64(firstVer) <= snapshotVersion+1 { //nolint:gosec // version fits int64
		return nil
	}
	return fmt.Errorf("%w: cloned WAL starts at version %d but snapshot is %d (truncated past snapshot mid-clone)",
		errSourceChurning, firstVer, snapshotVersion)
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
// the same filesystem as the source directory.
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
				"seidb tooling requires the temp clone to share a filesystem with the source: %w",
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

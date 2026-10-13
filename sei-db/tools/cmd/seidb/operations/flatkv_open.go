package operations

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
)

const (
	// flatkvStateWALName matches the WAL instance name FlatKV opens its state WAL with; it only labels
	// metrics, and the offline GetRange used here does not emit any, but keep it consistent.
	flatkvStateWALName = "flatkv"

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
	gigatypes.LiveStateStore
	tempDir string
}

func (o *openedFlatKV) Close() error {
	var err error
	if o.LiveStateStore != nil {
		err = o.LiveStateStore.Close()
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

	stateWAL, err := flatkv.OpenStateWAL(cfg)
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to open FlatKV state WAL: %w", err)
	}
	primary, err := flatkv.NewCommitStore(context.Background(), cfg, stateWAL)
	if err != nil {
		_ = stateWAL.Close()
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to create FlatKV store: %w", err)
	}

	// The view is built from the clone's snapshot and WAL by primary, which is disposable once the replay
	// has finished: primary was never opened, so it holds no writer lock of its own and the lock it takes
	// lazily is handed to the view.
	view, err := primary.LoadVersionReadOnly(height)
	if err != nil {
		_ = primary.Close()
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to open FlatKV at version %d: %w", height, err)
	}
	if err := primary.Close(); err != nil {
		_ = view.Close()
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to close FlatKV clone writer: %w", err)
	}

	return &openedFlatKV{
		LiveStateStore: view,
		tempDir:        tempDir,
	}, nil
}

// OpenFlatKVReadOnly opens an isolated read-only FlatKV view for a seidb
// operation. The caller must close the returned store.
func OpenFlatKVReadOnly(dbDir string, height int64) (gigatypes.LiveStateStore, error) {
	return openFlatKVReadOnly(dbDir, height)
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
	snapshotVersion, err := flatkv.ParseSnapshotDirName(snapshotName)
	if err != nil {
		return "", err
	}

	// Place the temp clone inside dbDir so it is on the exact same mounted
	// filesystem as the source snapshots. A sibling directory is not enough:
	// dbDir itself is often a mount point on dedicated data volumes.
	cloneRoot := dbDir
	if err := os.MkdirAll(cloneRoot, 0o750); err != nil {
		return "", fmt.Errorf("ensure clone root %s: %w", cloneRoot, err)
	}
	tempDir, err := os.MkdirTemp(cloneRoot, ".seidb-flatkv-tool-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir under %s: %w", cloneRoot, err)
	}
	cleanup := func(err error) (string, error) {
		_ = os.RemoveAll(tempDir)
		return "", err
	}

	dstSnapshotDir := filepath.Join(tempDir, snapshotName)
	if err := flatkv.PinSnapshot(dbDir, snapshotVersion, dstSnapshotDir); err != nil {
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
		version, err := flatkv.ActiveSnapshotVersion(dbDir)
		if err != nil {
			return "", fmt.Errorf("read current symlink: %w", err)
		}
		return flatkv.SnapshotDirName(version), nil
	}

	versions, err := flatkv.SnapshotVersions(dbDir)
	if err != nil {
		return "", err
	}
	for i := len(versions) - 1; i >= 0; i-- {
		if versions[i] <= height {
			return flatkv.SnapshotDirName(versions[i]), nil
		}
	}
	return "", fmt.Errorf("no snapshot found for target version %d", height)
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

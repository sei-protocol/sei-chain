package flatkv

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
	"sync"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
	"go.opentelemetry.io/otel/metric"
)

// On-disk layout under <home>/flatkv/:
//
//	flatkv/
//	  current -> snapshot-NNNNN              (symlink to active snapshot)
//	  snapshot-NNNNN/                        (immutable checkpoint)
//	    account/                             (PebbleDB: addr → AccountValue)
//	    code/                                (PebbleDB: addr → bytecode)
//	    storage/                             (PebbleDB: addr||slot → value)
//	    misc/                                (PebbleDB: full key → value)
//	  working/                               (mutable clone of active snapshot)
//	    account/, code/, storage/, misc/
//	    SNAPSHOT_BASE                        (records source snapshot name)
//	  changelog/                             (WAL, shared across snapshots)
const (
	// snapshotPrefix is the directory name prefix for versioned snapshots.
	snapshotPrefix = "snapshot-"
	// snapshotDirLen is the full directory name length: "snapshot-" + 20-digit zero-padded version.
	snapshotDirLen = len(snapshotPrefix) + 20

	// currentLink is the symlink name pointing to the active snapshot directory.
	currentLink = "current"
	// currentTmpLink is a temporary symlink used during atomic swap of currentLink.
	currentTmpLink = "current-tmp"

	// workingDirName is cloned from the baseline snapshot on each open().
	// Mutable DB operations go here, keeping snapshot dirs immutable.
	workingDirName = "working"

	// snapshotBaseFile records which snapshot the working dir was cloned from.
	// When the current symlink still points at the same snapshot, we skip
	// the expensive RemoveAll+re-clone on restart because WAL catchup is
	// idempotent and will bring the working dir up to date.
	snapshotBaseFile = "SNAPSHOT_BASE"
)

func snapshotName(version int64) string {
	return fmt.Sprintf("%s%020d", snapshotPrefix, version)
}

func isSnapshotName(name string) bool {
	return strings.HasPrefix(name, snapshotPrefix) && len(name) == snapshotDirLen
}

func parseSnapshotVersion(name string) (int64, error) {
	if !isSnapshotName(name) {
		return 0, fmt.Errorf("invalid snapshot name: %s", name)
	}
	v, err := strconv.ParseInt(name[len(snapshotPrefix):], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse snapshot version %q: %w", name, err)
	}
	return v, nil
}

func currentPath(root string) string {
	return filepath.Join(root, currentLink)
}

// currentSnapshotDir reads the current symlink and returns the full path
// and parsed version. Returns os.ErrNotExist if the symlink does not exist.
func currentSnapshotDir(root string) (dir string, version int64, err error) {
	target, err := os.Readlink(currentPath(root))
	if err != nil {
		return "", 0, err
	}
	version, err = parseSnapshotVersion(target)
	if err != nil {
		return "", 0, err
	}
	return filepath.Join(root, target), version, nil
}

// seekSnapshot finds the highest snapshot version <= targetVersion.
// Returns 0 and an error if no qualifying snapshot exists.
func seekSnapshot(root string, targetVersion int64) (int64, error) {
	var found int64
	var ok bool
	err := traverseSnapshots(root, false, func(version int64) (stop bool, err error) {
		if version <= targetVersion {
			found = version
			ok = true
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("no snapshot found for target version %d", targetVersion)
	}
	return found, nil
}

// traverseSnapshots iterates snapshot directories in the given order.
// ascending=true  -> lowest version first
// ascending=false -> highest version first
// The callback returns (stop, err). Traversal halts on stop=true or err!=nil.
func traverseSnapshots(dir string, ascending bool, fn func(int64) (bool, error)) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	versions := make([]int64, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || !isSnapshotName(e.Name()) {
			continue
		}
		v, err := parseSnapshotVersion(e.Name())
		if err != nil {
			continue
		}
		versions = append(versions, v)
	}

	sort.Slice(versions, func(i, j int) bool {
		if ascending {
			return versions[i] < versions[j]
		}
		return versions[i] > versions[j]
	})

	for _, v := range versions {
		stop, err := fn(v)
		if err != nil {
			return err
		}
		if stop {
			return nil
		}
	}
	return nil
}

// updateCurrentSymlink atomically updates the current symlink to point at snapshotDir.
// snapshotDir should be the bare directory name (e.g. "snapshot-00000000000000000100"),
// not a full path.
func updateCurrentSymlink(root, snapshotDir string) error {
	tmpPath := filepath.Join(root, currentTmpLink)
	if _, err := os.Lstat(tmpPath); err == nil {
		if err := os.Remove(tmpPath); err != nil {
			return fmt.Errorf("remove stale tmp symlink: %w", err)
		}
	}
	if err := os.Symlink(snapshotDir, tmpPath); err != nil {
		return fmt.Errorf("create tmp symlink: %w", err)
	}
	if err := os.Rename(tmpPath, currentPath(root)); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename tmp symlink to current: %w", err)
	}
	return nil
}

// removeTmpDirs removes any directories ending in "-tmp" or "-removing"
// left over from interrupted snapshot writes or deletes.
func removeTmpDirs(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() && (strings.HasSuffix(name, tmpSuffix) || strings.HasSuffix(name, removingSuffix)) {
			if err := os.RemoveAll(filepath.Join(dir, name)); err != nil {
				errs = append(errs, fmt.Errorf("remove tmp dir %s: %w", name, err))
			}
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// resolveSnapshotToClone returns the directory of the snapshot a read-only view of targetVersion
// opens against: the newest snapshot at or below it, or the active snapshot when targetVersion is 0.
func resolveSnapshotToClone(root string, targetVersion int64) (string, error) {
	if targetVersion <= 0 {
		snapDir, _, err := currentSnapshotDir(root)
		if err != nil {
			return "", fmt.Errorf("resolve current snapshot for readonly: %w", err)
		}
		return snapDir, nil
	}
	baseVersion, err := seekSnapshot(root, targetVersion)
	if err != nil {
		return "", fmt.Errorf("seek snapshot for readonly: %w", err)
	}
	return filepath.Join(root, snapshotName(baseVersion)), nil
}

// createWorkingDir ensures a mutable working directory exists, cloned from
// snapDir. If the working dir already exists and was cloned from the same
// snapshot (recorded in SNAPSHOT_BASE), the expensive re-clone is skipped
// because WAL catchup is idempotent and will bring data up to date.
func createWorkingDir(snapDir, workDir string) error {
	snapBase := filepath.Base(snapDir)
	if reuseWorkingDir(workDir, snapBase) {
		return nil
	}

	_ = os.RemoveAll(workDir)

	if err := os.MkdirAll(workDir, 0750); err != nil {
		return err
	}

	for _, sub := range dataDBDirs {
		srcPath := filepath.Join(snapDir, sub)
		dstPath := filepath.Join(workDir, sub)

		if _, err := os.Stat(srcPath); os.IsNotExist(err) {
			if mkErr := os.MkdirAll(dstPath, 0750); mkErr != nil {
				return fmt.Errorf("create empty %s: %w", sub, mkErr)
			}
			continue
		}

		if err := cloneDir(srcPath, dstPath); err != nil {
			return fmt.Errorf("clone %s: %w", sub, err)
		}
	}

	return writeSnapshotBase(workDir, snapBase)
}

// reuseWorkingDir returns true if workDir exists and was cloned from the
// same snapshot, meaning a full re-clone can be skipped.
func reuseWorkingDir(workDir, snapBase string) bool {
	data, err := os.ReadFile(filepath.Join(workDir, snapshotBaseFile)) //nolint:gosec // path built from internal working dir layout
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == snapBase
}

func writeSnapshotBase(workDir, snapBase string) error {
	return os.WriteFile(filepath.Join(workDir, snapshotBaseFile), []byte(snapBase+"\n"), 0600)
}

// cloneDir copies a single PebbleDB directory. Immutable .sst files are
// hard-linked; everything else is byte-copied. LOCK files are skipped.
func cloneDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0750); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "LOCK" {
			continue
		}

		srcPath := filepath.Join(src, name)
		dstPath := filepath.Join(dst, name)

		if strings.HasSuffix(name, ".sst") {
			if linkErr := os.Link(srcPath, dstPath); linkErr == nil {
				continue
			}
			// Fall back to copy if hardlink fails (e.g. cross-device).
		}

		if err := copyFile(srcPath, dstPath); err != nil {
			return fmt.Errorf("copy %s: %w", name, err)
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // path built from internal snapshot layout
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst) //nolint:gosec // path built from internal snapshot layout
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// atomicRemoveDir renames the directory to a trash name then removes it,
// preventing half-deleted snapshots on crash.
func atomicRemoveDir(path string) error {
	trashPath := path + removingSuffix
	_ = os.RemoveAll(trashPath)
	if err := os.Rename(path, trashPath); err != nil {
		return err
	}
	return os.RemoveAll(trashPath)
}

// resolveSnapshotDir returns the full path to the active snapshot directory.
// It handles three cases: (1) current symlink exists, (2) recovery of an
// orphaned snapshot whose symlink was never created, or (3) initialization of a
// fresh empty snapshot.
func (s *CommitStore) resolveSnapshotDir(flatkvDir string) (string, error) {
	snapDir, _, err := currentSnapshotDir(flatkvDir)
	if err == nil {
		return snapDir, nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("read current symlink: %w", err)
	}

	// Check for an orphaned snapshot directory — this happens when a previous
	// write moved everything into place but crashed before creating the symlink.
	var latestSnap int64 = -1
	_ = traverseSnapshots(flatkvDir, false, func(v int64) (bool, error) {
		latestSnap = v
		return true, nil
	})
	if latestSnap >= 0 {
		snapName := snapshotName(latestSnap)
		if err := updateCurrentSymlink(flatkvDir, snapName); err != nil {
			return "", fmt.Errorf("recover orphaned snapshot symlink: %w", err)
		}
		logger.Info("FlatKV: recovered orphaned snapshot", "snapshot", snapName)
		return filepath.Join(flatkvDir, snapName), nil
	}

	initSnap := snapshotName(0)
	initDir := filepath.Join(flatkvDir, initSnap)
	for _, sub := range dataDBDirs {
		if err := os.MkdirAll(filepath.Join(initDir, sub), 0750); err != nil {
			return "", fmt.Errorf("create initial snapshot subdir %s: %w", sub, err)
		}
	}
	if err := updateCurrentSymlink(flatkvDir, initSnap); err != nil {
		return "", fmt.Errorf("init current symlink: %w", err)
	}
	return initDir, nil
}

// outOfBandSnapshot writes a snapshot of the committed state and does not return until it is on disk,
// whatever snapshot interval is configured. Snapshots are always stored under the flatkv root
// (e.g. flatkv/snapshot-00000000000000000100).
//
// NOT SAFE on a live store. It reads lastSealed and checkpoints the databases without taking s.mu, and
// it publishes into the same snapshot tree the background writer owns, so a concurrent Commit,
// ApplyChangeSets or read races it. The caller must have quiesced the store. It exists for the two
// bootstrap paths that need a snapshot at a height the cadence would decline — the end of an import,
// and a seeded initial version — and it must not grow a third caller that is merely "convenient".
func (s *CommitStore) outOfBandSnapshot() (err error) {
	if s.readOnly {
		return errReadOnly
	}

	// Let the cadence-driven writer finish whatever it has in flight. It writes into the same snapshot
	// tree this is about to publish into, and only one writer of that tree may run at a time.
	//
	// The flush does not cover a retention cut line the collector may hand the writer, which arrives on
	// its own channel. That one is safe to overlap: it deletes strictly below the active snapshot while
	// this publishes above it, so a publication racing it can only make it delete less.
	if s.snapshotWriter != nil {
		if err := s.snapshotWriter.Flush(); err != nil {
			return fmt.Errorf("await pending snapshot: %w", err)
		}
	}

	blockView, err := s.lastSealed.get()
	if err != nil {
		return fmt.Errorf("read latest sealed view: %w", err)
	}
	version := blockView.blockHeight

	obs := s.observeOp("snapshot", otelMetrics.SnapshotWriteLatency, "version", version)
	defer obs.done(&err, func() {
		otelMetrics.CurrentSnapshotHeight.Record(s.ctx, version)
	})

	tmpPath, err := checkpointDatabases(
		s.ctx, s.flatkvDir(), blockView, s.checkpointables(), s.phaseTimer)
	if err != nil {
		// Error is fatal; leaking reservations doesn't make it worse.
		return fmt.Errorf("checkpoint databases at version %d: %w", version, err)
	}
	if err := blockView.release(); err != nil {
		return fmt.Errorf("release latest sealed view: %w", err)
	}
	pruned, err := publishSnapshot(
		s.ctx, s.flatkvDir(), s.config.SnapshotKeepRecent, s.config.ExternalPruning, version, tmpPath)
	if err != nil {
		return fmt.Errorf("publish snapshot at version %d: %w", version, err)
	}

	logger.Info("FlatKV snapshot created",
		"version", version, "pruned", pruned, "elapsed", obs.elapsed())
	return nil
}

// checkpointDatabases() copies every database at blockView's height into a fresh temporary directory and
// returns its path. The directory is removed again if any part of the copy fails.
//
// The caller must hold a reservation on blockView, and must keep holding it until this returns. That is
// what stops a later block reaching Pebble mid-copy, and so what makes the result a view of exactly
// this version rather than of no single moment.
//
// phaseTimer reports the two halves of the call separately — waiting for the databases to reach this
// version, then copying them — because the reservation is held across both and they are the same
// duration to a caller measuring only the total. It may be nil.
func checkpointDatabases(
	ctx context.Context,
	dir string,
	blockView *storeView,
	dbs map[string]types.Checkpointable,
	phaseTimer *metrics.PhaseTimer,
) (_ string, err error) {
	version := blockView.blockHeight

	// The databases are already flushing this block in the background; this waits for them to finish.
	// On return Pebble holds exactly this block, and stays there while the reservations are held.
	phaseTimer.SetPhase("snapshot_await_flush")
	if flushErr := blockView.awaitFlush(ctx); flushErr != nil {
		return "", fmt.Errorf("await flush at version %d: %w", version, flushErr)
	}
	phaseTimer.SetPhase("snapshot_copy_databases")

	tmpPath := filepath.Join(dir, snapshotName(version)) + tmpSuffix
	_ = os.RemoveAll(tmpPath)
	if mkErr := os.MkdirAll(tmpPath, 0750); mkErr != nil {
		return "", fmt.Errorf("create snapshot tmp dir: %w", mkErr)
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(tmpPath)
		}
	}()

	// Copied concurrently: the pin holds every database at this version for the whole call, so the
	// copies describe one moment no matter what order they run in. Serially, the pin — and with it the
	// stall on every later block's flush — would last the sum of the four rather than the longest.
	errs := make([]error, len(dataDBDirs))
	var wg sync.WaitGroup
	for i, name := range dataDBDirs {
		idx, dbName := i, name
		wg.Add(1)
		go func() {
			defer wg.Done()
			db, ok := dbs[dbName]
			if !ok {
				errs[idx] = fmt.Errorf("no checkpointable handle for db %s", dbName)
				return
			}
			if cpErr := db.Checkpoint(filepath.Join(tmpPath, dbName)); cpErr != nil {
				errs[idx] = fmt.Errorf("checkpoint %s: %w", dbName, cpErr)
			}
		}()
	}
	wg.Wait()
	if err = errors.Join(errs...); err != nil {
		return "", fmt.Errorf("checkpoint databases at version %d: %w", version, err)
	}
	return tmpPath, nil
}

// publishSnapshot makes a completed checkpoint directory the active snapshot: it takes the versioned
// name, the current symlink comes to point at it, and snapshots beyond the retention count are
// removed. Reports how many were removed.
//
// It touches no database, so a caller holding reservations may hand them back before calling this.
func publishSnapshot(
	ctx context.Context,
	dir string,
	keepRecent uint32,
	externalPruning bool,
	version int64,
	tmpPath string,
) (pruned int, err error) {
	defer func() {
		if err != nil {
			_ = os.RemoveAll(tmpPath)
		}
	}()

	snapDir := snapshotName(version)
	finalPath := filepath.Join(dir, snapDir)

	_ = atomicRemoveDir(finalPath) // idempotent: stale final may exist
	if err = os.Rename(tmpPath, finalPath); err != nil {
		return 0, fmt.Errorf("rename snapshot dir: %w", err)
	}

	if err = updateCurrentSymlink(dir, snapDir); err != nil {
		return 0, fmt.Errorf("update current symlink: %w", err)
	}

	// Keep SNAPSHOT_BASE in sync so the next restart reuses the working dir
	// instead of re-cloning from the snapshot and replaying the full WAL gap.
	workDir := filepath.Join(dir, workingDirName)
	if baseErr := writeSnapshotBase(workDir, snapDir); baseErr != nil {
		logger.Error("failed to update SNAPSHOT_BASE", "err", baseErr)
	}

	return pruneSnapshotsByCount(ctx, dir, keepRecent, externalPruning, version), nil
}

// pruneSnapshotsByCount removes old snapshots beyond SnapshotKeepRecent, keeping
// the latest snapshot (currentVersion) plus the N most recent older ones.
// Best-effort: errors are logged but do not fail the snapshot operation.
//
// Only snapshots strictly below currentVersion are candidates. A snapshot above it is either a rewrite in
// progress or a remnant of a rollback that could not finish, and neither is this function's to reclaim:
// counting one as "old" would spend a keep slot on it and evict a genuinely older snapshot that rollback
// still needs as a base. memiavl's pruneSnapshots applies the same guard.
//
// Does nothing when externalPruning is set, which hands retention to the
// StorageGarbageCollector and its by-block-height PruneSnapshots.
func pruneSnapshotsByCount(
	ctx context.Context,
	dir string,
	keepRecent uint32,
	externalPruning bool,
	currentVersion int64,
) int {
	if externalPruning {
		return 0
	}

	start := time.Now()
	defer func() {
		otelMetrics.SnapshotPruneLatency.Record(ctx, secondsSince(start))
	}()

	keep := int(keepRecent)
	pruned := 0

	var older []int64
	if err := traverseSnapshots(dir, false, func(v int64) (bool, error) {
		if v < currentVersion {
			older = append(older, v)
		}
		return false, nil
	}); err != nil {
		logger.Error("prune snapshots: failed to list snapshot dirs", "err", err)
		return 0
	}

	if len(older) <= keep {
		return 0
	}

	for _, v := range older[keep:] {
		snapPath := filepath.Join(dir, snapshotName(v))
		err := atomicRemoveDir(snapPath)
		otelMetrics.SnapshotPruneAttempts.Add(ctx, 1,
			metric.WithAttributes(successAttr(err)))
		if err != nil {
			logger.Error("prune snapshot failed", "version", v, "err", err)
		} else {
			pruned++
			logger.Info("pruned old snapshot", "version", v)
		}
	}
	return pruned
}

// rollbackBaseVersion returns the snapshot version Rollback should rewind to for targetVersion, and reports
// an error if the target cannot be reached from it. A target is reachable when a snapshot at or below it
// exists and either sits exactly on it, or the WAL still holds every block between that snapshot and the
// target. With a nil WAL there is no replay, so only a snapshot sitting exactly on the target qualifies.
// When the only snapshot behind the target is the initial one, the store's history starts at the WAL's first
// block rather than at block 1, so a target is reachable from there iff the WAL holds it.
//
// It reads only: no snapshot, symlink, or WAL state is modified, so Rollback can consult it before touching
// anything and refuse an impossible target outright.
func (s *CommitStore) rollbackBaseVersion(dir string, targetVersion int64) (int64, error) {
	if targetVersion < 1 {
		return 0, fmt.Errorf("rollback target %d is invalid: version 0 means no state, so there is nothing "+
			"to roll back to", targetVersion)
	}

	baseVersion, err := seekSnapshot(dir, targetVersion)
	if err != nil {
		return 0, fmt.Errorf("seek snapshot for rollback: %w", err)
	}
	if baseVersion == targetVersion {
		return baseVersion, nil
	}

	// The snapshot lands below the target, so the WAL has to supply the blocks in between.
	if s.wal == nil {
		return 0, fmt.Errorf("cannot roll back to version %d: nearest snapshot is %d and this store has no "+
			"WAL to replay the difference", targetVersion, baseVersion)
	}
	ok, first, last, err := s.wal.GetStoredRange()
	if err != nil {
		return 0, fmt.Errorf("read WAL range for rollback: %w", err)
	}
	if !ok {
		return 0, fmt.Errorf("cannot roll back to version %d: nearest snapshot is %d and the WAL is empty, "+
			"so blocks %d-%d are unavailable", targetVersion, baseVersion, baseVersion+1, targetVersion)
	}
	needFrom := uint64(baseVersion) + 1 //nolint:gosec // baseVersion >= 0
	needTo := uint64(targetVersion)     //nolint:gosec // targetVersion >= 1 checked above
	if first > needFrom || last < needTo {
		return 0, fmt.Errorf("cannot roll back to version %d: nearest snapshot is %d, so blocks %d-%d are "+
			"needed, but the WAL only holds %d-%d",
			targetVersion, baseVersion, needFrom, needTo, first, last)
	}
	return baseVersion, nil
}

// Rollback restores state to targetVersion by rewinding to the highest
// snapshot <= targetVersion, replaying WAL to reach the target, and
// truncating all WAL entries and snapshots beyond that point.
//
// An unreachable target is rejected before anything is modified.
//
// Not safe to call concurrently with commits, reads or exports: it closes,
// prunes and reopens the store's WAL, reassigning s.wal, so the caller must
// have quiesced the store. This is how it is used today — recovery at
// LoadVersion time — and long term rollback becomes a construction-time
// concern rather than an action on a live store.
//
// Crash safety: the WAL is truncated BEFORE catchup writes any data to
// PebbleDB. If the process crashes after truncation but before catchup
// completes, the next restart will simply re-run catchup against the
// already-truncated WAL, converging to targetVersion.
//
// A failure while resetting the WAL leaves the store mid-rollback: "current" and the working directory are
// already at the rollback snapshot while the WAL still holds the blocks past targetVersion, and s.wal is
// closed. Retrying in-process does not work, because establishing reachability reads the WAL's stored range
// and that now fails as closed. No block is lost: the un-pruned WAL still holds them, so a restart replays
// back to the old tail and the rollback can be retried. The errors from that window say so. Snapshots above
// the target are already gone by then, which costs a cached checkpoint the next snapshot rebuilds, not
// history.
func (s *CommitStore) Rollback(targetVersion int64) (err error) {
	obs := s.observeOp("Rollback", otelMetrics.RollbackLatency,
		"targetVersion", targetVersion)
	defer obs.done(&err, func() {
		otelMetrics.CurrentVersion.Record(s.ctx, s.committedVersion)
	})

	if s.readOnly {
		return errReadOnly
	}
	logger.Info("FlatKV Rollback", "targetVersion", targetVersion)

	dir := s.flatkvDir()

	// Establish reachability first: everything below this point mutates the store irreversibly, and closing
	// the DBs would leave it unusable on an early return.
	baseVersion, err := s.rollbackBaseVersion(dir, targetVersion)
	if err != nil {
		return err
	}

	if err := s.closeDBsOnly(); err != nil {
		return fmt.Errorf("close before rollback: %w", err)
	}

	if err := updateCurrentSymlink(dir, snapshotName(baseVersion)); err != nil {
		return fmt.Errorf("update current symlink for rollback: %w", err)
	}

	// Force a fresh working dir clone from the rollback snapshot: the
	// current working dir may contain data beyond targetVersion.
	if err := os.Remove(filepath.Join(dir, workingDirName, snapshotBaseFile)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove SNAPSHOT_BASE for rollback: %w", err)
	}

	if err := removeSnapshotsAbove(dir, targetVersion); err != nil {
		return err
	}

	if err := s.open(); err != nil {
		return fmt.Errorf("open for rollback: %w", err)
	}

	// Reset the WAL to targetVersion BEFORE catchup: drop every block after it so a later open-to-latest
	// can't replay past target and the write head resumes at targetVersion+1. Rollback is a startup/offline
	// operation (no concurrent commits), so rather than mutating a live instance we close the WAL, prune it
	// offline, and reopen it. The store owns the injected WAL and its location (see NewCommitStore), so the
	// reopen goes through the same config. The prune only ever runs for a target rollbackBaseVersion already
	// established is reachable, including the case where the target predates every retained block and the
	// prune empties the WAL. Skipped when the WAL is nil — the outer context owns it.
	if s.wal != nil {
		cfg := stateWALConfig(s.config.DataDir)
		if err := s.wal.Close(); err != nil {
			return fmt.Errorf("rollback to version %d (from snapshot %d): close WAL at %s: %w; "+
				"store is mid-rollback, restart to recover then retry",
				targetVersion, baseVersion, cfg.Path, err)
		}
		if err := statewal.PruneAfter(cfg.Path, uint64(targetVersion)); err != nil { //nolint:gosec // targetVersion >= 0}
			return fmt.Errorf("rollback to version %d (from snapshot %d): prune WAL at %s: %w; "+
				"store is mid-rollback, restart to recover then retry",
				targetVersion, baseVersion, cfg.Path, err)
		}
		w, err := statewal.New(cfg)
		if err != nil {
			return fmt.Errorf("rollback to version %d (from snapshot %d): reopen pruned WAL at %s: %w; "+
				"store is mid-rollback, restart to recover then retry",
				targetVersion, baseVersion, cfg.Path, err)
		}
		s.wal = w
	}

	if err := s.replayIntoMutableStore(targetVersion); err != nil {
		return fmt.Errorf("catchup after rollback: %w", err)
	}

	if s.committedVersion != targetVersion {
		return fmt.Errorf("rollback failed: wanted version %d but reached %d (WAL may be incomplete)",
			targetVersion, s.committedVersion)
	}

	logger.Info("FlatKV Rollback complete",
		"version", s.committedVersion,
		"elapsed", obs.elapsed())
	return nil
}

// removeSnapshotsAbove deletes every snapshot directory above targetVersion.
//
// A failure here is returned rather than logged, which is why the step runs before the WAL is pruned: an
// error then means the rollback did not take effect, so a caller that skips its own post-rollback bookkeeping
// on error is right to. `seid rollback` in particular rewinds the app before Tendermint, and aborting between
// those two would leave the two heights apart; running here it aborts before either moves. Reporting success
// with a snapshot above the target still on disk would break the same contract from the other side.
//
// Every candidate is attempted even after one fails, because their removals are independent and the caller
// needs the whole list to reconcile from, not just the first name. This mirrors removeTmpDirs.
func removeSnapshotsAbove(dir string, targetVersion int64) error {
	var errs []error
	if err := traverseSnapshots(dir, true, func(v int64) (bool, error) {
		if v <= targetVersion {
			return false, nil
		}
		if err := atomicRemoveDir(filepath.Join(dir, snapshotName(v))); err != nil {
			errs = append(errs, fmt.Errorf("remove snapshot %d above rollback target %d: %w", v, targetVersion, err))
		}
		return false, nil
	}); err != nil {
		return fmt.Errorf("list snapshots above rollback target %d: %w", targetVersion, err)
	}
	return errors.Join(errs...)
}

// tryTruncateWAL truncates WAL entries older than the earliest snapshot, keeping enough entries for
// rollback to any retained snapshot. Skipped when there is no snapshot to truncate against.
//
// Does nothing when config.ExternalPruning is set, under which the collector prunes the WAL as a
// managed store in its own right.
func (s *CommitStore) tryTruncateWAL() {
	if s.wal == nil || s.config.ExternalPruning {
		return
	}

	dir := s.flatkvDir()

	// Find the earliest (lowest-version) snapshot — we must keep WAL blocks from that point onward so
	// rollback to it is possible.
	var earliestSnapVersion int64
	_ = traverseSnapshots(dir, true, func(v int64) (bool, error) {
		earliestSnapVersion = v
		return true, nil
	})
	if earliestSnapVersion <= 0 {
		return
	}

	// Index == version, so prune below the earliest snapshot directly — no offset mapping.
	if err := s.wal.Prune(uint64(earliestSnapVersion)); err != nil { //nolint:gosec // earliestSnapVersion > 0
		// A prune only fails when the WAL is already dead or shutting down, so this is not a retryable
		// housekeeping miss. Name the consequence: the next commit fails at its WAL write with this same
		// cause, and that failure would otherwise look unrelated to this line.
		logger.Error("WAL is unusable; FlatKV commits will fail from here",
			"err", err, "lowestBlockToKeep", earliestSnapVersion)
	}
}

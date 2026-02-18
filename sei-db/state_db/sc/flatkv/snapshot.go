package flatkv

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	db_engine "github.com/sei-protocol/sei-chain/sei-db/db_engine"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
)

const (
	snapshotPrefix = "snapshot-"
	snapshotDirLen = len(snapshotPrefix) + 20

	currentLink    = "current"
	currentTmpLink = "current-tmp"
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

	var versions []int64
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
	_ = os.Remove(tmpPath)
	if err := os.Symlink(snapshotDir, tmpPath); err != nil {
		return fmt.Errorf("create tmp symlink: %w", err)
	}
	if err := os.Rename(tmpPath, currentPath(root)); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename tmp symlink to current: %w", err)
	}
	return nil
}

// snapshotDBDirs lists the DB subdirectory names included in a snapshot.
var snapshotDBDirs = []string{accountDBDir, codeDBDir, storageDBDir, metadataDir}

// removeTmpDirs removes any directories ending in "-tmp" or "-removing"
// left over from interrupted snapshot writes or deletes.
func removeTmpDirs(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() && (strings.HasSuffix(name, "-tmp") || strings.HasSuffix(name, "-removing")) {
			_ = os.RemoveAll(filepath.Join(dir, name))
		}
	}
	return nil
}

// atomicRemoveDir renames the directory to a trash name then removes it,
// so a crash mid-delete doesn't leave a half-deleted snapshot.
// Uses "-removing" suffix to avoid collision with "-tmp" used during writes.
func atomicRemoveDir(path string) error {
	trashPath := path + "-removing"
	_ = os.RemoveAll(trashPath)
	if err := os.Rename(path, trashPath); err != nil {
		return err
	}
	return os.RemoveAll(trashPath)
}

// ensureSnapshotDir returns the full path to the active snapshot directory,
// performing migration or initialization as needed. It also detects and
// recovers from partial migrations where the process crashed after moving
// some directories but before creating the current symlink.
func (s *CommitStore) ensureSnapshotDir(flatkvDir string) (string, error) {
	snapDir, _, err := currentSnapshotDir(flatkvDir)
	if err == nil {
		return snapDir, nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("read current symlink: %w", err)
	}

	// current symlink does not exist.

	// Check for pre-snapshot flat layout (any DB subdir at the flat level).
	hasFlatDirs := false
	for _, sub := range snapshotDBDirs {
		if _, err := os.Stat(filepath.Join(flatkvDir, sub)); err == nil {
			hasFlatDirs = true
			break
		}
	}
	if hasFlatDirs {
		return s.migrateFlatLayout(flatkvDir)
	}

	// No flat dirs. Check for an orphaned snapshot directory — this happens
	// when a previous migration moved all dirs but crashed before creating
	// the current symlink.
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
		s.log.Info("FlatKV: recovered orphaned snapshot", "snapshot", snapName)
		return filepath.Join(flatkvDir, snapName), nil
	}

	// Fresh node: create an initial empty snapshot directory.
	initSnap := snapshotName(0)
	initDir := filepath.Join(flatkvDir, initSnap)
	for _, sub := range snapshotDBDirs {
		if err := os.MkdirAll(filepath.Join(initDir, sub), 0750); err != nil {
			return "", fmt.Errorf("create initial snapshot subdir %s: %w", sub, err)
		}
	}
	if err := updateCurrentSymlink(flatkvDir, initSnap); err != nil {
		return "", fmt.Errorf("init current symlink: %w", err)
	}
	return initDir, nil
}

// migrateFlatLayout moves the existing flat DB directories
// (account/, code/, storage/, metadata/) into a snapshot directory and
// creates the current symlink.
//
// The function is idempotent: directories that were already moved by a
// previous partial attempt are skipped, so recovery from a mid-migration
// crash completes the remaining moves.
func (s *CommitStore) migrateFlatLayout(flatkvDir string) (string, error) {
	s.log.Info("FlatKV: migrating from flat layout to snapshot layout")

	// Determine version for the snapshot name. The metadata DB might still
	// be at the flat location or might have been moved in a prior attempt.
	var version int64
	metaPath := filepath.Join(flatkvDir, metadataDir)
	if tmpMeta, err := pebbledb.Open(metaPath, db_engine.OpenOptions{}); err == nil {
		verData, verErr := tmpMeta.Get([]byte(MetaGlobalVersion))
		_ = tmpMeta.Close()
		if verErr == nil && len(verData) == 8 {
			version = int64(binary.BigEndian.Uint64(verData))
		}
	} else {
		// Metadata already moved — look for the snapshot dir from a prior attempt.
		_ = traverseSnapshots(flatkvDir, false, func(v int64) (bool, error) {
			version = v
			return true, nil
		})
	}

	snapName := snapshotName(version)
	snapDir := filepath.Join(flatkvDir, snapName)
	if err := os.MkdirAll(snapDir, 0750); err != nil {
		return "", fmt.Errorf("migration: create snapshot dir: %w", err)
	}

	for _, sub := range snapshotDBDirs {
		src := filepath.Join(flatkvDir, sub)
		dst := filepath.Join(snapDir, sub)
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue
		}
		if err := os.Rename(src, dst); err != nil {
			return "", fmt.Errorf("migration: move %s -> %s: %w", src, dst, err)
		}
	}

	if err := updateCurrentSymlink(flatkvDir, snapName); err != nil {
		return "", fmt.Errorf("migration: update current symlink: %w", err)
	}

	s.log.Info("FlatKV: migration complete", "snapshot", snapName)
	return snapDir, nil
}

// WriteSnapshot creates a PebbleDB checkpoint of the committed state.
// The snapshot is written into a versioned subdirectory under the flatkv root
// (e.g. flatkv/snapshot-00000000000000000100) and the current symlink is updated.
// The dir parameter is ignored; snapshots are always stored alongside the live data.
func (s *CommitStore) WriteSnapshot(_ string) error {
	version := s.committedVersion
	if version <= 0 {
		return fmt.Errorf("cannot snapshot uncommitted store (version %d)", version)
	}

	flatkvDir := filepath.Join(s.homeDir, "flatkv")
	snapDir := snapshotName(version)
	finalPath := filepath.Join(flatkvDir, snapDir)
	tmpPath := finalPath + "-tmp"

	// Clean up any stale tmp dir from a prior failed attempt
	_ = os.RemoveAll(tmpPath)

	if err := os.MkdirAll(tmpPath, 0750); err != nil {
		return fmt.Errorf("create snapshot tmp dir: %w", err)
	}

	// On any failure, remove the tmp directory
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(tmpPath)
		}
	}()

	// Checkpoint each snapshotted DB into its subdirectory.
	// Order is deterministic (slice, not map) for easier debugging.
	type namedDB struct {
		name string
		db   db_engine.DB
	}
	dbs := []namedDB{
		{accountDBDir, s.accountDB},
		{codeDBDir, s.codeDB},
		{storageDBDir, s.storageDB},
		{metadataDir, s.metadataDB},
	}
	for _, ndb := range dbs {
		cp, ok := ndb.db.(db_engine.Checkpointable)
		if !ok {
			return fmt.Errorf("db %s does not support Checkpoint", ndb.name)
		}
		dest := filepath.Join(tmpPath, ndb.name)
		if err := cp.Checkpoint(dest); err != nil {
			return fmt.Errorf("checkpoint %s: %w", ndb.name, err)
		}
	}

	// Atomic rename tmp -> final (idempotent: remove stale final if it exists)
	_ = atomicRemoveDir(finalPath)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("rename snapshot dir: %w", err)
	}

	if err := updateCurrentSymlink(flatkvDir, snapDir); err != nil {
		return fmt.Errorf("update current symlink: %w", err)
	}

	// TODO(PR2): prune old snapshots based on a configurable KeepSnapshots count.

	success = true
	s.log.Info("FlatKV snapshot created", "version", version, "dir", finalPath)
	return nil
}

// Rollback restores state to targetVersion by rewinding to the highest
// snapshot <= targetVersion, replaying WAL to reach the target, and
// truncating all WAL entries and snapshots beyond that point.
func (s *CommitStore) Rollback(targetVersion int64) error {
	s.log.Info("FlatKV Rollback", "targetVersion", targetVersion)

	flatkvDir := filepath.Join(s.homeDir, "flatkv")

	// Close all open handles so we can reopen from a different snapshot.
	if err := s.Close(); err != nil {
		return fmt.Errorf("close before rollback: %w", err)
	}

	// Find the best snapshot baseline (<= target).
	baseVersion, err := seekSnapshot(flatkvDir, targetVersion)
	if err != nil {
		return fmt.Errorf("seek snapshot for rollback: %w", err)
	}

	// Point current at the baseline snapshot.
	if err := updateCurrentSymlink(flatkvDir, snapshotName(baseVersion)); err != nil {
		return fmt.Errorf("update current symlink for rollback: %w", err)
	}

	// Reopen from the baseline snapshot and catch up to targetVersion only.
	if err := s.openTo(targetVersion); err != nil {
		return fmt.Errorf("reopen after rollback: %w", err)
	}

	if s.committedVersion != targetVersion {
		return fmt.Errorf("rollback failed: wanted version %d but reached %d (WAL may be incomplete)",
			targetVersion, s.committedVersion)
	}

	// Truncate WAL entries beyond targetVersion.
	if s.changelog != nil {
		off, err := s.walOffsetForVersion(targetVersion)
		if err != nil {
			return fmt.Errorf("compute WAL offset for version %d: %w", targetVersion, err)
		}
		if off > 0 {
			if err := s.changelog.TruncateAfter(off); err != nil {
				return fmt.Errorf("truncate WAL after version %d (offset %d): %w", targetVersion, off, err)
			}
		}
	}

	// Prune snapshot directories with version > targetVersion.
	_ = traverseSnapshots(flatkvDir, true, func(v int64) (bool, error) {
		if v > targetVersion {
			if err := atomicRemoveDir(filepath.Join(flatkvDir, snapshotName(v))); err != nil {
				s.log.Error("failed to remove snapshot", "version", v, "err", err)
			}
		}
		return false, nil
	})

	s.log.Info("FlatKV Rollback complete", "version", s.committedVersion)
	return nil
}

// tryTruncateWAL is a best-effort truncation of WAL entries that are older
// than the earliest snapshot. This prevents unbounded WAL growth while
// keeping enough entries for rollback to any retained snapshot.
func (s *CommitStore) tryTruncateWAL() {
	if s.changelog == nil {
		return
	}

	flatkvDir := filepath.Join(s.homeDir, "flatkv")

	// Find the earliest (lowest-version) snapshot — we must keep WAL entries
	// from that point onward so rollback to it is possible.
	var earliestSnapVersion int64
	_ = traverseSnapshots(flatkvDir, true, func(v int64) (bool, error) {
		earliestSnapVersion = v
		return true, nil
	})
	if earliestSnapVersion <= 0 {
		return
	}

	off, err := s.walOffsetForVersion(earliestSnapVersion)
	if err != nil || off == 0 {
		return
	}

	firstOff, err := s.changelog.FirstOffset()
	if err != nil || off <= firstOff {
		return
	}

	if err := s.changelog.TruncateBefore(off); err != nil {
		s.log.Error("failed to truncate WAL", "err", err, "truncateOffset", off)
	}
}

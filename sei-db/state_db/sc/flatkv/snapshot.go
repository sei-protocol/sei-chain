package flatkv

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	db_engine "github.com/sei-protocol/sei-chain/sei-db/db_engine"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// On-disk layout under <home>/flatkv/:
//
//	flatkv/
//	  current -> snapshot-NNNNN              (symlink to active snapshot)
//	  snapshot-NNNNN/                        (immutable checkpoint)
//	    account/                             (PebbleDB: addr → AccountValue)
//	    code/                                (PebbleDB: addr → bytecode)
//	    storage/                             (PebbleDB: addr||slot → value)
//	    legacy/                              (PebbleDB: full key → value)
//	    metadata/                            (PebbleDB: version + LtHash)
//	  working/                               (mutable clone of active snapshot)
//	    account/, code/, storage/, legacy/, metadata/
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

// snapshotDBDirs lists the DB subdirectory names included in a snapshot.
var snapshotDBDirs = []string{accountDBDir, codeDBDir, storageDBDir, legacyDBDir, metadataDir}

// removeTmpDirs removes any directories ending in "-tmp" or "-removing"
// left over from interrupted snapshot writes or deletes.
func removeTmpDirs(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() && (strings.HasSuffix(name, tmpSuffix) || strings.HasSuffix(name, removingSuffix)) {
			_ = os.RemoveAll(filepath.Join(dir, name))
		}
	}
	return nil
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

	for _, sub := range snapshotDBDirs {
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
// It handles four cases: (1) current symlink exists, (2) migration from
// pre-snapshot flat layout, (3) recovery from a partial migration crash,
// or (4) initialization of a fresh empty snapshot.
func (s *CommitStore) resolveSnapshotDir(flatkvDir string) (string, error) {
	snapDir, _, err := currentSnapshotDir(flatkvDir)
	if err == nil {
		return snapDir, nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("read current symlink: %w", err)
	}

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
			version = int64(binary.BigEndian.Uint64(verData)) //nolint:gosec // block height, always < MaxInt64
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

	dir := s.flatkvDir()
	snapDir := snapshotName(version)
	finalPath := filepath.Join(dir, snapDir)
	tmpPath := finalPath + tmpSuffix

	_ = os.RemoveAll(tmpPath)

	if err := os.MkdirAll(tmpPath, 0750); err != nil {
		return fmt.Errorf("create snapshot tmp dir: %w", err)
	}

	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(tmpPath)
		}
	}()

	// Deterministic order (slice, not map) for reproducibility.
	type namedDB struct {
		name string
		db   db_engine.DB
	}
	dbs := []namedDB{
		{accountDBDir, s.accountDB},
		{codeDBDir, s.codeDB},
		{storageDBDir, s.storageDB},
		{legacyDBDir, s.legacyDB},
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

	_ = atomicRemoveDir(finalPath) // idempotent: stale final may exist
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("rename snapshot dir: %w", err)
	}

	if err := updateCurrentSymlink(dir, snapDir); err != nil {
		return fmt.Errorf("update current symlink: %w", err)
	}

	s.pruneSnapshots(dir, version)

	success = true
	s.lastSnapshotTime = time.Now()
	s.log.Info("FlatKV snapshot created", "version", version, "dir", finalPath)
	return nil
}

// pruneSnapshots removes old snapshots beyond SnapshotKeepRecent, keeping
// the latest snapshot (currentVersion) plus the N most recent older ones.
// Best-effort: errors are logged but do not fail the snapshot operation.
func (s *CommitStore) pruneSnapshots(dir string, currentVersion int64) {
	keep := int(s.config.SnapshotKeepRecent)

	var older []int64
	_ = traverseSnapshots(dir, false, func(v int64) (bool, error) {
		if v != currentVersion {
			older = append(older, v)
		}
		return false, nil
	})

	if len(older) <= keep {
		return
	}

	for _, v := range older[keep:] {
		snapPath := filepath.Join(dir, snapshotName(v))
		if err := atomicRemoveDir(snapPath); err != nil {
			s.log.Error("prune snapshot failed", "version", v, "err", err)
		} else {
			s.log.Info("pruned old snapshot", "version", v)
		}
	}
}

// Rollback restores state to targetVersion by rewinding to the highest
// snapshot <= targetVersion, replaying WAL to reach the target, and
// truncating all WAL entries and snapshots beyond that point.
//
// Crash safety: the WAL is truncated BEFORE catchup writes any data to
// PebbleDB. If the process crashes after truncation but before catchup
// completes, the next restart will simply re-run catchup against the
// already-truncated WAL, converging to targetVersion.
func (s *CommitStore) Rollback(targetVersion int64) error {
	s.log.Info("FlatKV Rollback", "targetVersion", targetVersion)

	dir := s.flatkvDir()

	if err := s.closeDBsOnly(); err != nil {
		return fmt.Errorf("close before rollback: %w", err)
	}

	baseVersion, err := seekSnapshot(dir, targetVersion)
	if err != nil {
		return fmt.Errorf("seek snapshot for rollback: %w", err)
	}

	if err := updateCurrentSymlink(dir, snapshotName(baseVersion)); err != nil {
		return fmt.Errorf("update current symlink for rollback: %w", err)
	}

	if err := s.open(); err != nil {
		return fmt.Errorf("open for rollback: %w", err)
	}

	// Truncate WAL beyond targetVersion BEFORE catchup (crash safety).
	if s.changelog != nil {
		off, err := s.walOffsetForVersion(targetVersion)
		if err != nil {
			return fmt.Errorf("compute WAL offset for version %d: %w", targetVersion, err)
		}
		if off > 0 {
			if err := s.changelog.TruncateAfter(off); err != nil {
				return fmt.Errorf("truncate WAL after version %d (offset %d): %w", targetVersion, off, err)
			}
			if err := s.verifyWALTail(targetVersion); err != nil {
				return err
			}
		} else {
			// Target predates all WAL entries; clear the entire WAL to
			// prevent re-application. tidwall/wal cannot truncate to empty,
			// so we close, delete, and reopen.
			lastOff, lErr := s.changelog.LastOffset()
			if lErr == nil && lastOff > 0 {
				if err := s.clearChangelog(); err != nil {
					return fmt.Errorf("clear WAL (target %d predates first entry): %w", targetVersion, err)
				}
			}
		}
	}

	if err := s.catchup(targetVersion); err != nil {
		return fmt.Errorf("catchup after rollback: %w", err)
	}

	if s.committedVersion != targetVersion {
		return fmt.Errorf("rollback failed: wanted version %d but reached %d (WAL may be incomplete)",
			targetVersion, s.committedVersion)
	}

	_ = traverseSnapshots(dir, true, func(v int64) (bool, error) {
		if v > targetVersion {
			if err := atomicRemoveDir(filepath.Join(dir, snapshotName(v))); err != nil {
				s.log.Error("failed to remove snapshot", "version", v, "err", err)
			}
		}
		return false, nil
	})

	s.log.Info("FlatKV Rollback complete", "version", s.committedVersion)
	return nil
}

// verifyWALTail checks that the last WAL entry has the expected version.
func (s *CommitStore) verifyWALTail(expectedVersion int64) error {
	lastOff, err := s.changelog.LastOffset()
	if err != nil {
		return fmt.Errorf("verify WAL last offset: %w", err)
	}
	var lastVer int64
	if err := s.changelog.Replay(lastOff, lastOff, func(_ uint64, entry proto.ChangelogEntry) error {
		lastVer = entry.Version
		return nil
	}); err != nil {
		return fmt.Errorf("verify WAL last entry: %w", err)
	}
	if lastVer != expectedVersion {
		return fmt.Errorf("WAL integrity check failed: last entry is version %d, expected %d", lastVer, expectedVersion)
	}
	return nil
}

// tryTruncateWAL is a best-effort truncation of WAL entries that are older
// than the earliest snapshot. This prevents unbounded WAL growth while
// keeping enough entries for rollback to any retained snapshot.
func (s *CommitStore) tryTruncateWAL() {
	if s.changelog == nil {
		return
	}

	dir := s.flatkvDir()

	// Find the earliest (lowest-version) snapshot — we must keep WAL entries
	// from that point onward so rollback to it is possible.
	var earliestSnapVersion int64
	_ = traverseSnapshots(dir, true, func(v int64) (bool, error) {
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

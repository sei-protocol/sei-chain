package composite

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	sssnapshot "github.com/sei-protocol/sei-chain/sei-db/state_db/ss/snapshot"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
)

const rollbackTargetFile = ".ss-rollback-target"

type ssRollbackPlan struct {
	root        string
	changelog   string
	baseVersion int64
	snapshotDir string
}

func (s *CompositeStateStore) ValidateRollback(target int64) error {
	_, err := s.planRollback(target)
	return err
}

func (s *CompositeStateStore) Rollback(target int64) error {
	plan, err := s.planRollback(target)
	if err != nil {
		return err
	}

	if err := s.Close(); err != nil {
		return fmt.Errorf("close state store before rollback: %w", err)
	}
	if err := runRollbackSteps(plan, s.dbHome, target); err != nil {
		return fmt.Errorf("roll back state store to version %d from snapshot %d: %w", target, plan.baseVersion, err)
	}

	reopened, err := NewCompositeStateStore(s.config, s.homeDir)
	if err != nil {
		return fmt.Errorf("reopen state store after rollback: %w", err)
	}
	// The reopen is the same path a crashed rollback takes: it advances the
	// watermark to the target through the marker, including the case where the
	// target height wrote no changelog entry of its own, and clears the marker.
	if got := reopened.GetLatestVersion(); got != target {
		_ = reopened.Close()
		return fmt.Errorf("state store rollback failed: wanted version %d but reached %d", target, got)
	}
	s.adopt(reopened)
	logger.Info("state store rollback complete", "target", target, "snapshot", plan.baseVersion)
	return nil
}

func (s *CompositeStateStore) planRollback(target int64) (ssRollbackPlan, error) {
	if target <= 0 {
		return ssRollbackPlan{}, fmt.Errorf("invalid state store rollback target: %d", target)
	}
	if s.evmStore != nil || s.config.EVMSplit {
		return ssRollbackPlan{}, fmt.Errorf("state store rollback does not support evm-ss-split yet; rebuild SS from state sync or set evm-ss-split=false")
	}
	if s.config.Backend != config.PebbleDBBackend {
		return ssRollbackPlan{}, fmt.Errorf("state store rollback requires pebbledb backend, got %q", s.config.Backend)
	}
	if s.homeDir == "" || s.dbHome == "" {
		return ssRollbackPlan{}, fmt.Errorf("state store rollback missing home or database directory")
	}
	if latest := s.GetLatestVersion(); target > latest {
		return ssRollbackPlan{}, fmt.Errorf("cannot roll back state store to version %d: the store is at version %d", target, latest)
	}

	// Planning reads the changelog through the live store's own handle. The
	// store is still open here — ValidateRollback never closes it — and a second
	// handle over the same directory would repair a corrupt tail instead of
	// reporting it, and would read segments the pruner can truncate underneath.
	changelog, ok := s.cosmosStore.(types.SnapshotWALReader)
	if !ok {
		return ssRollbackPlan{}, fmt.Errorf("state store %T cannot report what its changelog covers", s.cosmosStore)
	}
	root := cosmosSnapshotRoot(s.homeDir, s.dbHome, s.config)
	baseVersion, err := rollbackBaseVersion(root, changelog, target)
	if err != nil {
		return ssRollbackPlan{}, err
	}
	return ssRollbackPlan{
		root:        root,
		changelog:   utils.GetChangelogPath(s.dbHome),
		baseVersion: baseVersion,
		snapshotDir: filepath.Join(root, SnapshotDirName(baseVersion)),
	}, nil
}

func cosmosSnapshotRoot(homeDir, dbHome string, cfg config.StateStoreConfig) string {
	if cfg.DBDirectory != "" {
		return utils.GetStateStoreSnapshotsSiblingPath(dbHome)
	}
	return utils.GetStateStoreSnapshotsPath(homeDir)
}

func rollbackBaseVersion(root string, changelog types.SnapshotWALReader, target int64) (int64, error) {
	versions, err := sssnapshot.ListSnapshotVersions(root)
	if err != nil {
		return 0, fmt.Errorf("list state store snapshots for rollback: %w", err)
	}
	var base int64
	for _, version := range versions {
		if version <= target {
			base = version
		}
	}
	if base == 0 {
		return 0, fmt.Errorf("cannot roll back state store to version %d: no snapshot at or below target", target)
	}
	if base == target {
		// The snapshot is the target height, so nothing has to be replayed onto
		// it. The changelog is not consulted: every entry it still holds is
		// above the target and is dropped by the rollback either way.
		return base, nil
	}

	oldest, next, err := changelog.WALVersionsAfter(base)
	if err != nil {
		return 0, fmt.Errorf("read changelog coverage above snapshot %d: %w", base, err)
	}
	if oldest == 0 {
		return 0, fmt.Errorf("cannot roll back state store to version %d: nearest snapshot is %d and the changelog is empty", target, base)
	}
	if next == 0 {
		return 0, fmt.Errorf("cannot roll back state store to version %d: nearest snapshot is %d and the changelog has no newer entries", target, base)
	}
	// A first entry above base+1 means the versions in between either wrote
	// nothing, which a block with no changesets does, or were pruned away.
	// Retention prunes a prefix only, so a retained entry below the first one to
	// replay proves the gap was never pruned and is empty blocks. A changelog
	// that starts at the gap proves nothing and is refused.
	if next == oldest && next != base+1 {
		return 0, fmt.Errorf("cannot roll back state store to version %d: the changelog starts at version %d, above snapshot %d, so the versions in between may have been pruned", target, next, base)
	}
	return base, nil
}

type rollbackStep struct {
	name string
	run  func() error
}

// rollbackSteps lists, in order, every change a rollback makes on disk. One list
// is the whole story: a step kept outside it is a step no crash test reaches and
// no recovery path redoes.
//
// The order is what makes a crash survivable. Two rules set it.
//
// The changelog holds every version the snapshot does not and is the only copy,
// so it stays inside the live directory until that directory has been moved
// aside whole, and it is moved into the published directory before the
// moved-aside one is deleted. Between any two steps the changelog is inside
// exactly one of dbHome and the backup directory, and recoverRollbackDirSwap
// empties both into dbHome before it deletes either.
//
// The snapshots above the target go first, before anything is published. They
// describe the fork this rollback discards, and only the changelog cut and the
// version watermark are redone from the marker on the next open — so a snapshot
// above the target that outlives the swap outlives the rollback, and a later
// rollback would take it as its base and restore the discarded fork's writes.
// Losing them to an abandoned rollback only narrows a future rollback window.
func rollbackSteps(plan ssRollbackPlan, dbHome string, target int64) []rollbackStep {
	tmpDir := rollbackTmpDir(dbHome)
	backupDir := rollbackBackupDir(dbHome)
	return []rollbackStep{
		{"remove stale rollback dirs", func() error {
			if err := os.RemoveAll(tmpDir); err != nil {
				return err
			}
			return os.RemoveAll(backupDir)
		}},
		{"discard snapshots above target", func() error {
			return removeSnapshotsAbove(plan.root, target)
		}},
		{"clone snapshot", func() error {
			return utils.ClonePebbleDir(plan.snapshotDir, tmpDir)
		}},
		{"mark rollback target", func() error {
			return writeRollbackTarget(tmpDir, target)
		}},
		{"move live database aside", func() error {
			if err := os.Rename(dbHome, backupDir); err != nil && !os.IsNotExist(err) {
				return err
			}
			return nil
		}},
		{"publish restored database", func() error {
			return os.Rename(tmpDir, dbHome)
		}},
		{"move changelog into restored database", func() error {
			return moveChangelog(backupDir, dbHome)
		}},
		{"persist directory swap", func() error {
			return utils.SyncDir(filepath.Dir(dbHome))
		}},
		{"remove old database backup", func() error {
			return os.RemoveAll(backupDir)
		}},
		{"cut changelog to target", func() error {
			return truncateChangelogAfterVersion(plan.changelog, target)
		}},
	}
}

// rollbackStepNames returns the step names in order, for a caller that needs the
// sequence without a database to run it against.
func rollbackStepNames() []string {
	steps := rollbackSteps(ssRollbackPlan{}, "", 0)
	names := make([]string, len(steps))
	for i, step := range steps {
		names[i] = step.name
	}
	return names
}

func runRollbackSteps(plan ssRollbackPlan, dbHome string, target int64) error {
	for _, step := range rollbackSteps(plan, dbHome, target) {
		if err := step.run(); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}
	return nil
}

// stateStoreHome resolves the live database directory, including the window a
// rollback opens in it. GetStateStorePath answers by asking whether the legacy
// directory exists, and the swap renames that directory away between two of its
// steps; a node that crashes there would otherwise come back on the new layout,
// open an empty database, and orphan the real one — changelog included — beside
// the path it no longer looks at.
func stateStoreHome(homeDir, backend string) string {
	resolved := utils.GetStateStorePath(homeDir, backend)
	legacy := utils.LegacyStateStorePath(homeDir, backend)
	if resolved == legacy {
		return resolved
	}
	if dirExists(rollbackTmpDir(legacy)) || dirExists(rollbackBackupDir(legacy)) {
		return legacy
	}
	return resolved
}

// recoverRollbackDirSwap finishes or abandons the directory swap of a rollback
// that died partway. It runs before the state store opens.
//
// It takes the changelog out of a directory before it deletes that directory:
// without the changelog the versions above the retained snapshots are gone for
// good, and no later rollback can replay forward.
func recoverRollbackDirSwap(dbHome string) error {
	tmpDir := rollbackTmpDir(dbHome)
	backupDir := rollbackBackupDir(dbHome)

	_, err := os.Stat(dbHome)
	switch {
	case err == nil:
	case os.IsNotExist(err):
		// The crash landed between moving the live database aside and
		// publishing the restored one. Publish the restored database when it is
		// staged, otherwise put the live one back and leave the rollback undone.
		switch {
		case dirExists(tmpDir):
			if err := os.Rename(tmpDir, dbHome); err != nil {
				return fmt.Errorf("publish restored rollback tmp dir: %w", err)
			}
		case dirExists(backupDir):
			if err := os.Rename(backupDir, dbHome); err != nil {
				return fmt.Errorf("restore live database from rollback backup: %w", err)
			}
		default:
			return nil
		}
	default:
		return err
	}

	for _, dir := range []string{backupDir, tmpDir} {
		if err := moveChangelog(dir, dbHome); err != nil {
			return err
		}
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("remove rollback dir %q: %w", dir, err)
		}
	}
	return nil
}

// moveChangelog moves srcDir's changelog into dstDir when dstDir has none. It
// makes the new destination entry durable before returning, because the caller
// deletes srcDir next and that directory holds the only other copy.
func moveChangelog(srcDir, dstDir string) error {
	src := utils.GetChangelogPath(srcDir)
	if !dirExists(src) {
		return nil
	}
	dst := utils.GetChangelogPath(dstDir)
	if dirExists(dst) {
		// Only one of the two directories ever holds a changelog, so this is a
		// leftover of an older attempt and the live one is already in place.
		return nil
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("move changelog %q to %q: %w", src, dst, err)
	}
	return utils.SyncDir(dstDir)
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func completePendingRollback(changelogPath, dbHome string) (target int64, ok bool, err error) {
	target, ok, err = readRollbackTarget(dbHome)
	if err != nil || !ok {
		return target, ok, err
	}
	if err := truncateChangelogAfterVersion(changelogPath, target); err != nil {
		return 0, false, err
	}
	return target, true, nil
}

func rollbackTmpDir(dbHome string) string {
	return dbHome + "-rollback-tmp"
}

func rollbackBackupDir(dbHome string) string {
	return dbHome + "-rollback-old"
}

func rollbackTargetPath(dbHome string) string {
	return filepath.Join(dbHome, rollbackTargetFile)
}

// writeRollbackTarget persists the marker before the directory swap publishes
// the snapshot it belongs to. A marker that is lost to a crash after that swap
// is worse than no rollback at all: the next open finds a restored database, no
// pending target, and replays the whole uncut changelog forward, so the store
// silently returns to the height the rollback was undoing.
func writeRollbackTarget(dbHome string, target int64) error {
	path := rollbackTargetPath(dbHome)
	tmp := path + ".tmp"
	if err := writeFileSynced(tmp, []byte(strconv.FormatInt(target, 10)+"\n")); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("publish rollback target marker: %w", err)
	}
	return utils.SyncDir(dbHome)
}

func writeFileSynced(path string, contents []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) //nolint:gosec // internal marker path
	if err != nil {
		return fmt.Errorf("create %q: %w", path, err)
	}
	if _, err := f.Write(contents); err != nil {
		_ = f.Close()
		return fmt.Errorf("write %q: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync %q: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %q: %w", path, err)
	}
	return nil
}

func readRollbackTarget(dbHome string) (int64, bool, error) {
	bz, err := os.ReadFile(rollbackTargetPath(dbHome)) //nolint:gosec // internal marker path
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, false, err
	}
	target, err := strconv.ParseInt(strings.TrimSpace(string(bz)), 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("parse rollback target marker: %w", err)
	}
	return target, true, nil
}

// clearRollbackTarget persists the removal too. A marker that comes back after a
// crash sends the next open through the cut again, against a changelog that has
// grown blocks since the rollback finished, and those blocks would be discarded.
func clearRollbackTarget(dbHome string) error {
	if err := os.Remove(rollbackTargetPath(dbHome)); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return utils.SyncDir(dbHome)
}

// truncateChangelogAfterVersion drops every changelog entry above target. It is
// safe to run again on a changelog it has already cut, which is how a rollback
// that died after this step finishes on the next open.
//
// A changelog whose retained entries are all above the target is deleted rather
// than emptied in place. The WAL only empties a log that was opened with
// AllowEmpty, and a log emptied that way is then refused by every open that
// does not set it, which is every opener of this changelog. The entries are the
// ones the rollback discards regardless, so a log recreated empty on the next
// open holds the same history.
func truncateChangelogAfterVersion(changelogPath string, target int64) error {
	reset, err := cutChangelogAfterVersion(changelogPath, target)
	if err != nil {
		return err
	}
	if !reset {
		// TruncateAfter rewrites a segment and replaces it through a rename.
		// The WAL handle fsyncs the rewritten file; this sync makes its directory
		// entry durable before the marker that would redo the cut is cleared.
		return utils.SyncDir(changelogPath)
	}
	if err := os.RemoveAll(changelogPath); err != nil {
		return fmt.Errorf("reset changelog %q: %w", changelogPath, err)
	}
	return utils.SyncDir(filepath.Dir(changelogPath))
}

// cutChangelogAfterVersion truncates the changelog in place and reports whether
// it has to be reset instead, which is the case when it holds no entry at or
// below target.
func cutChangelogAfterVersion(changelogPath string, target int64) (reset bool, err error) {
	stream, err := wal.NewChangelogWAL(changelogPath, wal.Config{FsyncEnabled: true})
	if err != nil {
		return false, fmt.Errorf("open state store changelog: %w", err)
	}
	defer func() {
		if closeErr := stream.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close state store changelog: %w", closeErr)
		}
	}()
	firstOffset, err := stream.FirstOffset()
	if err != nil {
		return false, err
	}
	if firstOffset == 0 {
		return false, nil
	}
	lastOffset, err := stream.LastOffset()
	if err != nil {
		return false, err
	}
	keepOffset, ok, err := wal.FindLastOffsetAtOrBeforeVersion(stream, firstOffset, lastOffset, target)
	if err != nil {
		return false, err
	}
	if !ok {
		return true, nil
	}
	return false, stream.TruncateAfter(keepOffset)
}

func removeSnapshotsAbove(root string, target int64) error {
	versions, err := sssnapshot.ListSnapshotVersions(root)
	if err != nil {
		return err
	}
	var errs []error
	for _, version := range versions {
		if version <= target {
			continue
		}
		dir := filepath.Join(root, SnapshotDirName(version))
		if err := os.RemoveAll(dir); err != nil {
			errs = append(errs, fmt.Errorf("remove snapshot %q: %w", dir, err))
		}
	}
	if err := utils.SyncDir(root); err != nil {
		errs = append(errs, fmt.Errorf("persist state store snapshot removals: %w", err))
	}
	return errors.Join(errs...)
}

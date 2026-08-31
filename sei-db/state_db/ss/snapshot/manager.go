package snapshot

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/seilog"
)

const (
	// SnapshotsDirName is the directory under data/state_store that holds
	// online snapshots for the Cosmos SS member.
	SnapshotsDirName = utils.StateStoreSnapshotsDirName

	snapshotPrefix = "snapshot-"
	// snapshotDirLen is "snapshot-" + 20-digit zero-padded version.
	snapshotDirLen = len(snapshotPrefix) + 20

	snapshotCurrentLink    = "current"
	snapshotCurrentTmpLink = "current-tmp"
	snapshotTmpPrefix      = "tmp-"
	snapshotSizeFile       = ".apparent-size"
	linkProbeName          = ".ss-snapshot-link-probe"
)

// Config wires one SS member into a snapshot Manager.
type Config struct {
	Name            string
	Root            string
	SourceDirs      []string
	Backend         string
	KeepRecent      int
	ExternalPruning bool
	Checkpointer    Checkpointer
	// Floor names a height this member's retention must keep. Leave it nil when the member is the only
	// one that has to hold the height a restore starts from.
	Floor *Floor
}

// Floor is a height retention must keep, shared by the members of a coordinated snapshot set. A
// restore reads the newest height every member holds, and a member counting its own directories cannot
// see the other roots: an unpaired newer directory would otherwise consume the keep slot that height
// occupies. The coordinator publishes the shared height here after every publication.
//
// Keeping it costs one directory beyond KeepRecent while the members disagree.
type Floor struct {
	height atomic.Int64
}

// NewFloor returns a Floor holding height, which may be 0 for "no height to keep".
func NewFloor(height int64) *Floor {
	f := &Floor{}
	f.Set(height)
	return f
}

func (f *Floor) Height() int64 {
	if f == nil {
		return 0
	}
	return f.height.Load()
}

func (f *Floor) Set(height int64) {
	if f == nil {
		return
	}
	f.height.Store(height)
}

// NewestCommonVersion returns the newest snapshot height present under every root, 0 when the roots
// share none. A root that cannot be read is treated as holding nothing, so the answer never names a
// height that may be absent.
func NewestCommonVersion(roots []string) int64 {
	if len(roots) == 0 {
		return 0
	}
	counts := map[int64]int{}
	for _, root := range roots {
		versions, err := ListSnapshotVersions(root)
		if err != nil {
			logger.Error("failed to list state store snapshots", "root", root, "error", err)
			return 0
		}
		for _, v := range versions {
			counts[v]++
		}
	}
	var newest int64
	for version, count := range counts {
		if count == len(roots) && version > newest {
			newest = version
		}
	}
	return newest
}

// Manager owns one SS member's snapshot root, staging directories, current
// symlink, metadata, and retention. It has no cadence of its own; a coordinator
// decides which height to snapshot and then calls Stage and Commit.
type Manager struct {
	name            string
	root            string
	backend         string
	keepRecent      int
	externalPruning bool
	checkpointer    Checkpointer
	floor           *Floor
	snapshotSizes   map[int64]int64

	publishMu     sync.Mutex
	lastPublished int64
}

type snapshotWALPruner interface {
	PruneWALBeforeVersion(version int64) error
}

// Staged names a checkpoint that has been written to a staging directory but
// has not yet been published.
type Staged struct {
	manager  *Manager
	version  int64
	tmpDir   string
	finalDir string
}

func (s *Staged) Abort() {
	if s == nil || s.manager == nil {
		return
	}
	s.manager.abort(s)
}

// SnapshotDirName returns the directory name for a snapshot labeled with the
// given version.
func SnapshotDirName(version int64) string {
	return fmt.Sprintf("%s%020d", snapshotPrefix, version)
}

// ParseSnapshotVersion parses a snapshot directory name; ok is false for
// anything that is not a snapshot-<20 digits> name.
func ParseSnapshotVersion(name string) (version int64, ok bool) {
	if !strings.HasPrefix(name, snapshotPrefix) || len(name) != snapshotDirLen {
		return 0, false
	}
	v, err := strconv.ParseInt(name[len(snapshotPrefix):], 10, 64)
	if err != nil || v < 0 {
		return 0, false
	}
	return v, true
}

// ListSnapshotVersions returns the labels of all snapshots under root in
// ascending order. A missing root is not an error (no snapshots yet).
func ListSnapshotVersions(root string) ([]int64, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read snapshots dir %q: %w", root, err)
	}
	var versions []int64
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if v, ok := ParseSnapshotVersion(entry.Name()); ok {
			versions = append(versions, v)
		}
	}
	slices.Sort(versions)
	return versions, nil
}

// Open prepares a snapshot root and returns a Manager for one SS member.
func Open(cfg Config) (*Manager, error) {
	if cfg.Checkpointer == nil {
		return nil, fmt.Errorf("%s snapshot checkpointer is nil", cfg.Name)
	}
	if !cfg.Checkpointer.SupportsCheckpoint() {
		return nil, fmt.Errorf("%s backend %q does not support checkpoints", cfg.Name, cfg.Backend)
	}
	if err := verifyHardlinks(cfg.Root, cfg.SourceDirs); err != nil {
		return nil, err
	}
	m := &Manager{
		name:            cfg.Name,
		root:            cfg.Root,
		backend:         cfg.Backend,
		keepRecent:      cfg.KeepRecent,
		externalPruning: cfg.ExternalPruning,
		checkpointer:    cfg.Checkpointer,
		floor:           cfg.Floor,
		snapshotSizes:   map[int64]int64{},
	}
	m.lastPublished = m.Newest()
	m.removeStaleTmpDirs()
	m.prune()
	if m.lastPublished > 0 {
		if err := m.updateCurrentLink(SnapshotDirName(m.lastPublished)); err != nil {
			logger.Error("failed to restore state store snapshot current link",
				"store", m.name, "version", m.lastPublished, "error", err)
		}
		recordCurrentHeight(m.name, m.lastPublished)
	}
	logger.Info("state store member snapshotting enabled",
		"store", m.name,
		"root", m.root,
		"keepRecent", m.keepRecent,
	)
	return m, nil
}

func (m *Manager) Name() string {
	if m == nil {
		return ""
	}
	return m.name
}

func (m *Manager) Root() string {
	if m == nil {
		return ""
	}
	return m.root
}

// Prepare reserves a staging directory for version without queueing any work. A caller that snapshots
// several members prepares all of them before it schedules any, so a member that cannot be prepared
// leaves no barrier queued for a version nothing will publish.
func (m *Manager) Prepare(version int64) (*Staged, error) {
	name := SnapshotDirName(version)
	finalDir := filepath.Join(m.root, name)
	if _, err := os.Stat(finalDir); err == nil {
		return nil, fmt.Errorf("%s snapshot dir %q already exists", m.name, finalDir)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect %s snapshot dir %q: %w", m.name, finalDir, err)
	}
	tmpDir := filepath.Join(m.root, snapshotTmpPrefix+name)
	// Refused rather than cleared: a checkpoint may still be writing into it, and deleting underneath
	// one leaves a partial directory that is then published under an exact label. Open clears the
	// staging directories a crash left behind, so a stale one does not block the next boundary.
	if _, err := os.Stat(tmpDir); err == nil {
		return nil, fmt.Errorf("%s snapshot staging dir %q already exists", m.name, tmpDir)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect %s snapshot staging dir %q: %w", m.name, tmpDir, err)
	}
	if err := os.MkdirAll(m.root, 0o750); err != nil {
		return nil, fmt.Errorf("create %s snapshot root: %w", m.name, err)
	}
	return &Staged{
		manager:  m,
		version:  version,
		tmpDir:   tmpDir,
		finalDir: finalDir,
	}, nil
}

// Schedule queues staged's checkpoint behind the writes already enqueued on this member's backend.
// done is called exactly once, so a caller counting members can wait on it.
func (m *Manager) Schedule(staged *Staged, shouldRun func() bool, done func(error)) {
	if staged == nil || staged.manager != m {
		done(fmt.Errorf("%s staged snapshot belongs to a different manager", m.name))
		return
	}
	m.checkpointer.ScheduleCheckpoint(staged.tmpDir, shouldRun, done)
}

func (m *Manager) Commit(staged *Staged) error {
	if staged == nil || staged.manager != m {
		return fmt.Errorf("%s staged snapshot belongs to a different manager", m.name)
	}
	apparentBytes, sizeErr := snapshotDirApparentBytes(staged.tmpDir)
	if sizeErr != nil {
		logger.Error("failed to measure state store snapshot",
			"store", m.name, "dir", staged.tmpDir, "error", sizeErr)
	} else if err := writeSnapshotSize(staged.tmpDir, apparentBytes); err != nil {
		logger.Error("failed to persist state store snapshot size",
			"store", m.name, "dir", staged.tmpDir, "error", err)
		sizeErr = err
	}

	m.publishMu.Lock()
	defer m.publishMu.Unlock()
	defer m.prune()

	if err := m.checkpointer.SetCheckpointVersion(staged.tmpDir, staged.version); err != nil {
		_ = os.RemoveAll(staged.tmpDir)
		return fmt.Errorf("set %s snapshot version: %w", m.name, err)
	}
	if err := os.Rename(staged.tmpDir, staged.finalDir); err != nil {
		_ = os.RemoveAll(staged.tmpDir)
		return fmt.Errorf("finalize %s snapshot: %w", m.name, err)
	}
	if err := syncDir(m.root); err != nil {
		return fmt.Errorf("persist %s snapshot publication: %w", m.name, err)
	}
	if sizeErr == nil {
		m.snapshotSizes[staged.version] = apparentBytes
	}
	logger.Info("state store member snapshot created",
		"store", m.name, "version", staged.version, "dir", staged.finalDir)

	// Snapshots can finish out of order, so only move the link forward.
	if staged.version > m.lastPublished {
		if err := m.updateCurrentLink(SnapshotDirName(staged.version)); err != nil {
			return fmt.Errorf("update %s snapshot current link: %w", m.name, err)
		}
		m.lastPublished = staged.version
	}
	recordCurrentHeight(m.name, m.lastPublished)
	return nil
}

func (m *Manager) abort(staged *Staged) {
	if staged == nil || staged.manager != m {
		return
	}
	if err := os.RemoveAll(staged.tmpDir); err != nil {
		logger.Error("failed to remove aborted state store snapshot",
			"store", m.name, "dir", staged.tmpDir, "error", err)
	}
}

func (m *Manager) Versions() ([]int64, error) {
	if m == nil {
		return nil, nil
	}
	return ListSnapshotVersions(m.root)
}

func (m *Manager) Newest() int64 {
	versions, err := m.Versions()
	if err != nil {
		logger.Error("failed to list state store snapshots", "store", m.name, "error", err)
		return 0
	}
	if len(versions) == 0 {
		return 0
	}
	return versions[len(versions)-1]
}

func (m *Manager) ModTime(version int64) time.Time {
	if version <= 0 {
		return time.Time{}
	}
	info, err := os.Stat(filepath.Join(m.root, SnapshotDirName(version)))
	if err != nil {
		logger.Error("failed to read state store snapshot modification time",
			"store", m.name, "version", version, "error", err)
		return time.Time{}
	}
	return info.ModTime()
}

// RollbackFloor returns the oldest snapshot this member must keep to serve a rollback of
// rollbackWindow blocks behind head, bounded by the snapshot a restore currently resolves through:
//
//	a snapshot at or below head - rollbackWindow → the newest such snapshot
//	every snapshot above that height             → the oldest snapshot, the deepest this member can
//	                                               restore to
//	no snapshot, or a window deeper than head    → 0, nothing here is eligible for pruning
//
// It also returns 0 when the snapshot root cannot be read. Answering high is the damaging direction:
// nothing above clamps this, and a caller derives its cut line from it.
func (m *Manager) RollbackFloor(head uint64, rollbackWindow uint64) uint64 {
	if m == nil || head <= rollbackWindow {
		return 0
	}
	versions, err := m.Versions()
	if err != nil {
		logger.Error("failed to list state store snapshots for the rollback floor; holding it at 0",
			"store", m.name, "rollbackWindow", rollbackWindow, "error", err)
		return 0
	}

	target := head - rollbackWindow
	var oldest, newestPastWindow uint64
	for _, version := range versions {
		if version <= 0 {
			continue // version 0 restores to no committed height
		}
		block := uint64(version)
		if oldest == 0 || block < oldest {
			oldest = block
		}
		if block <= target && block > newestPastWindow {
			newestPastWindow = block
		}
	}
	if oldest == 0 {
		return 0
	}

	floor := newestPastWindow
	if floor == 0 {
		floor = oldest
	}

	current, exists, err := m.currentSnapshotVersion()
	if err != nil {
		logger.Error("failed to resolve the current state store snapshot for the rollback floor; holding it at 0",
			"store", m.name, "rollbackWindow", rollbackWindow, "error", err)
		return 0
	}
	if !exists || current <= 0 {
		return 0
	}
	return min(floor, uint64(current))
}

// PruneSnapshots deletes every snapshot below cutLine, never the current one. It acts whether or not
// retention is external: an external collector prunes this store through here, and it is the internal
// count-based retention that stands down instead.
func (m *Manager) PruneSnapshots(cutLine int64) error {
	if m == nil {
		return nil
	}
	// Publication renames a directory in and swaps current under this lock, so retention has to hold it
	// too: otherwise a cut line resolved a moment earlier can delete what current now names.
	m.publishMu.Lock()
	defer m.publishMu.Unlock()
	defer m.recordRetentionMetrics()

	versions, err := m.Versions()
	if err != nil {
		return err
	}
	candidates := make([]int64, 0, len(versions))
	for _, v := range versions {
		if v < cutLine {
			candidates = append(candidates, v)
		}
	}
	if err := m.removeSnapshots(candidates); err != nil {
		return err
	}
	m.pruneWALToOldestSnapshot()
	return nil
}

// removeSnapshots deletes each candidate except the current snapshot and the shared floor. Every
// candidate is attempted, the removals being independent of each other.
func (m *Manager) removeSnapshots(candidates []int64) error {
	currentVersion, hasCurrent, err := m.currentSnapshotVersion()
	if err != nil {
		return err
	}
	floor := m.floor.Height()
	var errs []error
	for _, v := range candidates {
		if (hasCurrent && v == currentVersion) || v == floor {
			continue
		}
		dir := filepath.Join(m.root, SnapshotDirName(v))
		if err := os.RemoveAll(dir); err != nil {
			errs = append(errs, fmt.Errorf("remove %s snapshot %q: %w", m.name, dir, err))
			continue
		}
		logger.Info("pruned state store snapshot", "store", m.name, "dir", dir)
	}
	return errors.Join(errs...)
}

func (m *Manager) removeStaleTmpDirs() {
	tmpLink := filepath.Join(m.root, snapshotCurrentTmpLink)
	if err := os.Remove(tmpLink); err != nil && !os.IsNotExist(err) {
		logger.Error("failed to remove stale state store snapshot link",
			"store", m.name, "path", tmpLink, "error", err)
	}

	entries, err := os.ReadDir(m.root)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Error("failed to scan state store snapshots dir",
				"store", m.name, "error", err)
		}
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), snapshotTmpPrefix) {
			continue
		}
		dir := filepath.Join(m.root, entry.Name())
		if err := os.RemoveAll(dir); err != nil {
			logger.Error("failed to remove stale snapshot tmp dir",
				"store", m.name, "dir", dir, "error", err)
			continue
		}
		logger.Info("removed stale state store snapshot tmp dir", "store", m.name, "dir", dir)
	}
}

func (m *Manager) updateCurrentLink(name string) error {
	tmpLink := filepath.Join(m.root, snapshotCurrentTmpLink)
	_ = os.Remove(tmpLink)
	if err := os.Symlink(name, tmpLink); err != nil {
		return fmt.Errorf("create snapshot current symlink: %w", err)
	}
	if err := os.Rename(tmpLink, filepath.Join(m.root, snapshotCurrentLink)); err != nil {
		return fmt.Errorf("swap snapshot current symlink: %w", err)
	}
	return syncDir(m.root)
}

func (m *Manager) prune() {
	if m.externalPruning {
		return
	}
	versions, err := m.Versions()
	if err != nil {
		logger.Error("failed to list state store snapshots for pruning",
			"store", m.name, "error", err)
		return
	}
	defer m.recordRetentionMetrics()
	keep := 1 + m.keepRecent
	if len(versions) <= keep {
		return
	}
	if err := m.removeSnapshots(versions[:len(versions)-keep]); err != nil {
		logger.Error("failed to prune state store snapshots", "store", m.name, "error", err)
		return
	}
	m.pruneWALToOldestSnapshot()
}

func (m *Manager) pruneWALToOldestSnapshot() {
	pruner, ok := m.checkpointer.(snapshotWALPruner)
	if !ok {
		return
	}
	versions, err := m.Versions()
	if err != nil {
		logger.Error("failed to list state store snapshots for WAL pruning",
			"store", m.name, "error", err)
		return
	}
	if len(versions) == 0 {
		return
	}
	oldest := versions[0]
	if err := pruner.PruneWALBeforeVersion(oldest); err != nil {
		logger.Error("failed to prune state store changelog WAL",
			"store", m.name, "version", oldest, "error", err)
	}
}

func (m *Manager) currentSnapshotVersion() (version int64, exists bool, err error) {
	target, err := os.Readlink(filepath.Join(m.root, snapshotCurrentLink))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("read current snapshot link: %w", err)
	}
	version, ok := ParseSnapshotVersion(filepath.Base(target))
	if !ok {
		return 0, false, fmt.Errorf("current snapshot link has invalid target %q", target)
	}
	return version, true, nil
}

func (m *Manager) recordRetentionMetrics() {
	versions, err := m.Versions()
	if err != nil {
		logger.Error("failed to list state store snapshots for metrics",
			"store", m.name, "error", err)
		return
	}
	recordRetainedCount(m.name, int64(len(versions)))

	if m.snapshotSizes == nil {
		m.snapshotSizes = map[int64]int64{}
	}
	retained := make(map[int64]struct{}, len(versions))
	var apparentBytes int64
	for _, version := range versions {
		retained[version] = struct{}{}
		if size, ok := m.snapshotSizes[version]; ok {
			apparentBytes += size
			continue
		}
		dir := filepath.Join(m.root, SnapshotDirName(version))
		size, err := readSnapshotSize(dir)
		if err != nil {
			if !os.IsNotExist(err) {
				logger.Error("failed to read state store snapshot size",
					"store", m.name, "dir", dir, "error", err)
			}
			continue
		}
		m.snapshotSizes[version] = size
		apparentBytes += size
	}
	for version := range m.snapshotSizes {
		if _, ok := retained[version]; !ok {
			delete(m.snapshotSizes, version)
		}
	}
	recordApparentBytes(m.name, apparentBytes)
}

// verifyHardlinks proves the snapshot root can hold hardlinks to each source directory, which is how a
// checkpoint avoids copying the database.
//
// The probe has a fixed name in both places so a process that dies between the link and the removals
// leaves one known path per directory, which the next start reclaims rather than accumulating.
func verifyHardlinks(root string, sourceDirs []string) error {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return fmt.Errorf("create snapshot root %q: %w", root, err)
	}
	for _, sourceDir := range sourceDirs {
		source := filepath.Join(sourceDir, linkProbeName)
		target := filepath.Join(root, linkProbeName)
		if err := removeIfExists(source); err != nil {
			return fmt.Errorf("clear hardlink probe %q: %w", source, err)
		}
		if err := removeIfExists(target); err != nil {
			return fmt.Errorf("clear hardlink probe %q: %w", target, err)
		}
		if err := os.WriteFile(source, nil, 0o600); err != nil {
			return fmt.Errorf("create hardlink probe in state store %q: %w", sourceDir, err)
		}
		if err := os.Link(source, target); err != nil {
			_ = os.Remove(source)
			return fmt.Errorf(
				"state store %q cannot hardlink snapshots into %q; place the SS database and its snapshot root on one filesystem: %w",
				sourceDir,
				root,
				err,
			)
		}
		if err := os.Remove(source); err != nil {
			_ = os.Remove(target)
			return fmt.Errorf("remove hardlink probe %q: %w", source, err)
		}
		if err := os.Remove(target); err != nil {
			return fmt.Errorf("remove hardlink probe %q: %w", target, err)
		}
	}
	return nil
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func syncDir(path string) error {
	// #nosec G304 -- path is an internal database or snapshot directory, not request input.
	dir, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open directory %q for sync: %w", path, err)
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil {
		syncErr = fmt.Errorf("sync directory %q: %w", path, syncErr)
	}
	if closeErr != nil {
		closeErr = fmt.Errorf("close directory %q after sync: %w", path, closeErr)
	}
	return errors.Join(syncErr, closeErr)
}

func snapshotDirApparentBytes(dir string) (int64, error) {
	var apparentBytes int64
	err := filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		apparentBytes += info.Size()
		return nil
	})
	return apparentBytes, err
}

func writeSnapshotSize(dir string, size int64) error {
	path := filepath.Join(dir, snapshotSizeFile)
	// #nosec G304 -- dir is a managed snapshot directory and the file name is fixed.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := fmt.Fprintf(file, "%d\n", size)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	return syncDir(dir)
}

func readSnapshotSize(dir string) (int64, error) {
	// #nosec G304 -- dir is a managed snapshot directory and the file name is fixed.
	data, err := os.ReadFile(filepath.Join(dir, snapshotSizeFile))
	if err != nil {
		return 0, err
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse snapshot size in %q: %w", dir, err)
	}
	if size < 0 {
		return 0, fmt.Errorf("snapshot size in %q must be non-negative", dir)
	}
	return size, nil
}

var logger = seilog.NewLogger("db", "state-db", "ss", "snapshot")

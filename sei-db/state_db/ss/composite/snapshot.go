package composite

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// Online state-store snapshots. Every SnapshotInterval blocks the store takes a
// Pebble checkpoint of each backend while the node keeps producing blocks.
// Checkpoints are hardlink trees, so they do not copy database contents, but
// each one occupies its backend's apply goroutine for the full checkpoint
// operation. Writes continue to enter the bounded queue, but a full queue
// applies backpressure until the checkpoint finishes. The result is an
// immutable, crash-consistent image of the query store.
//
// On-disk layout under the snapshot root. By default the root is
// <home>/data/state_store/snapshots. A custom Cosmos SS directory <db> moves it
// to the sibling <db>-snapshots directory so Pebble can use hardlinks.
//
//	snapshots/
//	  current -> snapshot-NNNNN            (symlink to newest snapshot)
//	  snapshot-NNNNN/                      (immutable; NNNNN = label version)
//	    cosmos/<backend>/                  (Pebble checkpoint of Cosmos SS)
//	    evm/<backend>/                     (Pebble checkpoint of EVM SS, if split)
//	      <subname>/                       (when EVM sub-DBs are separate)
//
// Snapshots are eligible at the same interval boundaries and minimum time
// cadence as state commit. Each layer applies its in-flight gate independently,
// so a skipped boundary can differ. For every accepted SS snapshot, the label
// is exact: it is the version the write path had just handed to the backends
// when the snapshot was requested. Placing a barrier in each backend's apply
// queue — rather than sampling what the backends had applied — makes that label
// exact without the request having to wait. See requestSnapshot.
//
// The barrier orders only the async block-commit queues. Import, recovery,
// pruning, and direct version-marker writes bypass those queues and must not
// call ScheduleSnapshot. The rootmulti commit path owns the trigger, including
// the explicit trigger for an empty block.
//
// Managed snapshot directories have no lease. A live consumer must not rely on
// a path remaining present across a retention pass. Until a lease API exists,
// consumers must stop the node or use external coordination that prevents
// pruning before they open or copy a snapshot.
const (
	// SnapshotsDirName is the directory under data/state_store that holds
	// online snapshots.
	SnapshotsDirName = utils.StateStoreSnapshotsDirName

	snapshotPrefix = "snapshot-"
	// snapshotDirLen is "snapshot-" + 20-digit zero-padded version.
	snapshotDirLen = len(snapshotPrefix) + 20

	snapshotCurrentLink    = "current"
	snapshotCurrentTmpLink = "current-tmp"
	snapshotTmpPrefix      = "tmp-"
	snapshotSizeFile       = ".apparent-size"
)

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

// snapshotManager owns the snapshots directory and the one-at-a-time discipline
// for filling it. It has no goroutine of its own: snapshots are requested from
// the write path and completed on the backends' apply goroutines.
type snapshotManager struct {
	root       string
	backend    string
	interval   int64
	keepRecent int
	minTime    time.Duration

	cosmosScheduler types.CheckpointScheduler
	evmScheduler    types.CheckpointScheduler
	snapshotSizes   map[int64]int64

	mu sync.Mutex
	// lastRequested is the newest label already requested or on disk, so a
	// boundary is not snapshotted twice across a restart or a re-sent version.
	lastRequested int64
	lastRequestAt time.Time
	inFlight      bool
	stopped       bool
	// scheduling closes the gap between accepting a request and enqueueing its
	// barriers. Close waits for it before closing backend queues.
	scheduling sync.WaitGroup
	// publishing tracks the goroutine finishing the accepted snapshot off.
	publishing sync.WaitGroup

	// publishMu serializes the publish step, which reads and rewrites the
	// shared directory (the current link, and pruning).
	publishMu     sync.Mutex
	lastPublished int64
}

type checkpointTarget struct {
	store types.CheckpointScheduler
	dest  string
}

// startSnapshotManager wires the manager into the composite store. Snapshot
// enablement is fail-closed: every backend must support checkpoints, and every
// live DB must be able to hardlink into root. Pebble otherwise silently falls
// back to copying SSTs across filesystems while its apply worker is blocked.
func (s *CompositeStateStore) startSnapshotManager(root string, sourceDirs []string) error {
	if s.config.SnapshotInterval <= 0 {
		return nil
	}
	cosmosScheduler, ok := s.cosmosStore.(types.CheckpointScheduler)
	if !ok || !cosmosScheduler.SupportsCheckpoint() {
		return fmt.Errorf("cosmos backend %q does not support checkpoints", s.config.Backend)
	}
	var evmScheduler types.CheckpointScheduler
	if s.evmStore != nil {
		evmScheduler, ok = s.evmStore.(types.CheckpointScheduler)
		if !ok || !evmScheduler.SupportsCheckpoint() {
			return fmt.Errorf("EVM backend %q does not support checkpoints", s.config.Backend)
		}
	}
	if err := verifySnapshotHardlinks(root, sourceDirs); err != nil {
		return err
	}
	m := &snapshotManager{
		root:            root,
		backend:         s.config.Backend,
		interval:        s.config.SnapshotInterval,
		keepRecent:      s.config.SnapshotKeepRecent,
		minTime:         s.config.SnapshotMinTimeInterval,
		cosmosScheduler: cosmosScheduler,
		evmScheduler:    evmScheduler,
		snapshotSizes:   map[int64]int64{},
	}
	m.lastRequested = m.newestSnapshotVersion()
	m.lastPublished = m.lastRequested
	m.lastRequestAt = m.snapshotModTime(m.lastRequested)
	m.removeStaleTmpDirs()
	m.prune()
	if m.lastPublished > 0 {
		if err := m.updateCurrentLink(SnapshotDirName(m.lastPublished)); err != nil {
			logger.Error("failed to restore state store snapshot current link",
				"version", m.lastPublished, "error", err)
		}
		snapshotMetrics.CurrentHeight.Record(context.Background(), m.lastPublished)
	}
	s.snapshotMgr = m
	logger.Info("state store snapshotting enabled",
		"root", root,
		"interval", m.interval,
		"minTimeInterval", m.minTime,
		"keepRecent", m.keepRecent,
	)
	return nil
}

func verifySnapshotHardlinks(root string, sourceDirs []string) error {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return fmt.Errorf("create snapshot root %q: %w", root, err)
	}
	for _, sourceDir := range sourceDirs {
		probe, err := os.CreateTemp(sourceDir, ".ss-snapshot-link-probe-*")
		if err != nil {
			return fmt.Errorf("create hardlink probe in state store %q: %w", sourceDir, err)
		}
		source := probe.Name()
		if err := probe.Close(); err != nil {
			_ = os.Remove(source)
			return fmt.Errorf("close hardlink probe in state store %q: %w", sourceDir, err)
		}
		target := filepath.Join(root, filepath.Base(source))
		if err := os.Link(source, target); err != nil {
			_ = os.Remove(source)
			return fmt.Errorf(
				"state store %q cannot hardlink snapshots into %q; place all SS databases and the snapshot root on one filesystem: %w",
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

// stop prevents further snapshots, waits for accepted requests to enqueue their
// barriers, and then waits for active publication. Queued barriers are canceled
// before they start when backend close drains their queues.
func (m *snapshotManager) stop() {
	m.mu.Lock()
	m.stopped = true
	m.mu.Unlock()
	m.scheduling.Wait()
	m.publishing.Wait()
}

func (m *snapshotManager) isRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.stopped
}

// maybeSnapshot takes a snapshot when version lands on an interval boundary.
// It is called from the write path for every version, so the common case is the
// modulo test and nothing else.
func (m *snapshotManager) maybeSnapshot(version int64) {
	if m == nil || version <= 0 || m.interval <= 0 || version%m.interval != 0 {
		return
	}
	now := time.Now()
	m.mu.Lock()
	previous := m.lastRequested
	previousRequestAt := m.lastRequestAt
	var skipReason string
	accepted := false
	switch {
	case m.stopped || version <= m.lastRequested:
		// A repeated commit-path call is expected and is not a skipped attempt.
	case m.inFlight:
		skipReason = "in_flight"
	case !m.lastRequestAt.IsZero() && now.Sub(m.lastRequestAt) < m.minTime:
		skipReason = "minimum_time_interval"
	default:
		m.lastRequested = version
		m.lastRequestAt = now
		m.inFlight = true
		m.scheduling.Add(1)
		recordSnapshotInFlight(1)
		accepted = true
	}
	m.mu.Unlock()
	if !accepted {
		if skipReason != "" {
			recordSnapshotSkipped(skipReason)
		}
		return
	}
	defer m.scheduling.Done()
	start := time.Now()
	recordSnapshotAttempt()
	if err := m.requestSnapshot(version, start); err != nil {
		recordSnapshotCompletion(start, "failure")
		m.mu.Lock()
		if m.lastRequested == version {
			m.lastRequested = previous
			m.lastRequestAt = previousRequestAt
			m.inFlight = false
			recordSnapshotInFlight(0)
		}
		m.mu.Unlock()
		logger.Error("state store snapshot failed", "version", version, "error", err)
	}
}

func (m *snapshotManager) finishSnapshot() {
	m.mu.Lock()
	m.inFlight = false
	recordSnapshotInFlight(0)
	m.mu.Unlock()
}

// requestSnapshot asks every backend to checkpoint itself into a staging
// directory and publishes the result once they all have.
//
// The label is exact because of when this runs: the caller has just enqueued
// version on the backends and has not enqueued anything above it, so a barrier
// placed in each apply queue now captures that backend with everything up to
// version applied and nothing after it. The backends reach their barriers
// independently and at different wall-clock times, and the caller waits for
// none of it — enqueueing a barrier costs what enqueueing a changeset costs.
func (m *snapshotManager) requestSnapshot(version int64, start time.Time) error {
	name := SnapshotDirName(version)
	finalDir := filepath.Join(m.root, name)
	if _, err := os.Stat(finalDir); err == nil {
		return fmt.Errorf("snapshot dir %q already exists", finalDir)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect snapshot dir %q: %w", finalDir, err)
	}
	tmpDir := filepath.Join(m.root, snapshotTmpPrefix+name)
	if err := os.RemoveAll(tmpDir); err != nil {
		return fmt.Errorf("clear stale snapshot tmp dir: %w", err)
	}

	targets := []checkpointTarget{
		{m.cosmosScheduler, filepath.Join(tmpDir, "cosmos", m.backend)},
	}
	if m.evmScheduler != nil {
		targets = append(targets, checkpointTarget{
			m.evmScheduler,
			filepath.Join(tmpDir, "evm", m.backend),
		})
	}
	for _, target := range targets {
		if err := os.MkdirAll(filepath.Dir(target.dest), 0o750); err != nil {
			_ = os.RemoveAll(tmpDir)
			return fmt.Errorf("create snapshot dir: %w", err)
		}
	}

	var (
		mu        sync.Mutex
		remaining = len(targets)
		firstErr  error
	)
	// Set up before scheduling because callbacks can complete while the loop is
	// still scheduling the remaining targets.
	for _, target := range targets {
		target.store.ScheduleCheckpoint(target.dest, m.isRunning, func(err error) {
			mu.Lock()
			if err != nil && firstErr == nil {
				firstErr = err
			}
			remaining--
			last, outcome := remaining == 0, firstErr
			mu.Unlock()
			if !last {
				return
			}
			m.startPublish(version, tmpDir, finalDir, targets, outcome, start)
		})
	}
	return nil
}

// startPublish hands a finished set of checkpoints off to a goroutine. It runs
// on whichever backend's apply goroutine finished last, so it must not do the
// work itself: publishing renames directories and prunes old snapshots, and a
// writer stalled on that is a writer not applying blocks.
func (m *snapshotManager) startPublish(
	version int64,
	tmpDir, finalDir string,
	targets []checkpointTarget,
	checkpointErr error,
	start time.Time,
) {
	// Taken under the same lock stop uses, so no goroutine is registered after
	// stop has started waiting.
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		_ = os.RemoveAll(tmpDir)
		recordSnapshotCompletion(start, "canceled")
		m.finishSnapshot()
		return
	}
	m.publishing.Add(1)
	m.mu.Unlock()

	go func() {
		defer m.publishing.Done()
		defer m.finishSnapshot()
		if checkpointErr != nil {
			if errors.Is(checkpointErr, types.ErrCheckpointCanceled) {
				recordSnapshotCompletion(start, "canceled")
			} else {
				recordSnapshotCompletion(start, "failure")
				logger.Error("state store snapshot failed", "version", version, "error", checkpointErr)
			}
			_ = os.RemoveAll(tmpDir)
			return
		}
		for _, target := range targets {
			if err := target.store.SetCheckpointVersion(target.dest, version); err != nil {
				recordSnapshotCompletion(start, "failure")
				logger.Error("failed to set state store snapshot version",
					"version", version, "dir", target.dest, "error", err)
				_ = os.RemoveAll(tmpDir)
				return
			}
		}
		if m.publish(version, tmpDir, finalDir, start) {
			recordSnapshotCompletion(start, "success")
		} else {
			recordSnapshotCompletion(start, "failure")
		}
	}()
}

func (m *snapshotManager) publish(version int64, tmpDir, finalDir string, start time.Time) bool {
	apparentBytes, sizeErr := snapshotDirApparentBytes(tmpDir)
	if sizeErr != nil {
		logger.Error("failed to measure state store snapshot", "dir", tmpDir, "error", sizeErr)
	} else if err := writeSnapshotSize(tmpDir, apparentBytes); err != nil {
		logger.Error("failed to persist state store snapshot size", "dir", tmpDir, "error", err)
		sizeErr = err
	}

	m.publishMu.Lock()
	defer m.publishMu.Unlock()
	defer m.prune()

	if err := os.Rename(tmpDir, finalDir); err != nil {
		logger.Error("failed to finalize state store snapshot", "version", version, "error", err)
		_ = os.RemoveAll(tmpDir)
		return false
	}
	if err := syncDir(m.root); err != nil {
		logger.Error("failed to persist state store snapshot publication",
			"version", version, "dir", finalDir, "error", err)
		return false
	}
	if sizeErr == nil {
		if m.snapshotSizes == nil {
			m.snapshotSizes = map[int64]int64{}
		}
		m.snapshotSizes[version] = apparentBytes
	}
	logger.Info("state store snapshot created",
		"version", version, "dir", finalDir, "took", time.Since(start).String())

	// Snapshots can finish out of order, so only move the link forward.
	if version > m.lastPublished {
		if err := m.updateCurrentLink(SnapshotDirName(version)); err != nil {
			// The snapshot itself is intact and discoverable by name; only the
			// convenience symlink is stale. The link is part of the publication
			// contract, so record this attempt as a failure.
			logger.Error("failed to update state store snapshot current link",
				"version", version, "error", err)
			return false
		}
		m.lastPublished = version
	}
	snapshotMetrics.CurrentHeight.Record(context.Background(), m.lastPublished)
	return true
}

func (m *snapshotManager) newestSnapshotVersion() int64 {
	versions, err := ListSnapshotVersions(m.root)
	if err != nil {
		logger.Error("failed to list state store snapshots", "error", err)
		return 0
	}
	if len(versions) == 0 {
		return 0
	}
	return versions[len(versions)-1]
}

func (m *snapshotManager) snapshotModTime(version int64) time.Time {
	if version <= 0 {
		return time.Time{}
	}
	info, err := os.Stat(filepath.Join(m.root, SnapshotDirName(version)))
	if err != nil {
		logger.Error("failed to read state store snapshot modification time",
			"version", version, "error", err)
		return time.Time{}
	}
	return info.ModTime()
}

// removeStaleTmpDirs clears staging directories left behind by a crash or a
// shutdown that landed mid-snapshot. They are named after the snapshot they
// were staging, so they would otherwise sit there until that exact boundary
// came round again.
func (m *snapshotManager) removeStaleTmpDirs() {
	tmpLink := filepath.Join(m.root, snapshotCurrentTmpLink)
	if err := os.Remove(tmpLink); err != nil && !os.IsNotExist(err) {
		logger.Error("failed to remove stale state store snapshot link", "path", tmpLink, "error", err)
	}

	entries, err := os.ReadDir(m.root)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Error("failed to scan state store snapshots dir", "error", err)
		}
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), snapshotTmpPrefix) {
			continue
		}
		dir := filepath.Join(m.root, entry.Name())
		if err := os.RemoveAll(dir); err != nil {
			logger.Error("failed to remove stale snapshot tmp dir", "dir", dir, "error", err)
			continue
		}
		logger.Info("removed stale state store snapshot tmp dir", "dir", dir)
	}
}

// updateCurrentLink atomically points the current symlink at name.
func (m *snapshotManager) updateCurrentLink(name string) error {
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

func syncDir(path string) error {
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

// prune removes all but the newest 1+keepRecent snapshots.
func (m *snapshotManager) prune() {
	versions, err := ListSnapshotVersions(m.root)
	if err != nil {
		logger.Error("failed to list state store snapshots for pruning", "error", err)
		return
	}
	defer m.recordRetentionMetrics()
	keep := 1 + m.keepRecent
	if len(versions) <= keep {
		return
	}
	for _, v := range versions[:len(versions)-keep] {
		dir := filepath.Join(m.root, SnapshotDirName(v))
		if err := os.RemoveAll(dir); err != nil {
			logger.Error("failed to prune state store snapshot", "dir", dir, "error", err)
			continue
		}
		logger.Info("pruned state store snapshot", "dir", dir)
	}
}

func (m *snapshotManager) recordRetentionMetrics() {
	versions, err := ListSnapshotVersions(m.root)
	if err != nil {
		logger.Error("failed to list state store snapshots for metrics", "error", err)
		return
	}
	snapshotMetrics.RetainedCount.Record(context.Background(), int64(len(versions)))

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
				logger.Error("failed to read state store snapshot size", "dir", dir, "error", err)
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
	snapshotMetrics.ApparentBytes.Record(context.Background(), apparentBytes)
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

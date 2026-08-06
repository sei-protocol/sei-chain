package composite

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// Online state-store snapshots. Every SnapshotInterval blocks the store takes a
// Pebble checkpoint of each backend while the node keeps producing blocks.
// Checkpoints are hardlink trees, so they neither block the write path nor copy
// data. The result is an immutable, crash-consistent image of the query store.
//
// On-disk layout under <home>/data/state_store/snapshots/:
//
//	snapshots/
//	  current -> snapshot-NNNNN            (symlink to newest snapshot)
//	  snapshot-NNNNN/                      (immutable; NNNNN = label version)
//	    cosmos/<backend>/                  (Pebble checkpoint of Cosmos SS)
//	    evm/<backend>/                     (Pebble checkpoint of EVM SS, if split)
//
// Snapshots are taken at interval boundaries, at the same heights the state
// commit layer snapshots, and the label is exactly that height: it is the
// version the write path had just handed to the backends when the snapshot was
// requested. Placing a barrier in each backend's apply queue — rather than
// sampling what the backends had applied — is what makes the label exact
// without the request having to wait for anything. See requestSnapshot.
const (
	// SnapshotsDirName is the directory under data/state_store that holds
	// online snapshots.
	SnapshotsDirName = "snapshots"

	snapshotPrefix = "snapshot-"
	// snapshotDirLen is "snapshot-" + 20-digit zero-padded version.
	snapshotDirLen = len(snapshotPrefix) + 20

	snapshotCurrentLink    = "current"
	snapshotCurrentTmpLink = "current-tmp"
	snapshotTmpPrefix      = "tmp-"
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
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
	return versions, nil
}

// snapshotManager owns the snapshots directory and the one-at-a-time discipline
// for filling it. It has no goroutine of its own: snapshots are requested from
// the write path and completed on the backends' apply goroutines.
type snapshotManager struct {
	store      *CompositeStateStore
	root       string
	backend    string
	interval   int64
	keepRecent int

	mu sync.Mutex
	// lastRequested is the newest label already requested or on disk, so a
	// boundary is not snapshotted twice across a restart or a re-sent version.
	lastRequested int64
	stopped       bool
	// publishing tracks the goroutines finishing snapshots off, so stop can
	// wait them out. Requests are deliberately allowed to overlap: skipping a
	// boundary because the previous snapshot was still in flight would put SS
	// snapshots on different heights than SC's, which is the whole point of
	// taking them at boundaries.
	publishing sync.WaitGroup

	// publishMu serializes the publish step, which reads and rewrites the
	// shared directory (the current link, and pruning).
	publishMu     sync.Mutex
	lastPublished int64
}

// startSnapshotManager wires the manager into the composite store. It declines
// with a log line when a backend cannot take snapshots, so misconfigured
// deployments degrade to no SS snapshots instead of failing startup.
func (s *CompositeStateStore) startSnapshotManager(root string) {
	if s.config.SnapshotInterval <= 0 {
		return
	}
	cosmosScheduler, ok := s.cosmosStore.(types.CheckpointScheduler)
	if !ok || !cosmosScheduler.SupportsCheckpoint() {
		logger.Error("state store snapshotting disabled: cosmos backend does not support checkpoints",
			"backend", s.config.Backend)
		return
	}
	if s.evmStore != nil {
		evmScheduler, ok := s.evmStore.(types.CheckpointScheduler)
		if !ok || !evmScheduler.SupportsCheckpoint() {
			logger.Error("state store snapshotting disabled: EVM backend does not support checkpoints",
				"backend", s.config.Backend)
			return
		}
	}
	m := &snapshotManager{
		store:      s,
		root:       root,
		backend:    s.config.Backend,
		interval:   s.config.SnapshotInterval,
		keepRecent: s.config.SnapshotKeepRecent,
	}
	m.lastRequested = m.newestSnapshotVersion()
	m.lastPublished = m.lastRequested
	m.removeStaleTmpDirs()
	s.snapshotMgr = m
	logger.Info("state store snapshotting enabled",
		"root", root, "interval", m.interval, "keepRecent", m.keepRecent)
}

// stop prevents further snapshots and waits for the ones being published.
// Checkpoints already queued on the backends still run — they are drained as
// part of closing those backends — but once stopped they clean up after
// themselves instead of publishing, since a store being torn down should not
// leave a new snapshot half-linked into the directory.
func (m *snapshotManager) stop() {
	m.mu.Lock()
	m.stopped = true
	m.mu.Unlock()
	m.publishing.Wait()
}

// maybeSnapshot takes a snapshot when version lands on an interval boundary.
// It is called from the write path for every version, so the common case is the
// modulo test and nothing else.
func (m *snapshotManager) maybeSnapshot(version int64) {
	if m == nil || version <= 0 || m.interval <= 0 || version%m.interval != 0 {
		return
	}
	m.mu.Lock()
	skip := m.stopped || version <= m.lastRequested
	if !skip {
		m.lastRequested = version
	}
	m.mu.Unlock()
	if skip {
		return
	}
	if err := m.requestSnapshot(version); err != nil {
		logger.Error("state store snapshot failed", "version", version, "error", err)
	}
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
func (m *snapshotManager) requestSnapshot(version int64) error {
	name := SnapshotDirName(version)
	finalDir := filepath.Join(m.root, name)
	if _, err := os.Stat(finalDir); err == nil {
		return fmt.Errorf("snapshot dir %q already exists", finalDir)
	}
	tmpDir := filepath.Join(m.root, snapshotTmpPrefix+name)
	if err := os.RemoveAll(tmpDir); err != nil {
		return fmt.Errorf("clear stale snapshot tmp dir: %w", err)
	}

	targets := []struct {
		store types.CheckpointScheduler
		dest  string
	}{
		{m.store.cosmosStore.(types.CheckpointScheduler), filepath.Join(tmpDir, "cosmos", m.backend)},
	}
	if m.store.evmStore != nil {
		targets = append(targets, struct {
			store types.CheckpointScheduler
			dest  string
		}{m.store.evmStore.(types.CheckpointScheduler), filepath.Join(tmpDir, "evm", m.backend)})
	}
	for _, target := range targets {
		if err := os.MkdirAll(filepath.Dir(target.dest), 0o750); err != nil {
			_ = os.RemoveAll(tmpDir)
			return fmt.Errorf("create snapshot dir: %w", err)
		}
	}

	start := time.Now()
	var (
		mu        sync.Mutex
		remaining = len(targets)
		firstErr  error
	)
	// Set up before scheduling: a backend that is already at rest reports back
	// from inside ScheduleCheckpoint, before the loop has finished.
	for _, target := range targets {
		target.store.ScheduleCheckpoint(target.dest, func(err error) {
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
			m.startPublish(version, tmpDir, finalDir, outcome, start)
		})
	}
	return nil
}

// startPublish hands a finished set of checkpoints off to a goroutine. It runs
// on whichever backend's apply goroutine finished last, so it must not do the
// work itself: publishing renames directories and prunes old snapshots, and a
// writer stalled on that is a writer not applying blocks.
func (m *snapshotManager) startPublish(version int64, tmpDir, finalDir string, err error, start time.Time) {
	if err != nil {
		logger.Error("state store snapshot failed", "version", version, "error", err)
		_ = os.RemoveAll(tmpDir)
		return
	}
	// Taken under the same lock stop uses, so no goroutine is registered after
	// stop has started waiting.
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		_ = os.RemoveAll(tmpDir)
		return
	}
	m.publishing.Add(1)
	m.mu.Unlock()

	go func() {
		defer m.publishing.Done()
		m.publish(version, tmpDir, finalDir, start)
	}()
}

func (m *snapshotManager) publish(version int64, tmpDir, finalDir string, start time.Time) {
	m.publishMu.Lock()
	defer m.publishMu.Unlock()

	if err := os.Rename(tmpDir, finalDir); err != nil {
		logger.Error("failed to finalize state store snapshot", "version", version, "error", err)
		_ = os.RemoveAll(tmpDir)
		return
	}
	logger.Info("state store snapshot created",
		"version", version, "dir", finalDir, "took", time.Since(start).String())

	// Snapshots can finish out of order, so only move the link forward.
	if version > m.lastPublished {
		m.lastPublished = version
		if err := m.updateCurrentLink(SnapshotDirName(version)); err != nil {
			// The snapshot itself is intact and discoverable by name; only the
			// convenience symlink is stale.
			logger.Error("failed to update state store snapshot current link",
				"version", version, "error", err)
		}
	}
	m.prune()
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

// removeStaleTmpDirs clears staging directories left behind by a crash or a
// shutdown that landed mid-snapshot. They are named after the snapshot they
// were staging, so they would otherwise sit there until that exact boundary
// came round again.
func (m *snapshotManager) removeStaleTmpDirs() {
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
	return nil
}

// discardAbove removes every snapshot labeled above targetVersion and repoints
// `current` at the newest survivor, dropping the link entirely when none is
// left. Used after a rollback, whose whole point is that the versions those
// snapshots image are no longer the chain.
func (m *snapshotManager) discardAbove(targetVersion int64) {
	versions, err := ListSnapshotVersions(m.root)
	if err != nil {
		logger.Error("failed to list state store snapshots after rollback", "error", err)
		return
	}
	newest := int64(-1)
	for _, v := range versions {
		if v <= targetVersion {
			newest = v
			continue
		}
		dir := filepath.Join(m.root, SnapshotDirName(v))
		if err := os.RemoveAll(dir); err != nil {
			logger.Error("failed to remove state store snapshot above rollback target",
				"dir", dir, "error", err)
			continue
		}
		logger.Info("removed state store snapshot above rollback target",
			"dir", dir, "targetVersion", targetVersion)
	}
	link := filepath.Join(m.root, snapshotCurrentLink)
	if newest < 0 {
		if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
			logger.Error("failed to remove state store snapshot current link", "error", err)
		}
		return
	}
	if err := m.updateCurrentLink(SnapshotDirName(newest)); err != nil {
		logger.Error("failed to repoint state store snapshot current link after rollback", "error", err)
	}
}

// prune removes all but the newest 1+keepRecent snapshots.
func (m *snapshotManager) prune() {
	versions, err := ListSnapshotVersions(m.root)
	if err != nil {
		logger.Error("failed to list state store snapshots for pruning", "error", err)
		return
	}
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

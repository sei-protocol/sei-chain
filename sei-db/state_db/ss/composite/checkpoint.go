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

// Online state-store checkpoints. Every CheckpointInterval blocks the manager
// takes a Pebble checkpoint of each SS backend while the node keeps producing
// blocks — checkpoints are hardlink trees, so they neither block the write
// path nor copy data. The result is an immutable, crash-consistent image of
// the query store that flatkv-archive can hash and pack without racing the
// live database (the failure mode that previously forced archive donors to
// quiesce).
//
// On-disk layout under <home>/data/state_store/snapshots/:
//
//	snapshots/
//	  current -> snapshot-NNNNN            (symlink to newest checkpoint)
//	  snapshot-NNNNN/                      (immutable; NNNNN = label version)
//	    cosmos/<backend>/                  (Pebble checkpoint of Cosmos SS)
//	    evm/<backend>/                     (Pebble checkpoint of EVM SS, if split)
//
// The label is the minimum version the backends had fully APPLIED before the
// checkpoint was taken (a pre-barrier read; see createCheckpoint), so a
// checkpoint named snapshot-V is guaranteed to contain every version <= V.
// Content past V may be partial across backends, but an archive restore
// paired with a FlatKV snapshot at height H <= V refills everything > H
// through block replay, so completeness up to the label is the only invariant
// that matters.
const (
	// CheckpointsDirName is the directory under data/state_store that holds
	// online checkpoints.
	CheckpointsDirName = "snapshots"

	checkpointPrefix = "snapshot-"
	// checkpointDirLen is "snapshot-" + 20-digit zero-padded version.
	checkpointDirLen = len(checkpointPrefix) + 20

	checkpointCurrentLink    = "current"
	checkpointCurrentTmpLink = "current-tmp"
	checkpointTmpPrefix      = "tmp-"

	// checkpointPollInterval is how often the manager samples applied
	// versions to see whether a new interval boundary has been crossed.
	checkpointPollInterval = 10 * time.Second
)

// CheckpointDirName returns the directory name for a checkpoint labeled with
// the given version.
func CheckpointDirName(version int64) string {
	return fmt.Sprintf("%s%020d", checkpointPrefix, version)
}

// ParseCheckpointVersion parses a checkpoint directory name; ok is false for
// anything that is not a snapshot-<20 digits> name.
func ParseCheckpointVersion(name string) (version int64, ok bool) {
	if !strings.HasPrefix(name, checkpointPrefix) || len(name) != checkpointDirLen {
		return 0, false
	}
	v, err := strconv.ParseInt(name[len(checkpointPrefix):], 10, 64)
	if err != nil || v < 0 {
		return 0, false
	}
	return v, true
}

// ListCheckpointVersions returns the labels of all checkpoints under root in
// ascending order. A missing root is not an error (no checkpoints yet).
func ListCheckpointVersions(root string) ([]int64, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read checkpoints dir %q: %w", root, err)
	}
	var versions []int64
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if v, ok := ParseCheckpointVersion(entry.Name()); ok {
			versions = append(versions, v)
		}
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
	return versions, nil
}

// pendingWaiter is the optional barrier a backend exposes to drain its async
// apply queue (implemented by the Pebble MVCC DB and the SS wrappers).
type pendingWaiter interface {
	WaitForPendingWrites()
}

// checkpointManager periodically checkpoints the composite store's backends.
type checkpointManager struct {
	store      *CompositeStateStore
	root       string
	backend    string
	interval   int64
	keepRecent int

	quit chan struct{}
	wg   sync.WaitGroup
}

// startCheckpointManager wires the manager into the composite store. It
// refuses (with a log line, not an error) when a backend cannot take
// checkpoints, so misconfigured deployments degrade to the old behavior
// instead of failing startup.
func (s *CompositeStateStore) startCheckpointManager(root string) {
	if _, ok := s.cosmosStore.(types.Checkpointable); !ok {
		logger.Error("state store checkpointing disabled: cosmos backend does not support checkpoints",
			"backend", s.config.Backend)
		return
	}
	if s.evmStore != nil {
		if _, ok := s.evmStore.(types.Checkpointable); !ok {
			logger.Error("state store checkpointing disabled: EVM backend does not support checkpoints",
				"backend", s.config.Backend)
			return
		}
	}
	m := &checkpointManager{
		store:      s,
		root:       root,
		backend:    s.config.Backend,
		interval:   s.config.CheckpointInterval,
		keepRecent: s.config.CheckpointKeepRecent,
		quit:       make(chan struct{}),
	}
	s.checkpointMgr = m
	m.wg.Add(1)
	go m.run()
	logger.Info("state store checkpointing enabled",
		"root", root, "interval", m.interval, "keepRecent", m.keepRecent)
}

func (m *checkpointManager) stop() {
	close(m.quit)
	m.wg.Wait()
}

func (m *checkpointManager) run() {
	defer m.wg.Done()
	last := m.newestCheckpointVersion()
	ticker := time.NewTicker(checkpointPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.quit:
			return
		case <-ticker.C:
			v := m.store.minAppliedVersion()
			// Fire once per interval bucket: the first tick after the
			// applied version crosses a boundary the previous checkpoint
			// had not reached.
			if v <= 0 || v/m.interval <= last/m.interval {
				continue
			}
			if err := m.createCheckpoint(v); err != nil {
				logger.Error("state store checkpoint failed", "version", v, "error", err)
				continue
			}
			last = v
		}
	}
}

// minAppliedVersion returns the lowest latest-version marker across backends.
func (s *CompositeStateStore) minAppliedVersion() int64 {
	v := s.cosmosStore.GetLatestVersion()
	if s.evmStore != nil {
		if ev := s.evmStore.GetLatestVersion(); ev < v {
			v = ev
		}
	}
	return v
}

// waitForPendingWrites drains every backend's async apply queue.
func (s *CompositeStateStore) waitForPendingWrites() {
	if w, ok := s.cosmosStore.(pendingWaiter); ok {
		w.WaitForPendingWrites()
	}
	if s.evmStore != nil {
		if w, ok := s.evmStore.(pendingWaiter); ok {
			w.WaitForPendingWrites()
		}
	}
}

// createCheckpoint takes a checkpoint of every backend labeled with version,
// which the caller read BEFORE this call. Ordering is what makes the label a
// completeness guarantee: once version was visible as a latest-version
// marker, every changeset <= version had already been enqueued (the commit
// pipeline is sequential), so draining the queues before checkpointing
// ensures all of them are applied to Pebble. Without the barrier an
// empty-block SetLatestVersion — which writes the marker directly — could
// stamp the checkpoint with a version whose predecessors were still queued,
// leaving a hole below the label that block replay would never refill.
//
// Note the checkpoint's on-disk latest marker may read slightly below the
// label when the label came from trailing empty blocks (a drained data batch
// re-stamps the marker with its own, lower version). That understatement
// spans only versions with no data, so the checkpoint still contains every
// data version <= label; the first block applied after a restore pushes the
// marker forward again.
func (m *checkpointManager) createCheckpoint(version int64) error {
	start := time.Now()
	m.store.waitForPendingWrites()

	name := CheckpointDirName(version)
	finalDir := filepath.Join(m.root, name)
	if _, err := os.Stat(finalDir); err == nil {
		return nil // already exists (e.g. restart within the same bucket)
	}
	tmpDir := filepath.Join(m.root, checkpointTmpPrefix+name)
	if err := os.RemoveAll(tmpDir); err != nil {
		return fmt.Errorf("clear stale checkpoint tmp dir: %w", err)
	}

	cosmosDest := filepath.Join(tmpDir, "cosmos", m.backend)
	if err := os.MkdirAll(filepath.Dir(cosmosDest), 0o750); err != nil {
		return fmt.Errorf("create checkpoint dir: %w", err)
	}
	if err := m.store.cosmosStore.(types.Checkpointable).Checkpoint(cosmosDest); err != nil {
		_ = os.RemoveAll(tmpDir)
		return fmt.Errorf("checkpoint cosmos state store: %w", err)
	}
	if m.store.evmStore != nil {
		evmDest := filepath.Join(tmpDir, "evm", m.backend)
		if err := os.MkdirAll(filepath.Dir(evmDest), 0o750); err != nil {
			_ = os.RemoveAll(tmpDir)
			return fmt.Errorf("create EVM checkpoint dir: %w", err)
		}
		if err := m.store.evmStore.(types.Checkpointable).Checkpoint(evmDest); err != nil {
			_ = os.RemoveAll(tmpDir)
			return fmt.Errorf("checkpoint EVM state store: %w", err)
		}
	}

	if err := os.Rename(tmpDir, finalDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		return fmt.Errorf("finalize checkpoint dir: %w", err)
	}
	if err := m.updateCurrentLink(name); err != nil {
		return err
	}
	m.prune()
	logger.Info("state store checkpoint created",
		"version", version, "dir", finalDir, "took", time.Since(start).String())
	return nil
}

func (m *checkpointManager) newestCheckpointVersion() int64 {
	versions, err := ListCheckpointVersions(m.root)
	if err != nil {
		logger.Error("failed to list state store checkpoints", "error", err)
		return 0
	}
	if len(versions) == 0 {
		return 0
	}
	return versions[len(versions)-1]
}

// updateCurrentLink atomically points the current symlink at name.
func (m *checkpointManager) updateCurrentLink(name string) error {
	tmpLink := filepath.Join(m.root, checkpointCurrentTmpLink)
	_ = os.Remove(tmpLink)
	if err := os.Symlink(name, tmpLink); err != nil {
		return fmt.Errorf("create checkpoint current symlink: %w", err)
	}
	if err := os.Rename(tmpLink, filepath.Join(m.root, checkpointCurrentLink)); err != nil {
		return fmt.Errorf("swap checkpoint current symlink: %w", err)
	}
	return nil
}

// prune removes all but the newest 1+keepRecent checkpoints.
func (m *checkpointManager) prune() {
	versions, err := ListCheckpointVersions(m.root)
	if err != nil {
		logger.Error("failed to list state store checkpoints for pruning", "error", err)
		return
	}
	keep := 1 + m.keepRecent
	if len(versions) <= keep {
		return
	}
	for _, v := range versions[:len(versions)-keep] {
		dir := filepath.Join(m.root, CheckpointDirName(v))
		if err := os.RemoveAll(dir); err != nil {
			logger.Error("failed to prune state store checkpoint", "dir", dir, "error", err)
			continue
		}
		logger.Info("pruned state store checkpoint", "dir", dir)
	}
}

package gc

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("db", "gc")

// StorageGarbageCollector periodically prunes a set of PrunableStores against one shared
// config.RollbackWindow and config.LookbackWindow. Each cycle:
//
//  1. every store reports GetRollbackFloor(RollbackWindow)
//  2. snapshotCutLine = the minimum of those answers
//  3. historyCutLine = snapshotCutLine - LookbackWindow, or 0 when LookbackWindow is -1
//  4. every store reporting ExternalPruning gets PruneSnapshots(snapshotCutLine), then
//     PruneHistory(historyCutLine)
type StorageGarbageCollector struct {
	config *StorageGarbageCollectorConfig
	stores []PrunableStore
	ctx    context.Context
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewStorageGarbageCollector starts a collector that prunes stores every config.PruneInterval until
// Close is called or ctx is cancelled. Both ctx and config are required.
func NewStorageGarbageCollector(
	ctx context.Context,
	config *StorageGarbageCollectorConfig,
	stores []PrunableStore,
) (*StorageGarbageCollector, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid storage garbage collector config: %w", err)
	}

	s := &StorageGarbageCollector{
		config: config,
		stores: stores,
		ctx:    ctx,
		stopCh: make(chan struct{}),
	}
	s.wg.Add(1)
	go s.run()
	return s, nil
}

// Close stops the run loop and waits for it to exit. Must be called exactly once.
func (s *StorageGarbageCollector) Close() error {
	close(s.stopCh)
	s.wg.Wait()
	return nil
}

func (s *StorageGarbageCollector) run() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.PruneInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if err := prune(s.config, s.stores); err != nil {
				logger.Error("prune cycle failed", "err", err)
			}
		}
	}
}

// prune runs one prune cycle. See StorageGarbageCollector for the decision rules.
func prune(config *StorageGarbageCollectorConfig, stores []PrunableStore) error {
	if len(stores) == 0 {
		return nil
	}

	// Answers are positional so duplicate Name() values cannot mis-attribute one.
	decisions := make([]storeDecision, len(stores))
	snapshotCutLine := uint64(math.MaxUint64)
	for i, store := range stores {
		decisions[i].externalPruning = store.ExternalPruning()
		decisions[i].floor = store.GetRollbackFloor(config.RollbackWindow)
		snapshotCutLine = min(snapshotCutLine, decisions[i].floor)
	}

	historyCutLine := getHistoryCutLine(snapshotCutLine, config.LookbackWindow)

	logger.Info("pruning stores",
		"rollbackWindow", config.RollbackWindow,
		"lookbackWindow", config.LookbackWindow,
		"snapshotCutLine", snapshotCutLine,
		"historyCutLine", historyCutLine,
		"decisionByStore", describeDecisions(stores, decisions),
	)
	return pruneStores(stores, decisions, snapshotCutLine, historyCutLine)
}

// pruneStores issues the cycle's deletions: snapshots below snapshotHeight and history below
// historyHeight. A height of 0 is skipped, and a store that prunes itself is left alone.
func pruneStores(
	stores []PrunableStore,
	decisions []storeDecision,
	snapshotHeight uint64,
	historyHeight uint64,
) error {
	var errs error
	for i, store := range stores {
		if !decisions[i].externalPruning {
			continue
		}
		if snapshotHeight > 0 {
			if err := store.PruneSnapshots(snapshotHeight); err != nil {
				errs = errors.Join(errs, fmt.Errorf("failed to prune %s snapshots below %d: %w",
					store.Name(), snapshotHeight, err))
			}
		}
		if historyHeight > 0 {
			if err := store.PruneHistory(historyHeight); err != nil {
				errs = errors.Join(errs, fmt.Errorf("failed to prune %s history below %d: %w",
					store.Name(), historyHeight, err))
			}
		}
	}
	return errs
}

// storeDecision records what one store answered, for the prune log.
type storeDecision struct {
	floor           uint64
	externalPruning bool
}

// describeDecisions renders one "name=floor" entry per store, in store order, for the prune log. A
// store that prunes itself is tagged selfPruned.
func describeDecisions(stores []PrunableStore, decisions []storeDecision) string {
	var sb strings.Builder
	for i, store := range stores {
		if i > 0 {
			sb.WriteString(" ")
		}
		if decisions[i].externalPruning {
			fmt.Fprintf(&sb, "%s=%d", store.Name(), decisions[i].floor)
			continue
		}
		fmt.Fprintf(&sb, "%s=%d(selfPruned)", store.Name(), decisions[i].floor)
	}
	return sb.String()
}

// getHistoryCutLine returns the height history may be pruned below: snapshotCutLine less
// lookbackWindow. It returns 0 — keep everything — when lookbackWindow is -1 (infinite retention)
// or when the window reaches below genesis.
func getHistoryCutLine(snapshotCutLine uint64, lookbackWindow int64) uint64 {
	if lookbackWindow < 0 {
		return 0 // infinite retention
	}
	window := uint64(lookbackWindow)
	if snapshotCutLine <= window {
		return 0
	}
	return snapshotCutLine - window
}

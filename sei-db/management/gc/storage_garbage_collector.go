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

// StorageGarbageCollector periodically prunes a set of PrunableStores.
//
// One config.RollbackWindow and one config.LookbackWindow cover every store. Each cycle:
//
//  1. ask every store GetRollbackFloor(RollbackWindow) — the earliest height it could still be
//     asked to roll back to, which it resolves against its own head
//  2. snapshotCutLine = min of those answers, the deepest rollback the fleet still owes
//  3. historyCutLine sits LookbackWindow below snapshotCutLine, so the lookback window falls
//     entirely beneath the deepest promised rollback point rather than overlapping it; a
//     LookbackWindow of -1 pins it to 0, which is infinite history retention (snapshots are still
//     reclaimed to the floor)
//  4. on every store reporting ExternalPruning, PruneSnapshots(snapshotCutLine) then
//     PruneHistory(historyCutLine)
//
// The two windows stack rather than share: RollbackWindow buys the ability to rewind, LookbackWindow
// buys history that is still readable after rewinding as far as that promise allows. Deriving the
// history cut line by subtracting the second from the first is what makes the lookback guarantee
// independent of how deep a rollback actually goes.
type StorageGarbageCollector struct {
	config *StorageGarbageCollectorConfig
	stores []PrunableStore
	ctx    context.Context
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewStorageGarbageCollector starts a collector that prunes stores every config.PruneInterval
// until Close is called or ctx is cancelled. ctx and config are both required: run dereferences
// ctx on a background goroutine, where a nil would panic unrecoverably instead of surfacing here.
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

// pruneStores issues the deletions the cycle decided on: snapshots below snapshotHeight, history
// below historyHeight — normally the deeper of the two, and 0 (never prune) under an infinite
// lookback window. See StorageGarbageCollector for why the two depths differ.
//
// A cut line of 0 is skipped rather than passed down. It means nothing is eligible, which every
// store would absorb as a no-op anyway; not making the call keeps a cycle that decided to delete
// nothing from reaching the deletion paths at all.
//
// A store is skipped only when it prunes itself — it still answered above, and is still protected by
// the minimum its answer produced, but its own pruner is the one enforcing its retention (see
// PrunableStore.ExternalPruning).
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

// storeDecision records what one store answered, for the prune log. The windows and the resulting
// heights are shared by every store and so are logged once alongside these rather than repeated
// per entry.
type storeDecision struct {
	floor           uint64
	externalPruning bool
}

// describeDecisions renders one entry per store, in store order, for the prune log. It is the stated
// mechanism for reconstructing a deletion after the fact, which is why every store appears whatever
// it answered: a floor of 0 is the entry that explains a cycle that pruned nothing, and naming the
// store that produced it is the whole value of the line.
//
// A store that answered but prunes itself is tagged selfPruned, because its floor is in the minimum
// below while no deletion follows on it — without the tag that reads like a prune that silently did
// nothing.
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
// lookbackWindow, which places the lookback window entirely beneath the deepest rollback the fleet
// still owes rather than overlapping it.
//
// Subtracting from the minimum of the stores' answers is what makes the result safe without clamping
// any of them: it can only sit at or below every store's own floor.
//
// A lookbackWindow of -1 is infinite retention: it returns 0, the literal height "keep everything
// from block 0 up", so history is never pruned however far the snapshot floor advances. The same 0
// falls out when a finite window reaches below genesis, and it needs no special handling on the
// other end either — every store's PruneHistory absorbs a height of 0 as a no-op.
func getHistoryCutLine(snapshotCutLine uint64, lookbackWindow int64) uint64 {
	if lookbackWindow < 0 {
		return 0 // -1: infinite retention, nothing below the snapshot floor is ever given up
	}
	window := uint64(lookbackWindow)
	if snapshotCutLine <= window {
		return 0
	}
	return snapshotCutLine - window
}

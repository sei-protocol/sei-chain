package gc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("db", "gc")

// StorageGarbageCollector periodically prunes a set of PrunableStores.
//
// Every store shares config.RollbackWindow and may request extra history via
// GetRetentionWindow. Each cycle:
//
//  1. head = min non-zero GetLatestBlock across stores
//  2. per store: cutLine = head - RollbackWindow - GetRetentionWindow, skipping the store
//     when that is 0 (infinite retention, or head still inside its own window)
//  3. ask GetPruningBoundary(cutLine): a positive answer votes, and CannotServeRollback
//     abandons the cycle while RollbackWindow > 0
//  4. pruneHeight = min of the positive answers; PruneBelow(pruneHeight) on every store
//     that answered positively
//
// Step 4 prunes to the shared minimum rather than to each store's own boundary so that a
// retained snapshot stays restorable: contiguous stores must still hold the blocks that follow
// it. The side effect is one effective retention across the fleet — receiptDB retention 100_000
// also keeps SC/SS snapshots that far back, even though those stores report retention 0. That
// uniform prune is an intentional Giga tradeoff, not independent per-store retention.
//
// Two properties do the safety work.
//
// Answers are bounded by the cut line they were given, so pruneHeight <= head - RollbackWindow
// holds by construction and the collector never clamps. That yields the invariant: a prune cannot
// take away a rollback that was possible before it. Full headroom is not promised after a
// rollback has already consumed part of the window.
//
// And a store that can serve no rollback at all is not merely skipped, it abandons the cycle. It
// will replay forward once its first snapshot lands, so pruning the others now could delete the
// range it replays from, and a store contributing no boundary is not covered by the minimum in
// step 4. See CannotServeRollback for the limits of that signal.
//
// Precondition: every store passed in is live. A store disabled for this node is not
// instantiated and never reaches the collector, so the set holds no permanently-empty member.
//
// PruneBelow failures are joined and do not skip later stores: permission-to-drop is not
// transactional, and one unhealthy store must not block pruning of the others.
//
// The type is a ticker around prune; the decision logic lives in prune for unit testing.
// Not safe for concurrent use (Close must be called exactly once).
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

	globalLatestBlock, err := getGlobalLatestBlock(stores)
	if err != nil {
		return err
	}
	if globalLatestBlock == 0 {
		logger.Info("skipping pruning: no store has a latest block")
		return nil
	}

	// Answers are positional so duplicate Name() values cannot mis-attribute one.
	decisions := make([]storeDecision, len(stores))
	var pruneHeight uint64
	blockedBy := -1
	for i, store := range stores {
		retention := store.GetRetentionWindow()
		cutLine := getCutLine(globalLatestBlock, config.RollbackWindow, retention)
		if cutLine == 0 {
			// Infinite retention, or head still inside this store's retain window: never asked.
			continue
		}

		decisions[i].cutLine = cutLine
		decisions[i].boundary = store.GetPruningBoundary(cutLine)
		if decisions[i].boundary == CannotServeRollback {
			if config.RollbackWindow > 0 {
				// One blocker is enough to abandon the cycle, so stop asking.
				blockedBy = i
				break
			}
			// RollbackWindow 0 waives the guarantee, leaving this store nothing to protect.
			// The others must still be asked, or the waiver would have nothing to prune.
			continue
		}
		if pruneHeight == 0 || decisions[i].boundary < pruneHeight {
			pruneHeight = decisions[i].boundary
		}
	}

	if blockedBy >= 0 {
		// No decisionByStore here: the break leaves later stores unasked, and they would render
		// as "notAsked" — indistinguishable from a store skipped at the cutLine == 0 branch.
		logger.Info("pruning blocked: store cannot serve the rollback window yet",
			"store", stores[blockedBy].Name(),
			"globalLatestBlock", globalLatestBlock,
		)
		return nil
	}
	if pruneHeight == 0 {
		logger.Info("skipping pruning: no store reported a pruning boundary",
			"globalLatestBlock", globalLatestBlock,
			"decisionByStore", describeDecisions(stores, decisions),
		)
		return nil
	}

	logger.Info("pruning stores",
		"globalLatestBlock", globalLatestBlock,
		"pruneHeight", pruneHeight,
		"decisionByStore", describeDecisions(stores, decisions),
	)
	var pruneErrs error
	for i, store := range stores {
		// Covers both a never-asked store and one that cannot serve a rollback.
		if decisions[i].boundary == 0 {
			continue
		}
		if err := store.PruneBelow(pruneHeight); err != nil {
			pruneErrs = errors.Join(pruneErrs, fmt.Errorf("failed to prune %s below %d: %w", store.Name(), pruneHeight, err))
		}
	}
	return pruneErrs
}

// storeDecision records what the collector asked one store and what it answered, for the prune
// log. cutLine == 0 means the store was never asked, which is what separates it from a store that
// was asked and answered 0.
type storeDecision struct {
	cutLine  uint64
	boundary uint64
}

// describeDecisions renders one entry per store, in store order, for the prune log. The three
// outcomes render differently on purpose — never asked, cannot serve a rollback, and reported a
// boundary — since collapsing them would cost exactly the distinction wanted when auditing a
// deletion after the fact. Each asked store carries the cut line its answer is a function of.
func describeDecisions(stores []PrunableStore, decisions []storeDecision) string {
	var sb strings.Builder
	for i, store := range stores {
		if i > 0 {
			sb.WriteString(" ")
		}
		switch {
		case decisions[i].cutLine == 0:
			fmt.Fprintf(&sb, "%s=notAsked", store.Name())
		case decisions[i].boundary == CannotServeRollback:
			fmt.Fprintf(&sb, "%s=cannotServeRollback(cutLine=%d)", store.Name(), decisions[i].cutLine)
		default:
			fmt.Fprintf(&sb, "%s=%d(cutLine=%d)", store.Name(), decisions[i].boundary, decisions[i].cutLine)
		}
	}
	return sb.String()
}

// getCutLine returns head - RollbackWindow - retention for retention >= 0.
// Returns 0 when retention < 0, when the combined window overflows uint64, or when
// head is still inside that window (unsigned subtraction must not wrap).
func getCutLine(globalLatestBlock uint64, rollbackWindow uint64, retention int64) uint64 {
	if retention < 0 {
		return 0
	}
	totalRetainWindow := rollbackWindow + uint64(retention)
	if totalRetainWindow < rollbackWindow {
		// Addition wrapped; treat as an unsatisfiable retain window (skip pruning).
		return 0
	}
	if globalLatestBlock <= totalRetainWindow {
		return 0
	}
	return globalLatestBlock - totalRetainWindow
}

// getGlobalLatestBlock returns the smallest non-zero GetLatestBlock among stores, or 0 when no
// store has data. The minimum keeps a lagging store from being pruned past blocks it still needs.
//
// Heads of 0 are skipped rather than treated as "unknown". That necessarily excludes the store
// holding nothing from the minimum, so the minimum cannot protect it; what protects it is
// CannotServeRollback, plus the store-set precondition on StorageGarbageCollector.
func getGlobalLatestBlock(stores []PrunableStore) (uint64, error) {
	var blockNum uint64
	for _, store := range stores {
		storeHeight, err := store.GetLatestBlock()
		if err != nil {
			return 0, fmt.Errorf("failed to read latest block from %s: %w", store.Name(), err)
		}
		if storeHeight == 0 {
			continue
		}
		if blockNum == 0 || storeHeight < blockNum {
			blockNum = storeHeight
		}
	}
	return blockNum, nil
}

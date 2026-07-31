package gc

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("db", "gc")

// StorageGarbageCollector periodically prunes a set of PrunableStores.
//
// All stores share config.RollbackWindow. Each store may request extra history via
// GetRetentionWindow (see that method). SC/SS return 0 — the collector only drives
// their snapshot pruning for the shared window; SS version-history pruning is separate.
//
// Each cycle:
//  1. head = min non-zero GetLastestBlock across stores
//  2. per store: cutLine = head - RollbackWindow - GetRetentionWindow
//     (skipped when cutLine == 0: infinite retention, or head still inside the window)
//  3. ask GetPruningBoundry(cutLine); 0 = opt out for this cycle
//  4. pruneHeight = min of non-zero answers; PruneBelow(pruneHeight) on every store
//     that answered non-zero
//
// Step 4 uses the shared min (not each store's own boundry) so a snapshot remains
// restorable: contiguous stores must still hold the blocks that follow it.
//
// Invariant: if RollbackWindow blocks of rollback were possible before a prune, that
// prune does not take the ability away. Full headroom is not promised after a rollback
// has already consumed part of the window.
//
// The type is a ticker around prune; decision logic lives in prune for unit testing.
// Not safe for concurrent use (Close must be called exactly once).
type StorageGarbageCollector struct {
	config *StorageGarbageCollectorConfig
	stores []PrunableStore
	ctx    context.Context
	stopCh chan struct{}
	wg     sync.WaitGroup
}

func NewStorageGarbageCollector(
	ctx context.Context,
	config *StorageGarbageCollectorConfig,
	stores []PrunableStore,
) (*StorageGarbageCollector, error) {
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

	globalLatestBlock, err := getGlobalLastestBlock(stores)
	if err != nil {
		return err
	}
	if globalLatestBlock == 0 {
		logger.Info("skipping pruning: no store has a latest block")
		return nil
	}

	// Boundries are positional so duplicate Name() values cannot mis-attribute an opt-out.
	pruningBoundries := make([]uint64, len(stores))
	var pruneHeight uint64
	for i, store := range stores {
		retention := store.GetRetentionWindow()
		cutLine := getCutLine(globalLatestBlock, config.RollbackWindow, retention)
		if cutLine == 0 {
			// Infinite retention, or head still inside this store's retain window.
			continue
		}

		pruningBoundries[i] = store.GetPruningBoundry(cutLine)
		if pruningBoundries[i] == 0 {
			// Opt-out (e.g. no completed snapshot yet).
			continue
		}
		if pruneHeight == 0 || pruningBoundries[i] < pruneHeight {
			pruneHeight = pruningBoundries[i]
		}
	}
	if pruneHeight == 0 {
		logger.Info("skipping pruning: no store reported a pruning boundry",
			"globalLatestBlock", globalLatestBlock,
			"pruningBoundryByStore", describeAnswers(stores, pruningBoundries),
		)
		return nil
	}

	logger.Info("pruning stores",
		"globalLatestBlock", globalLatestBlock,
		"pruneHeight", pruneHeight,
		"pruningBoundryByStore", describeAnswers(stores, pruningBoundries),
	)
	for i, store := range stores {
		if pruningBoundries[i] == 0 {
			continue
		}
		if err := store.PruneBelow(pruneHeight); err != nil {
			return fmt.Errorf("failed to prune %s below %d: %w", store.Name(), pruneHeight, err)
		}
	}
	return nil
}

func describeAnswers(stores []PrunableStore, pruningBoundries []uint64) string {
	var sb strings.Builder
	for i, store := range stores {
		if i > 0 {
			sb.WriteString(" ")
		}
		fmt.Fprintf(&sb, "%s=%d", store.Name(), pruningBoundries[i])
	}
	return sb.String()
}

// getCutLine returns head - RollbackWindow - retention for retention >= 0.
// Returns 0 when retention < 0, or when head is still inside that combined window
// (unsigned subtraction must not wrap).
func getCutLine(globalLatestBlock uint64, rollbackWindow uint64, retention int64) uint64 {
	if retention < 0 {
		return 0
	}
	totalRetainWindow := rollbackWindow + uint64(retention)
	if globalLatestBlock <= totalRetainWindow {
		return 0
	}
	return globalLatestBlock - totalRetainWindow
}

// getGlobalLastestBlock returns the smallest non-zero GetLastestBlock among stores.
// Using the min keeps a lagging store from being pruned past blocks it still needs.
// Heads of 0 are skipped so an uninitialized store does not stall pruning.
// Returns 0 when no store has data.
func getGlobalLastestBlock(stores []PrunableStore) (uint64, error) {
	var blockNum uint64
	for _, store := range stores {
		storeHeight, err := store.GetLastestBlock()
		if err != nil {
			return 0, fmt.Errorf("failed to read lastest block from %s: %w", store.Name(), err)
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

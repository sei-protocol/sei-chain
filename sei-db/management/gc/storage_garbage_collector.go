package gc

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("db", "gc")

// StorageGarbageCollector manages deletion of stored data across a set of stores.
//
// Each cycle locates the head of the chain, derives the cut line the system must remain able to serve, asks every store
// how far back it must retain data to serve that cut line, and prunes every store below the lowest answer. Deleting to
// the lowest answer rather than to each store's own is what keeps the stores mutually usable: a snapshot is only
// restorable if the contiguous stores still hold the blocks that follow it.
//
// The StorageGarbageCollector performs state deletion while maintaining the following invariant:
// "If it's possible to roll back at least config.RollbackWindow blocks, then any state deletion operation
// will retain the system's ability to roll back at least config.RollbackWindow blocks." That is to say,
// while the StorageGarbageCollector cannot unilaterally guarantee the ability to roll back config.RollbackWindow
// blocks by itself, it guarantees that the act of deleting old state does not prevent rollback within this
// rollback window.
//
// StorageGarbageCollector is not thread safe.
type StorageGarbageCollector struct {

	// Configuration for this storage garbage collector.
	config *StorageGarbageCollectorConfig

	// The stores this collector prunes.
	stores []PrunableStore

	// Cancelled to signal the run loop to stop.
	ctx context.Context

	// Closed by Close to signal the run loop to stop.
	stopCh chan struct{}

	// Tracks the run loop goroutine so Close can wait for it to exit.
	wg sync.WaitGroup
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

// Close stops the storage garbage collector and waits for its run loop to exit. It must be called exactly once.
func (s *StorageGarbageCollector) Close() error {
	close(s.stopCh)
	s.wg.Wait()
	return nil
}

// run periodically drives prune cycles until the manager is stopped. All decision logic lives in prune so it can
// be unit tested without threading.
func (s *StorageGarbageCollector) run() {
	defer s.wg.Done()

	//nolint:gosec // G115 - Config.Validate rejects PruneIntervalSeconds large enough to overflow this conversion.
	ticker := time.NewTicker(time.Duration(s.config.PruneIntervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if err := s.prune(); err != nil {
				logger.Error("prune cycle failed", "err", err)
			}
		}
	}
}

// prune performs a single prune cycle: it locates the head of the chain, derives the cut line from it, asks every store
// how far back it must retain data to serve that cut line, and prunes every store below the lowest answer.
func (s *StorageGarbageCollector) prune() error {
	if len(s.stores) == 0 {
		return nil
	}
	globalLatestBlock, err := getGlobalLastCommittedBlock(s.stores)
	if err != nil {
		return err
	}
	if globalLatestBlock == 0 {
		logger.Debug("skipping pruning, no store has committed a block")
		return nil
	}

	cutLine := getCutLine(globalLatestBlock, s.config.RollbackWindow, s.config.StoreRetention)
	if cutLine == 0 {
		logger.Debug("skipping pruning, the chain is younger than the retain window",
			"globalLatestBlock", globalLatestBlock)
		return nil
	}

	// Every store must keep back to its own answer, so the system may only prune below the lowest of them. Pruning
	// below a higher answer would delete blocks that a lower-answering store just reported it still needs, which is
	// exactly how deletion would break the rollback invariant.
	//
	// A store that answers 0 is treated as unknown rather than empty: a snapshot may be mid-write and take hours to
	// land. Skipping that store and pruning everyone else would delete the contiguous blocks the in-flight snapshot
	// will need once it appears, so the whole cycle is skipped until every store reports something to retain.
	var pruneHeight uint64
	oldestBlockToRetainByStore := make(map[string]uint64, len(s.stores))
	for _, store := range s.stores {
		oldestBlockToRetain := store.GetOldestBlockToRetain(cutLine)
		oldestBlockToRetainByStore[store.Name()] = oldestBlockToRetain
		if oldestBlockToRetain == 0 {
			logger.Debug("skipping pruning, a store holds no data to retain",
				"cutLine", cutLine, "oldestBlockToRetainByStore", oldestBlockToRetainByStore)
			return nil
		}
		if pruneHeight == 0 || oldestBlockToRetain < pruneHeight {
			pruneHeight = oldestBlockToRetain
		}
	}

	logger.Info("pruning storage",
		"globalLatestBlock", globalLatestBlock,
		"cutLine", cutLine,
		"pruneHeight", pruneHeight,
		"oldestBlockToRetainByStore", oldestBlockToRetainByStore,
	)
	for _, store := range s.stores {
		if err := store.PruneBelow(pruneHeight); err != nil {
			return fmt.Errorf("failed to prune %s below %d: %w", store.Name(), pruneHeight, err)
		}
	}
	return nil
}

// getCutLine returns the oldest block the system must remain able to serve, which is the head of the chain less the
// rollback window and the extra retention.
//
// Returns 0 to mean "prune nothing", which is the case for a chain younger than that combined window. The comparison has
// to happen before the subtraction: these are unsigned, so subtracting past zero wraps to a cut line far above the head
// of the chain, and pruning to it would delete everything.
//
// Config.Validate rejects a rollback window and retention that overflow when summed, so retainWindow below is exact.
func getCutLine(globalLatestBlock uint64, rollbackWindow uint64, retention uint64) uint64 {
	retainWindow := rollbackWindow + retention
	if globalLatestBlock <= retainWindow {
		return 0
	}
	return globalLatestBlock - retainWindow
}

// getGlobalLastCommittedBlock reads the head height of every store and returns the smallest.
//
// The smallest head is the head of the chain as far as the collector can tell, because it bounds what the system can
// actually serve. Measuring the cut line from a store that has run ahead of the others would let the collector prune a
// lagging store past blocks that store still needs in order to serve a rollback.
//
// Heads of 0 are skipped: a store that has ingested nothing would otherwise hold the head of the whole system at 0 and
// stall pruning everywhere. Skipping it does not put its data at risk, because whatever it holds, its
// GetOldestBlockToRetain answer still bounds the prune height. Returns 0 when no store has committed a block.
func getGlobalLastCommittedBlock(stores []PrunableStore) (uint64, error) {
	var blockNum uint64
	for _, store := range stores {
		storeHeight, err := store.GetLastCommittedBlock()
		if err != nil {
			return 0, fmt.Errorf("failed to read last committed block from %s: %w", store.Name(), err)
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

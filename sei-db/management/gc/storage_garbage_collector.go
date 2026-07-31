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
// be unit tested without threading; the ticker path is covered by constructing a collector with a short
// config.PruneInterval.
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
		logger.Info("skipping pruning, no store has committed a block")
		return nil
	}

	cutLine := getCutLine(globalLatestBlock, s.config.RollbackWindow, s.config.StoreRetention)
	if cutLine == 0 {
		logger.Info("skipping pruning, the chain is younger than the retain window",
			"globalLatestBlock", globalLatestBlock)
		return nil
	}

	// Every participating store must keep back to its own answer, so the system may only prune below the lowest of
	// them. Pruning below a higher answer would delete blocks that a lower-answering store just reported it still
	// needs, which is exactly how deletion would break the rollback invariant.
	//
	// A store that answers 0 is left out of both the prune-height vote and PruneBelow: it holds no snapshot yet, is
	// disabled, or is intentionally retaining forever (see GetOldestBlockToRetain).
	//
	// The answers are kept positionally rather than keyed by Name so that two stores sharing a name cannot make the
	// collector prune one that opted out. Names are for logs only.
	var pruneHeight uint64
	oldestBlockToRetain := make([]uint64, len(s.stores))
	for i, store := range s.stores {
		oldestBlockToRetain[i] = store.GetOldestBlockToRetain(cutLine)
		if oldestBlockToRetain[i] == 0 {
			continue
		}
		if pruneHeight == 0 || oldestBlockToRetain[i] < pruneHeight {
			pruneHeight = oldestBlockToRetain[i]
		}
	}
	if pruneHeight == 0 {
		logger.Info("skipping pruning, no store has data to retain",
			"cutLine", cutLine, "oldestBlockToRetainByStore", s.describeAnswers(oldestBlockToRetain))
		return nil
	}

	logger.Info("pruning storage",
		"globalLatestBlock", globalLatestBlock,
		"cutLine", cutLine,
		"pruneHeight", pruneHeight,
		"oldestBlockToRetainByStore", s.describeAnswers(oldestBlockToRetain),
	)
	for i, store := range s.stores {
		if oldestBlockToRetain[i] == 0 {
			continue
		}
		if err := store.PruneBelow(pruneHeight); err != nil {
			return fmt.Errorf("failed to prune %s below %d: %w", store.Name(), pruneHeight, err)
		}
	}
	return nil
}

// describeAnswers renders the per-store retain answers for a log line, in store order so duplicate names stay
// distinguishable.
func (s *StorageGarbageCollector) describeAnswers(oldestBlockToRetain []uint64) string {
	var sb strings.Builder
	for i, store := range s.stores {
		if i > 0 {
			sb.WriteString(" ")
		}
		fmt.Fprintf(&sb, "%s=%d", store.Name(), oldestBlockToRetain[i])
	}
	return sb.String()
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

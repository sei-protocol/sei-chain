package storagemanager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("db", "storagemanager")

// StorageManager manages deletion of stored data across the SC, SS, and StateWAL stores.
//
// The StorageManager performs state deletion while maintaining the following invariant:
// "If it's possible to roll back at least config.RollbackWindow blocks, then any state deletion operation
// will retain the system's ability to roll back at least config.RollbackWindow blocks." That is to say,
// while the StorageManager cannot unilaterally guarantee the ability to roll back config.RollbackWindow
// blocks by itself, it guarantees that the act of deleting old state does not prevent rollback within this
// rollback window.
//
// StorageManager is not thread safe.
type StorageManager struct {

	// Configuration for this storage manager.
	config *StorageManagerConfig

	// The store that contains the mutable state.
	commitStore SnapshotStore

	// The store that contains historical state.
	stateStore SnapshotStore

	// The WAL containing changesets for each block (i.e. the key-value pairs that change each block).
	stateWAL StreamStore

	// Cancelled to signal the run loop to stop.
	ctx context.Context

	// Closed by Close to signal the run loop to stop.
	stopCh chan struct{}

	// Tracks the run loop goroutine so Close can wait for it to exit.
	wg sync.WaitGroup
}

func NewStorageManager(
	ctx context.Context,
	config *StorageManagerConfig,
	commitStore SnapshotStore,
	stateStore SnapshotStore,
	stateWAL StreamStore,
) (*StorageManager, error) {

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid storage manager config: %w", err)
	}

	s := &StorageManager{
		config:      config,
		commitStore: commitStore,
		stateStore:  stateStore,
		stateWAL:    stateWAL,
		ctx:         ctx,
		stopCh:      make(chan struct{}),
	}

	s.wg.Add(1)
	go s.run()

	return s, nil
}

// Close stops the storage manager and waits for its run loop to exit. It must be called exactly once.
func (s *StorageManager) Close() error {
	close(s.stopCh)
	s.wg.Wait()
	return nil
}

// run periodically drives prune cycles until the manager is stopped. All decision logic lives in pruneOnce so it can
// be unit tested without threading.
func (s *StorageManager) run() {
	defer s.wg.Done()

	//nolint:gosec // G115 - PruneIntervalSeconds is a config value in seconds; overflow requires a >290-year interval.
	ticker := time.NewTicker(time.Duration(s.config.PruneIntervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			err := prune(
				s.config.RollbackWindow,
				s.commitStore,
				s.stateStore,
				s.stateWAL)
			if err != nil {
				logger.Error("prune cycle failed", "err", err)
			}
		}
	}
}

// pruneOnce performs a single prune cycle: it observes the blocks retained by each managed store, computes how far
// each store may prune while preserving the rollback window, and issues the prune commands. It is the testable core of
// the manager and performs no threading.
func prune(
	rollbackWindow uint64,
	commitStore SnapshotStore,
	stateStore SnapshotStore,
	stateWAL StreamStore,
) error {
	stateWALStart, stateWALEnd, stateWALHasData, err := stateWAL.GetStoredBlocks()
	if err != nil {
		return fmt.Errorf("failed to read stored blocks from state WAL: %w", err)
	}
	commitBlocks, err := commitStore.GetStoredBlocks()
	if err != nil {
		return fmt.Errorf("failed to read stored blocks from commit store: %w", err)
	}
	stateBlocks, err := stateStore.GetStoredBlocks()
	if err != nil {
		return fmt.Errorf("failed to read stored blocks from state store: %w", err)
	}

	if !stateWALHasData || len(commitBlocks) == 0 || len(stateBlocks) == 0 {
		// We only prune if every store has at least some data. Otherwise we risk breaking primary invariant
		// (i.e. breaking the ability to roll back by act of deletion).
		logger.Info("skipping pruning, not all stores have data",
			"commitStore", commitBlocks,
			"stateStore", stateBlocks,
			"stateWAL", blockRange(stateWALStart, stateWALEnd),
		)

		return nil
	}

	// The latest committed block is defined by the head of the state WAL.
	latestBlock := stateWALEnd

	// The oldest block we must remain able to roll back to.
	var oldestBlockNeeded uint64
	if latestBlock > rollbackWindow {
		oldestBlockNeeded = latestBlock - rollbackWindow
	}

	commitFloor := snapshotPruningFloor(commitBlocks, oldestBlockNeeded)
	stateFloor := snapshotPruningFloor(stateBlocks, oldestBlockNeeded)
	stateWALFloor := min(commitFloor, stateFloor)

	// Record the whole cycle in a single log line: for each store, its current blocks and the blocks we expect it to
	// hold once pruning has completed.
	logger.Info("pruning storage",
		"latestBlock", latestBlock,
		"oldestBlockNeeded", oldestBlockNeeded,
		"commitStoreInitial", commitBlocks,
		"commitStoreFinal", blocksAtOrAbove(commitBlocks, commitFloor),
		"stateStoreInitial", stateBlocks,
		"stateStoreFinal", blocksAtOrAbove(stateBlocks, stateFloor),
		"stateWALInitial", blockRange(stateWALStart, stateWALEnd),
		"stateWALFinal", blockRange(stateWALFloor, stateWALEnd),
	)

	if err := commitStore.PruneBelow(commitFloor); err != nil {
		return fmt.Errorf("failed to prune commit store below %d: %w", commitFloor, err)
	}
	if err := stateStore.PruneBelow(stateFloor); err != nil {
		return fmt.Errorf("failed to prune state store below %d: %w", stateFloor, err)
	}
	if err := stateWAL.PruneBelow(stateWALFloor); err != nil {
		return fmt.Errorf("failed to prune state WAL below %d: %w", stateWALFloor, err)
	}

	return nil
}

// Given a list of snapshot block numbers, determine the lowest snapshot we need to keep in order to be able
// to roll back to the target block number.
//
// Returns the highest numbered block from snapshotBlocks that is less than or equal to the rollbackTarget. If every
// snapshot is greater than rollbackTarget, the lowest snapshot is returned, since none can be safely pruned. ok is
// false when snapshotBlocks is empty. snapshotBlocks must be sorted in ascending order, per the SnapshotStore contract.
func snapshotPruningFloor(
	// Blocks we have snapshots for, in ascending order. Must be non-empty.
	snapshotBlocks []uint64,
	// The target block that the system needs to be able to roll back to.
	rollbackTarget uint64,
) (floor uint64) {

	floor = snapshotBlocks[0] // guaranteed non-empty
	for _, b := range snapshotBlocks {
		if b > rollbackTarget {
			break
		}
		floor = b
	}
	return floor
}

// blocksAtOrAbove returns the subset of blocks that are >= floor, preserving order. This is the set a snapshot store is
// expected to hold after PruneBelow(floor) completes.
func blocksAtOrAbove(blocks []uint64, floor uint64) []uint64 {
	result := make([]uint64, 0, len(blocks))
	for _, b := range blocks {
		if b >= floor {
			result = append(result, b)
		}
	}
	return result
}

// blockRange renders an inclusive block range for logging.
func blockRange(start uint64, end uint64) string {
	return fmt.Sprintf("[%d, %d]", start, end)
}

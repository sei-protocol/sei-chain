package storagemanager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("db", "storagemanager")

// StorageManager manages deletion of stored data across a set of state stores and the StateWAL.
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

	// The stores whose data can be rebuilt by replaying the state WAL.
	stateStores []SnapshotStore

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
	stateStores []SnapshotStore,
	stateWAL StreamStore,
) (*StorageManager, error) {

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid storage manager config: %w", err)
	}

	s := &StorageManager{
		config:      config,
		stateStores: stateStores,
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

// run periodically drives prune cycles until the manager is stopped. All decision logic lives in prune so it can
// be unit tested without threading.
func (s *StorageManager) run() {
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
			if err := prune(s.config.RollbackWindow, s.stateStores, s.stateWAL); err != nil {
				logger.Error("prune cycle failed", "err", err)
			}
		}
	}
}

// prune performs a single prune cycle: it observes the blocks retained by each managed store, computes how far each
// store may prune while preserving the rollback window, and issues the prune commands.
func prune(
	rollbackWindow uint64,
	stateStores []SnapshotStore,
	stateWAL StreamStore,
) error {
	stateWALStart, stateWALEnd, stateWALHasData, err := stateWAL.GetStoredBlocks()
	if err != nil {
		return fmt.Errorf("failed to read stored blocks from %s: %w", stateWAL.Name(), err)
	}

	stateStoreBlocks := make([][]uint64, len(stateStores))
	anyStoreEmpty := false
	for i, store := range stateStores {
		blocks, err := store.GetStoredBlocks()
		if err != nil {
			return fmt.Errorf("failed to read stored blocks from %s: %w", store.Name(), err)
		}
		stateStoreBlocks[i] = blocks
		if len(blocks) == 0 {
			anyStoreEmpty = true
		}
	}

	if !stateWALHasData || anyStoreEmpty {
		// We only prune if the WAL and every provided store has at least some data. An empty store is treated as
		// unknown rather than empty (a snapshot may be mid-write and take hours), and pruning against it would risk
		// breaking the primary invariant (i.e. breaking the ability to roll back by the act of deletion).
		logArgs := []any{"stateWAL", blockRange(stateWALStart, stateWALEnd), "stateWALHasData", stateWALHasData}
		for i, store := range stateStores {
			logArgs = append(logArgs, store.Name(), stateStoreBlocks[i])
		}
		logger.Info("skipping pruning, not all stores have data", logArgs...)
		return nil
	}

	// The latest committed block is defined by the head of the state WAL.
	latestBlock := stateWALEnd

	// The oldest block we must remain able to roll back to.
	var oldestBlockNeeded uint64
	if latestBlock > rollbackWindow {
		oldestBlockNeeded = latestBlock - rollbackWindow
	}

	floors := make([]uint64, len(stateStores))
	for i, blocks := range stateStoreBlocks {
		floors[i] = snapshotPruningFloor(blocks, oldestBlockNeeded)
	}

	// The WAL retains from the oldest block any state store still needs. With no state stores, nothing depends on the
	// WAL for rollback, so it retains only the rollback window.
	stateWALFloor := oldestBlockNeeded
	for i, floor := range floors {
		if i == 0 || floor < stateWALFloor {
			stateWALFloor = floor
		}
	}

	logArgs := []any{"latestBlock", latestBlock, "oldestBlockNeeded", oldestBlockNeeded}
	for i, store := range stateStores {
		logArgs = append(logArgs,
			store.Name()+"Initial", stateStoreBlocks[i],
			store.Name()+"Final", blocksAtOrAbove(stateStoreBlocks[i], floors[i]),
		)
	}
	logArgs = append(logArgs,
		"stateWALInitial", blockRange(stateWALStart, stateWALEnd),
		"stateWALFinal", blockRange(stateWALFloor, stateWALEnd),
	)
	logger.Info("pruning storage", logArgs...)

	for i, store := range stateStores {
		if err := store.PruneBelow(floors[i]); err != nil {
			return fmt.Errorf("failed to prune %s below %d: %w", store.Name(), floors[i], err)
		}
	}
	if err := stateWAL.PruneBelow(stateWALFloor); err != nil {
		return fmt.Errorf("failed to prune %s below %d: %w", stateWAL.Name(), stateWALFloor, err)
	}

	return nil
}

// Given a list of snapshot block numbers, determine the lowest snapshot we need to keep in order to be able
// to roll back to the target block number.
//
// Returns the highest numbered block from snapshotBlocks that is less than or equal to the rollbackTarget. If every
// snapshot is greater than rollbackTarget, the lowest snapshot is returned, since none can be safely pruned.
// snapshotBlocks must be non-empty and sorted in ascending order, per the SnapshotStore contract.
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

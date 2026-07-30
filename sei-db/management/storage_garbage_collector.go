package management

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("db", "management")

// StorageGarbageCollector manages deletion of stored data across a set of stores. Each store declares its
// StoreType, which determines both how the collector observes what the store retains and how far the store is
// allowed to prune: a SnapshotStore retains whole snapshots, while a StreamStore (e.g. the state WAL) retains a
// contiguous range of blocks that the snapshot stores are replayed from.
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

	for _, store := range stores {
		switch storeType := store.GetStoreType(); storeType {
		case SnapshotStore, StreamStore:
		default:
			return nil, fmt.Errorf("store %s has unsupported store type %s", store.Name(), storeType)
		}
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
			if err := prune(s.config.RollbackWindow, s.stores); err != nil {
				logger.Error("prune cycle failed", "err", err)
			}
		}
	}
}

// prune performs a single prune cycle: it observes the blocks retained by each managed store, computes how far each
// store may prune while preserving the rollback window, and issues the prune commands.
func prune(rollbackWindow uint64, stores []PrunableStore) error {
	observations, err := observeStores(stores)
	if err != nil {
		return err
	}

	if len(observations) == 0 {
		return nil
	}

	allStoresHaveData := true
	for _, obs := range observations {
		if !obs.hasData {
			allStoresHaveData = false
		}
	}

	if !allStoresHaveData {
		// We only prune if every provided store has at least some data. An empty store is treated as unknown rather
		// than empty (a snapshot may be mid-write and take hours), and pruning against it would risk breaking the
		// primary invariant (i.e. breaking the ability to roll back by the act of deletion).
		logArgs := make([]any, 0, 2*len(observations))
		for _, obs := range observations {
			logArgs = append(logArgs, obs.store.Name(), obs.describeRetained())
		}
		logger.Info("skipping pruning, not all stores have data", logArgs...)
		return nil
	}

	latestBlock, highestBlock := committedBlockRange(observations)

	// The lowest head sets the rollback target for every store, so a store trailing the rest by more than the whole
	// rollback window is throttling how much anything else can prune. That errs toward retention and is safe, but it
	// usually means the store is stalled or is reporting the wrong height, and it is otherwise invisible: pruning
	// still looks like it is working, it just stops reclaiming much.
	if highestBlock-latestBlock > rollbackWindow {
		logArgs := make([]any, 0, 2+2*len(observations))
		logArgs = append(logArgs, "rollbackWindow", rollbackWindow)
		for _, obs := range observations {
			logArgs = append(logArgs, obs.store.Name()+"Head", obs.latestBlock)
		}
		logger.Warn("store heads disagree by more than the rollback window, pruning is limited by the "+
			"furthest behind store", logArgs...)
	}

	// The oldest block we must remain able to roll back to.
	var oldestBlockNeeded uint64
	if latestBlock > rollbackWindow {
		oldestBlockNeeded = latestBlock - rollbackWindow
	}

	floors := pruningFloors(observations, oldestBlockNeeded)

	logArgs := []any{"latestBlock", latestBlock, "oldestBlockNeeded", oldestBlockNeeded}
	for i, obs := range observations {
		logArgs = append(logArgs,
			obs.store.Name()+"Initial", obs.describeRetained(),
			obs.store.Name()+"Final", obs.describeRetainedAfterPruning(floors[i]),
		)
	}
	logger.Info("pruning storage", logArgs...)

	for i, obs := range observations {
		if err := obs.store.PruneBelow(floors[i]); err != nil {
			return fmt.Errorf("failed to prune %s below %d: %w", obs.store.Name(), floors[i], err)
		}
	}

	return nil
}

// observation is what a single prune cycle read back from one store.
type observation struct {
	store     PrunableStore
	storeType StoreType

	// The snapshots held by a SnapshotStore, ascending. Always empty for a StreamStore.
	snapshots []uint64

	// The inclusive range of blocks held by a StreamStore. Meaningful only when hasData is true.
	start uint64
	end   uint64

	// The highest block the store has ingested. Meaningful only when hasData is true.
	latestBlock uint64

	// Whether the store holds any blocks at all.
	hasData bool
}

// observeStores reads what every store currently retains, using the accessor that matches each store's type.
// Observation happens up front so that a read failure aborts the cycle before anything has been deleted.
func observeStores(stores []PrunableStore) ([]observation, error) {
	observations := make([]observation, len(stores))
	for i, store := range stores {
		obs := observation{store: store, storeType: store.GetStoreType()}

		switch obs.storeType {
		case SnapshotStore:
			snapshots, err := store.GetStoredSnapshots()
			if err != nil {
				return nil, fmt.Errorf("failed to read stored snapshots from %s: %w", store.Name(), err)
			}
			obs.snapshots = snapshots
			obs.hasData = len(snapshots) > 0
		case StreamStore:
			start, end, hasData, err := store.GetBlockRange()
			if err != nil {
				return nil, fmt.Errorf("failed to read block range from %s: %w", store.Name(), err)
			}
			obs.start, obs.end, obs.hasData = start, end, hasData
		default:
			return nil, fmt.Errorf("store %s has unsupported store type %s", store.Name(), obs.storeType)
		}

		latestBlock, err := store.GetLastCommittedBlock()
		if err != nil {
			return nil, fmt.Errorf("failed to read last committed block from %s: %w", store.Name(), err)
		}
		obs.latestBlock = latestBlock

		observations[i] = obs
	}
	return observations, nil
}

// committedBlockRange returns the lowest and highest heads reported across the stores.
//
// The lowest head is the head of the chain as the collector understands it, because it bounds what the system can
// actually serve; treating a lagging store as caught up would place the rollback target above the blocks that store
// still holds. The highest is reported alongside it so callers can see how far the stores disagree. Callers must have
// verified that there is at least one store and that every store has data.
func committedBlockRange(observations []observation) (lowest uint64, highest uint64) {
	for i, obs := range observations {
		if i == 0 || obs.latestBlock < lowest {
			lowest = obs.latestBlock
		}
		if i == 0 || obs.latestBlock > highest {
			highest = obs.latestBlock
		}
	}
	return lowest, highest
}

// pruningFloors computes the block each store may prune below, indexed in parallel with observations.
//
// A snapshot store retains the newest snapshot at or below oldestBlockNeeded, since that is the snapshot a rollback to
// oldestBlockNeeded would start from. Every stream store retains from the oldest block any snapshot store still needs,
// because a rollback replays the stream forward from that snapshot. With no snapshot stores nothing depends on the
// streams for rollback, so they retain only the rollback window.
//
// A stream store must not simply retain from oldestBlockNeeded: snapshots land at arbitrary heights, so the newest
// snapshot at or below oldestBlockNeeded can sit far below it. With snapshots at 80,000 and 92,000 and
// oldestBlockNeeded of 90,000, the 80,000 snapshot is the only one a rollback to 90,000 can start from, and replaying
// forward to 90,000 needs stream entries 80,001 onward. Pruning the stream to 90,000 would strand that snapshot.
func pruningFloors(observations []observation, oldestBlockNeeded uint64) []uint64 {
	floors := make([]uint64, len(observations))

	streamFloor := oldestBlockNeeded
	seenSnapshotStore := false
	for i, obs := range observations {
		if obs.storeType != SnapshotStore {
			continue
		}
		floors[i] = snapshotPruningFloor(obs.snapshots, oldestBlockNeeded)
		if !seenSnapshotStore || floors[i] < streamFloor {
			streamFloor = floors[i]
		}
		seenSnapshotStore = true
	}

	for i, obs := range observations {
		if obs.storeType == StreamStore {
			floors[i] = streamFloor
		}
	}

	return floors
}

// describeRetained renders what the store currently holds, for logging.
func (o observation) describeRetained() string {
	if !o.hasData {
		return "empty"
	}
	if o.storeType == SnapshotStore {
		return fmt.Sprintf("%v", o.snapshots)
	}
	return blockRange(o.start, o.end)
}

// describeRetainedAfterPruning renders what the store is expected to hold once PruneBelow(floor) completes.
func (o observation) describeRetainedAfterPruning(floor uint64) string {
	if o.storeType == SnapshotStore {
		return fmt.Sprintf("%v", blocksAtOrAbove(o.snapshots, floor))
	}
	return blockRange(floor, o.end)
}

// Given a list of snapshot block numbers, determine the lowest snapshot we need to keep in order to be able
// to roll back to the target block number.
//
// Returns the highest numbered block from snapshotBlocks that is less than or equal to the rollbackTarget. If every
// snapshot is greater than rollbackTarget, the lowest snapshot is returned, since none can be safely pruned.
// snapshotBlocks must be non-empty and sorted in ascending order, per the PrunableStore contract.
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

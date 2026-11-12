package flatkv

import (
	"errors"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/sview"
	"go.opentelemetry.io/otel/metric"
)

// commitTask is the result of committing one block to one store.
type commitTask struct {
	// The store this result is for.
	store view.ViewManager
	// A view of the store after committing the mutations in the block. During replay, may be a view from a future
	// block if this store is currently ahead of the block being committed.
	view view.View
	// True if this view was created as a result of this commit. May be false during replay.
	newlySealed bool
	// Set if the commit was unsuccessful.
	err error
}

// CommitStateChanges writes the block's mutations and commits them at blockNum.
func (s *CommitStore) CommitStateChanges(blockNum int64, mutations []mutation.Mutation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.commitMutations(uint64(blockNum), mutations, false); err != nil { //nolint:gosec // non-negative
		return fmt.Errorf("CommitStateChanges: commit version %d: %w", blockNum, err)
	}
	return nil
}

// commitReplayedStateChanges commits a block read back from the state WAL at blockHeight, as CommitStateChanges
// does, except that a DB which already holds the block is left as it is.
func (s *CommitStore) commitReplayedStateChanges(blockHeight uint64, mutations []mutation.Mutation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.commitMutations(blockHeight, mutations, true); err != nil {
		return fmt.Errorf("commit replayed block %d: %w", blockHeight, err)
	}
	return nil
}

// Commit commits the block ApplyChangeSets buffered at version, or an empty block if nothing is buffered, and
// returns version. version must be the committed version plus one; committing the committed version again does
// nothing.
func (s *CommitStore) Commit(version int64) (int64, error) {
	// TODO(concurrency): This takes a single coarse write lock for the whole
	// commit, so it also blocks readers/iterator construction during the WAL
	// fsync and the periodic auto-snapshot. Views are unaffected, since
	// OpenView reads do not take this lock, but a caller on Get or Iterator
	// waits out the whole commit. Commits are now pipelined behind the next
	// block's execution, so that wait is on the execution path: replace this
	// with a finer-grained scheme.
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.readOnly {
		return version, errReadOnly
	}

	// Committing a block that is already committed does nothing and reports success. Composite's
	// flatKVWorkingHash seals the pending block in order to hash it, and rootmulti then commits that
	// same block, so the second call has nothing left to do.
	//
	// Post-Cosmos this goes away: one call will supply a block's writes and commit them.
	if version > 0 && version == s.committedVersion {
		return version, nil
	}

	blockHeight := uint64(version) //nolint:gosec // non-negative
	mutations, err := s.pendingMutations(blockHeight)
	if err != nil {
		return version, fmt.Errorf("Commit: read block %d's pending changes: %w", blockHeight, err)
	}
	if err := s.commitMutations(blockHeight, mutations, false); err != nil {
		return version, fmt.Errorf("Commit: commit block %d: %w", blockHeight, err)
	}
	return version, nil
}

// pendingMutations converts the changesets ApplyChangeSets buffered into the mutations of block blockHeight. The
// caller holds mu.
func (s *CommitStore) pendingMutations(blockHeight uint64) ([]mutation.Mutation, error) {
	// Ahead of commitMutations' own check, because the conversion reads the state of the block before this one.
	if err := s.requireExpectedBlockNumber(blockHeight, false); err != nil {
		return nil, fmt.Errorf("check block %d before converting it: %w", blockHeight, err)
	}
	s.phaseTimer.SetPhase("commit_convert_mutations")
	// The last sealed block is the one before this one, which commitMutations has yet to seal.
	prior := s.OpenView()
	defer prior.Close()
	mutations, err := MutationsFromChangeSets(s.pendingChangeSets, blockHeight, prior)
	if err != nil {
		return nil, fmt.Errorf("convert block %d to mutations: %w", blockHeight, err)
	}
	return mutations, nil
}

// commitMutations writes and commits one block's mutations at blockHeight, which must be the committed version
// plus one. The caller holds mu.
func (s *CommitStore) commitMutations(
	blockHeight uint64,
	mutations []mutation.Mutation,
	// Whether the block is being replayed.
	replay bool,
) (err error) {
	start := time.Now()
	defer func() {
		otelMetrics.CommitLatency.Record(s.ctx, secondsSince(start),
			metric.WithAttributes(successAttr(err)))
		if err != nil && !errors.Is(err, errReadOnly) {
			logger.Error("FlatKV Commit failed",
				"version", blockHeight,
				"mutations", len(mutations),
				"elapsed", time.Since(start),
				"err", err)
		}
	}()

	if s.readOnly {
		return errReadOnly
	}
	if err := s.requireExpectedBlockNumber(blockHeight, replay); err != nil {
		return fmt.Errorf("check block %d before committing it: %w", blockHeight, err)
	}

	// Step 1: Stage the block into the stores. Every key is routed before anything is written, so a malformed
	// block fails here, before the WAL records it.
	staged, err := s.stageMutations(blockHeight, mutations)
	if err != nil {
		return fmt.Errorf("stage block %d: %w", blockHeight, err)
	}

	// Step 2: Write the WAL (source of truth) before the seal, so crash recovery via catchup stays valid.
	// Write records this block's mutations and Flush makes the record durable. An empty block writes an empty
	// but contiguous record. Skipped entirely when the WAL is nil — the outer context then owns the WAL pipeline.
	if s.wal != nil {
		s.phaseTimer.SetPhase("commit_write_wal")
		if err := s.wal.Write(blockHeight, mutations); err != nil {
			return fmt.Errorf("WAL write: %w", err)
		}
		if err := s.wal.Flush(); err != nil {
			return fmt.Errorf("WAL flush: %w", err)
		}
	}

	// Step 3: Seal the block on every store, hash it, and carry each database's metadata down with its
	// diff. The stores flush to Pebble asynchronously from here; the WAL (Step 2) remains the source of
	// truth for anything that has not landed yet, so a restart self-heals via catchup.
	if err := s.sealBlock(blockHeight, staged, replay); err != nil {
		return fmt.Errorf("seal block: %w", err)
	}

	// Step 4: Update in-memory committed state, only once every store accepted the seal. The block's
	// hash is not part of this: it is computed and recorded asynchronously, and handed to the
	// registered listeners once it is.
	s.committedVersion = int64(blockHeight) //nolint:gosec // never near the int64 ceiling

	// Step 5: Clear per-block bookkeeping
	s.clearPendingBlock()

	// Step 6: Offer the block to the snapshot writer, which decides whether it becomes a snapshot and,
	// if so, writes it on its own goroutine. Periodic snapshots are what keep the WAL bounded and
	// restarts fast.
	if s.snapshotWriter != nil {
		s.phaseTimer.SetPhase("commit_offer_snapshot")
		if err := s.offerToSnapshotWriter(); err != nil {
			return fmt.Errorf("auto snapshot at block %d: %w", blockHeight, err)
		}
	}

	// Best-effort WAL truncation, throttled to amortize ReadDir cost.
	if blockHeight%1000 == 0 {
		s.tryTruncateWAL()
	}

	// Ends the commit rather than opening a phase to sit in, so that these phases cover the commits
	// themselves and not the gaps between them.
	s.phaseTimer.Reset()
	otelMetrics.CurrentVersion.Record(s.ctx, int64(blockHeight)) //nolint:gosec // never near the int64 ceiling
	return nil
}

// requireExpectedBlockNumber returns an error unless blockHeight is the block this store expects next: the block
// after the committed version, and each data DB's next block.
func (s *CommitStore) requireExpectedBlockNumber(
	blockHeight uint64,
	// Whether the block is being replayed.
	replay bool,
) error {
	committedHeight := uint64(s.committedVersion) //nolint:gosec // committed versions are non-negative
	// Blocks are contiguous and the first block is 1, so the next commit is always committedVersion+1. On a
	// fresh store that means block 1; a store whose history starts higher gets there via SetInitialVersion,
	// which seeds committedVersion to one below its first block.
	if blockHeight != committedHeight+1 {
		return fmt.Errorf("flatkv: committing bad version: got %d, want %d (current %d)",
			blockHeight, committedHeight+1, committedHeight)
	}
	for _, store := range s.stores {
		nextBlockHeight := store.NextBlockHeight()
		if nextBlockHeight == blockHeight || (replay && nextBlockHeight > blockHeight) {
			continue
		}
		return fmt.Errorf("flatkv: cannot commit block %d: the %s DB's next block is %d",
			blockHeight, store.Name(), nextBlockHeight)
	}
	return nil
}

// FlushSnapshots blocks until no snapshot is being written. It is a synchronization point for callers
// that need the snapshot tree on disk to have caught up with the blocks committed so far; block
// commit does not need it.
func (s *CommitStore) FlushSnapshots() error {
	if s.snapshotWriter == nil {
		return nil
	}
	return s.snapshotWriter.Flush()
}

// clearPendingBlock resets the per-block bookkeeping that Commit consumed.
func (s *CommitStore) clearPendingBlock() {
	s.pendingChangeSets = make([]*proto.NamedChangeSet, 0, len(s.pendingChangeSets))
	s.pendingBlockHeight = 0
}

// sealBlock marks the block as closed for new writes and hands it to the hashing pipeline.
func (s *CommitStore) sealBlock(
	blockHeight uint64,
	// The block's mutations grouped by database, as stageMutations returned them.
	staged []lthash.DatabaseMutations,
	// Whether the block is being replayed.
	replay bool,
) (err error) {
	var storeView *sview.StoreView
	defer func() {
		if err != nil {
			// Error is fatal; the reservation is given up rather than released.
			storeView.Abandon()
		}
	}()

	s.phaseTimer.SetPhase("commit_seal_stores")

	var viewsToFinalize []view.View
	storeView, viewsToFinalize, err = s.commitStores(blockHeight, replay)
	if err != nil {
		return fmt.Errorf("seal the stores: %w", err)
	}

	if err := s.lastSealed.Set(storeView); err != nil {
		return fmt.Errorf("install block %d: %w", blockHeight, err)
	}

	s.phaseTimer.SetPhase("commit_offer_finalization")
	if err := s.finalizer.Offer(blockHeight, storeView, viewsToFinalize); err != nil {
		return fmt.Errorf("hand block %d to the finalizer: %w", blockHeight, err)
	}

	s.phaseTimer.SetPhase("commit_schedule_hash")
	//nolint:gosec // never near the int64 ceiling
	if err := s.hashEngine.ScheduleHash(int64(blockHeight), staged); err != nil {
		return fmt.Errorf("schedule block %d's hash: %w", blockHeight, err)
	}

	if err := storeView.Release(); err != nil {
		return fmt.Errorf("release block %d's view: %w", blockHeight, err)
	}
	return nil
}

// commitStores() seals block blockHeight on every store as one view, and returns it with the views sealed for the
// block. During replay a store that already holds the block contributes the view it has instead of sealing a new
// one. The returned view carries a reservation on each store's view, and the caller owns it.
func (s *CommitStore) commitStores(
	blockHeight uint64,
	// Whether the block is being replayed.
	replay bool,
) (*sview.StoreView, []view.View, error) {
	commitTasks := make(chan commitTask, len(s.stores))
	for _, store := range s.stores {
		s.miscPool.Submit(func() {
			commitTasks <- s.commitStore(store, blockHeight, replay)
		})
	}

	viewsByStore := make(map[view.ViewManager]view.View, len(s.stores))
	viewsToFinalize := make([]view.View, 0, len(s.stores))
	var errs []error
	for range s.stores {
		task := <-commitTasks
		if task.err != nil {
			errs = append(errs, task.err)
			continue
		}
		viewsByStore[task.store] = task.view
		if task.newlySealed {
			viewsToFinalize = append(viewsToFinalize, task.view)
		}
	}
	if err := errors.Join(errs...); err != nil {
		for _, dbView := range viewsByStore {
			dbView.Abandon()
		}
		return nil, nil, fmt.Errorf("seal block %d: %w", blockHeight, err)
	}

	//nolint:gosec // never near the int64 ceiling
	storeView, err := sview.NewStoreView(int64(blockHeight),
		viewsByStore[s.accountStore],
		viewsByStore[s.codeStore],
		viewsByStore[s.storageStore],
		viewsByStore[s.miscStore])
	if err != nil {
		return nil, nil, fmt.Errorf("build the view of block %d: %w", blockHeight, err)
	}
	return storeView, viewsToFinalize, nil
}

// commitStore() commits block blockHeight to store.
func (s *CommitStore) commitStore(
	store view.ViewManager,
	blockHeight uint64,
	// Whether the block is being replayed.
	replay bool,
) commitTask {
	nextBlockHeight := store.NextBlockHeight()
	if nextBlockHeight < blockHeight || (nextBlockHeight > blockHeight && !replay) {
		return commitTask{store: store, err: fmt.Errorf("cannot seal block %d: the %s DB's next block is %d",
			blockHeight, store.Name(), nextBlockHeight)}
	}
	if nextBlockHeight > blockHeight {
		// Replaying a block this DB already holds: it keeps the view it has.
		latestView, err := store.LatestView()
		if err != nil {
			return commitTask{store: store, err: fmt.Errorf("keep %s's view for block %d: %w",
				store.Name(), blockHeight, err)}
		}
		return commitTask{store: store, view: latestView}
	}

	start := time.Now()
	newView, err := store.Commit(blockHeight)
	otelMetrics.CommitBatchLatency.Record(s.ctx, secondsSince(start),
		metric.WithAttributes(dbAttr(store.Name()), successAttr(err)))
	if err != nil {
		return commitTask{store: store, err: fmt.Errorf("seal %s at block %d: %w",
			store.Name(), blockHeight, err)}
	}
	return commitTask{store: store, view: newView, newlySealed: true}
}

// offerToSnapshotWriter() hands the most recently committed block to the writer, which decides whether
// it becomes a snapshot. The writer takes its own reservation, so this one lasts only for the call.
func (s *CommitStore) offerToSnapshotWriter() error {
	blockView, err := s.lastSealed.Get()
	if err != nil {
		return fmt.Errorf("read latest sealed view: %w", err)
	}
	if err := s.snapshotWriter.Offer(blockView); err != nil {
		// Error is fatal; the reservation is given up rather than released.
		blockView.Abandon()
		return err
	}
	if err := blockView.Release(); err != nil {
		return fmt.Errorf("release latest sealed view: %w", err)
	}
	return nil
}

// replaceSealedView() installs blockView, discarding whatever was installed before. The startup
// lifecycle seals use it because they may install a block no later than the current one, which set()
// refuses. The caller keeps its own reservations.
func (s *CommitStore) replaceSealedView(blockView *sview.StoreView) error {
	if s.lastSealed != nil {
		if err := s.lastSealed.Close(); err != nil {
			// Error is fatal; leaking reservations doesn't make it worse.
			return fmt.Errorf("retire previous sealed view: %w", err)
		}
		s.lastSealed = nil
	}

	installed, err := sview.NewAtomicStoreView(blockView)
	if err != nil {
		return fmt.Errorf("install sealed view at height %d: %w", blockView.BlockHeight(), err)
	}
	s.lastSealed = installed
	return nil
}

// flushLatestVersion blocks until the most recently committed block has been flushed down to all five
// pebble instances. It does not start that flush; the stores are already doing it in the background,
// and this waits for them to finish.
//
// Since we continue to hold the reservation on that block, later blocks are prevented from being flushed
// down to pebble. So on return the pebble instances hold exactly the most recently committed block, and
// stay there until the reservation is released — which is what anyone reading the databases directly,
// rather than through the stores, depends on.
func (s *CommitStore) flushLatestVersion() error {
	blockView, err := s.lastSealed.Get()
	if err != nil {
		return fmt.Errorf("read latest sealed view: %w", err)
	}
	if err := blockView.AwaitFlush(s.ctx); err != nil {
		// Error is fatal; the reservation is given up rather than released.
		blockView.Abandon()
		return fmt.Errorf("await flush: %w", err)
	}
	if err := blockView.Release(); err != nil {
		return fmt.Errorf("release latest sealed view: %w", err)
	}
	return nil
}

// finalizeStore finalizes one store's sealed block, recording the LocalMeta that describes it.
//
// Finalizing with an empty write set still makes the sealed version flushable, which is the only thing
// finalization is required to do.
func finalizeStore(
	viewToFinalize view.View,
	blockHeight uint64,
	// The block's hashes, which this store's own entry is read out of.
	hashes *lthash.BlockHash,
) error {
	writes, err := encodeLocalMeta(
		int64(blockHeight), //nolint:gosec // never near the int64 ceiling
		hashes.PerDB[viewToFinalize.Name()],
		hashes.PerModule[viewToFinalize.Name()],
		hashes.PerModuleStats[viewToFinalize.Name()],
	)
	if err != nil {
		return fmt.Errorf("encode %s local meta at block %d: %w", viewToFinalize.Name(), blockHeight, err)
	}
	if err := viewToFinalize.Finalize(blockHeight, writes); err != nil {
		return fmt.Errorf("finalize view at block %d: %w", blockHeight, err)
	}
	return nil
}

// rawKVPair is a raw physical key/value pair as stored on disk.
type rawKVPair struct {
	Key   []byte
	Value []byte
}

// FinalizeImport persists each data DB's metadata (version + LtHash) after all
// import data has been written, and reopens the store at that version.
// This must be called exactly once at the end of an import to make the data
// durable across restarts.
func (s *CommitStore) FinalizeImport(version int64) error {
	if err := s.persistLocalMetaAndReopen(uint64(version)); err != nil { //nolint:gosec // non-negative
		return fmt.Errorf("after import: %w", err)
	}
	return nil
}

// persistLocalMetaAndReopen writes, durably and straight to each data DB, metadata recording blockHeight as the
// height the DB has reached and loadedHashes as its hashes, then closes and reopens the store on it.
func (s *CommitStore) persistLocalMetaAndReopen(blockHeight uint64) error {
	syncOpt := types.WriteOptions{Sync: true}
	for _, dir := range dataDBDirs {
		batch := s.rawDBFor(dir).NewBatch()
		//nolint:gosec // never near the int64 ceiling
		err := writeLocalMetaToBatch(batch, int64(blockHeight), s.loadedHashes.PerDB[dir],
			s.loadedHashes.PerModule[dir], s.loadedHashes.PerModuleStats[dir])
		if err != nil {
			_ = batch.Close()
			return fmt.Errorf("%s local meta: %w", dir, err)
		}
		if err := batch.Commit(syncOpt); err != nil {
			_ = batch.Close()
			return fmt.Errorf("%s commit: %w", dir, err)
		}
		_ = batch.Close()
	}

	// The stores count blocks from the height they were opened at, so a store whose height has jumped is
	// reopened rather than sealed at the new height.
	if err := s.closeDBsOnly(); err != nil {
		return fmt.Errorf("close before reopening at block %d: %w", blockHeight, err)
	}
	if err := s.open(); err != nil {
		return fmt.Errorf("reopen at block %d: %w", blockHeight, err)
	}
	return nil
}

// sealBaseline seals a data-free version on every store at the height its database holds, and installs
// them as the sealed view at the store's committed version, so that a view of the current block always
// exists.
func (s *CommitStore) sealBaseline() (err error) {
	viewsByStore := make(map[view.ViewManager]view.View, len(s.stores))
	defer func() {
		if err != nil {
			// Error is fatal; the reservations are given up rather than released.
			for _, dbView := range viewsByStore {
				dbView.Abandon()
			}
		}
	}()

	for _, store := range s.stores {
		height := uint64(s.localMeta[store.Name()].CommittedVersion) //nolint:gosec // non-negative
		baselineView, err := store.Commit(height)
		if err != nil {
			return fmt.Errorf("seal %s's baseline at block %d: %w", store.Name(), height, err)
		}
		viewsByStore[store] = baselineView
		if err := baselineView.Finalize(height, nil); err != nil {
			return fmt.Errorf("finalize %s's baseline at block %d: %w", store.Name(), height, err)
		}
	}

	storeView, err := sview.NewStoreView(s.committedVersion,
		viewsByStore[s.accountStore],
		viewsByStore[s.codeStore],
		viewsByStore[s.storageStore],
		viewsByStore[s.miscStore])
	if err != nil {
		return fmt.Errorf("build the baseline view: %w", err)
	}
	if err := s.replaceSealedView(storeView); err != nil {
		return fmt.Errorf("install baseline: %w", err)
	}
	if err := storeView.Release(); err != nil {
		return fmt.Errorf("release baseline reservations: %w", err)
	}
	return nil
}

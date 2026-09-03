package flatkv

import (
	"errors"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/sview"
	"go.opentelemetry.io/otel/metric"
)

func (s *CommitStore) CommitStateChanges(blockNum int64, changeset []*proto.NamedChangeSet) error {
	if err := s.ApplyChangeSets(blockNum, changeset); err != nil {
		return fmt.Errorf("CommitStateChanges: apply version %d: %w", blockNum, err)
	}
	if _, err := s.Commit(blockNum); err != nil {
		return fmt.Errorf("CommitStateChanges: commit version %d: %w", blockNum, err)
	}
	return nil
}

// Commit persists buffered writes at the given version (block height). One Commit persists exactly one
// block; version must equal the height the pending writes were stamped with. Consecutive commits must also
// be contiguous: the state WAL rejects a version that skips a height, though the first block written to an
// empty WAL may be any height.
// Protocol: WAL → seal on every store, each one's LocalMeta finalized into the same batch as its diff
// → asynchronous flush. On crash, catchup replays WAL to recover incomplete commits.
func (s *CommitStore) Commit(version int64) (committed int64, err error) {
	start := time.Now()

	// TODO(concurrency): This takes a single coarse write lock for the whole
	// commit, so it also blocks readers/iterator construction during the WAL
	// fsync and the periodic auto-snapshot. That is fine today because commits
	// are not pipelined with reads (there is currently no pipelining at all).
	// When commit pipelining is introduced, replace this with a finer-grained
	// scheme.
	s.mu.Lock()
	defer s.mu.Unlock()

	// Committing a block that is already committed does nothing and reports success. Composite's
	// flatKVWorkingHash seals the pending block in order to hash it, and rootmulti then commits that
	// same block, so the second call has nothing left to do.
	//
	// Post-Cosmos this goes away: one call will supply a block's writes and commit them.
	if !s.readOnly && version > 0 && version == s.committedVersion {
		return version, nil
	}

	// Row counts are no longer available here: the staged rows live inside the stores' current
	// version, which does not expose a size. The changeset count is the closest stand-in and is what
	// a failure investigation actually starts from.
	pendingChangeSets := len(s.pendingChangeSets)
	defer func() {
		otelMetrics.CommitLatency.Record(s.ctx, secondsSince(start),
			metric.WithAttributes(successAttr(err)))
		if err != nil && !errors.Is(err, errReadOnly) {
			logger.Error("FlatKV Commit failed",
				"version", version,
				"pendingChangeSets", pendingChangeSets,
				"elapsed", time.Since(start),
				"err", err)
		}
	}()

	if s.readOnly {
		return 0, errReadOnly
	}
	// Blocks are contiguous and the first block is 1, so the next commit is always committedVersion+1. On a
	// fresh store that means block 1; a store whose history starts higher gets there via SetInitialVersion,
	// which seeds committedVersion to one below its first block.
	if version != s.committedVersion+1 {
		return 0, fmt.Errorf("flatkv: committing bad version: got %d, want %d (current %d)",
			version, s.committedVersion+1, s.committedVersion)
	}

	// Step 1: Write the WAL (source of truth) before the DBs, so crash recovery via catchup stays valid.
	// Write buffers this block's changesets, SignalEndOfBlock seals them as one record, and Flush makes the
	// record durable. An empty block (no ApplyChangeSets) writes an empty but contiguous record. Skipped
	// entirely when the WAL is nil — the outer context then owns the WAL pipeline.
	if s.wal != nil {
		s.phaseTimer.SetPhase("commit_write_wal")
		if err := s.wal.Write(uint64(version), s.pendingChangeSets); err != nil { //nolint:gosec // version > committed >= 0
			return version, fmt.Errorf("WAL write: %w", err)
		}
		if err := s.wal.SignalEndOfBlock(); err != nil {
			return version, fmt.Errorf("WAL end of block: %w", err)
		}
		if err := s.wal.Flush(); err != nil {
			return version, fmt.Errorf("WAL flush: %w", err)
		}
	}

	// Step 2: Seal the block on every store, hash it, and carry each database's metadata down with its
	// diff. The stores flush to Pebble asynchronously from here; the WAL (Step 1) remains the source of
	// truth for anything that has not landed yet, so a restart self-heals via catchup.
	if err := s.sealBlock(version, nil); err != nil {
		return version, fmt.Errorf("seal block: %w", err)
	}

	// Step 3: Update in-memory committed state, only once every store accepted the seal. The block's
	// hash is not part of this: it is computed and recorded asynchronously, and read back through
	// PublishedHash or HashChan.
	s.committedVersion = version

	// Step 4: Clear per-block bookkeeping
	s.clearPendingBlock()

	// Step 5: Offer the block to the snapshot writer, which decides whether it becomes a snapshot and,
	// if so, writes it on its own goroutine. Periodic snapshots are what keep the WAL bounded and
	// restarts fast.
	if s.snapshotWriter != nil {
		s.phaseTimer.SetPhase("commit_offer_snapshot")
		if err := s.offerToSnapshotWriter(); err != nil {
			return version, fmt.Errorf("auto snapshot at version %d: %w", version, err)
		}
	}

	// Best-effort WAL truncation, throttled to amortize ReadDir cost.
	if version%1000 == 0 {
		s.tryTruncateWAL()
	}

	s.phaseTimer.SetPhase("commit_done")
	otelMetrics.CurrentVersion.Record(s.ctx, version)
	logger.Info("FlatKV Commit complete",
		"version", version,
		"changeSets", pendingChangeSets,
		"elapsed", time.Since(start))
	return version, nil
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
	version int64,
	// The replay skip list: the height each database had already reached when replay started, or nil
	// outside replay. A database listed at or above version keeps the metadata it already has.
	alreadyHave map[string]int64,
) error {
	s.phaseTimer.SetPhase("commit_seal_stores")

	blockView, err := s.commitStores(version)
	if err != nil {
		return err
	}

	previous, err := s.lastSealed.Get()
	if err != nil {
		// Error is fatal; leaking reservations doesn't make it worse.
		return fmt.Errorf("read previous block's view: %w", err)
	}

	if err := s.lastSealed.Set(blockView); err != nil {
		// Error is fatal; leaking reservations doesn't make it worse.
		return fmt.Errorf("install block %d: %w", version, err)
	}

	s.phaseTimer.SetPhase("commit_offer_finalization")
	if err := s.finalizer.Offer(version, blockView, alreadyHave); err != nil {
		// Error is fatal; leaking reservations doesn't make it worse.
		return err
	}

	s.phaseTimer.SetPhase("commit_schedule_hash")
	if err := s.hashEngine.ScheduleHash(blockView, previous); err != nil {
		// Error is fatal; leaking reservations doesn't make it worse.
		return err
	}

	if err := previous.Release(); err != nil {
		return fmt.Errorf("release previous block's view: %w", err)
	}
	if err := blockView.Release(); err != nil {
		return fmt.Errorf("release block %d's view: %w", version, err)
	}
	return nil
}

// commitStores() seals the current block on every store as one view at version. The returned view
// carries the reservation each store's Commit() handed out, and the caller owns it.
func (s *CommitStore) commitStores(version int64) (*sview.StoreView, error) {
	commit := func(store view.ViewManager) (view.View, error) {
		start := time.Now()
		dbView, err := store.Commit()
		otelMetrics.CommitBatchLatency.Record(s.ctx, secondsSince(start),
			metric.WithAttributes(dbAttr(store.Name()), successAttr(err)))
		if err != nil {
			return nil, fmt.Errorf("%s seal: %w", store.Name(), err)
		}
		return dbView, nil
	}

	account, err := commit(s.accountStore)
	if err != nil {
		return nil, err
	}
	code, err := commit(s.codeStore)
	if err != nil {
		// Error is fatal; leaking reservations doesn't make it worse.
		return nil, err
	}
	storage, err := commit(s.storageStore)
	if err != nil {
		// Error is fatal; leaking reservations doesn't make it worse.
		return nil, err
	}
	misc, err := commit(s.miscStore)
	if err != nil {
		// Error is fatal; leaking reservations doesn't make it worse.
		return nil, err
	}
	return sview.NewStoreView(version, account, code, storage, misc)
}

// offerToSnapshotWriter() hands the most recently committed block to the writer, which decides whether
// it becomes a snapshot. The writer takes its own reservation, so this one lasts only for the call.
func (s *CommitStore) offerToSnapshotWriter() error {
	blockView, err := s.lastSealed.Get()
	if err != nil {
		return fmt.Errorf("read latest sealed view: %w", err)
	}
	if err := s.snapshotWriter.Offer(blockView); err != nil {
		// Error is fatal; leaking reservations doesn't make it worse.
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
		// Error is fatal; leaking reservations doesn't make it worse.
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
	dbView view.View,
	version int64,
	// The replay skip list. A store listed at or above version records nothing: its writes were skipped,
	// so its hash still describes the later height it holds, and writing this block's height alongside
	// that hash would persist a pair that describes no single moment.
	alreadyHave map[string]int64,
	// The block's hashes, which this store's own entry is read out of.
	hashes *lthash.BlockHash,
) error {
	if alreadyHave[dbView.Name()] >= version {
		return dbView.Finalize(nil)
	}

	writes, err := encodeLocalMeta(
		version,
		hashes.PerDB[dbView.Name()],
		hashes.PerModule[dbView.Name()],
		hashes.PerModuleStats[dbView.Name()],
	)
	if err != nil {
		return fmt.Errorf("encode %s local meta at version %d: %w", dbView.Name(), version, err)
	}
	if err := dbView.Finalize(writes); err != nil {
		return fmt.Errorf("finalize view at version %d: %w", version, err)
	}
	return nil
}

// rawKVPair is a raw physical key/value pair as stored on disk.
type rawKVPair struct {
	Key   []byte
	Value []byte
}

// FinalizeImport persists each data DB's metadata (version + LtHash) after all
// import data has been written, and recomputes the store's global state from it.
// This must be called exactly once at the end of an import to make the data
// durable across restarts.
func (s *CommitStore) FinalizeImport(version int64) error {
	syncOpt := types.WriteOptions{Sync: true}
	for _, dir := range dataDBDirs {
		db := s.rawDBFor(dir)
		moduleHashes := s.loadedHashes.PerModule[dir]
		moduleStats := s.loadedHashes.PerModuleStats[dir]
		batch := db.NewBatch()
		err := writeLocalMetaToBatch(batch, version, s.loadedHashes.PerDB[dir], moduleHashes, moduleStats)
		if err != nil {
			_ = batch.Close()
			return fmt.Errorf("%s local meta: %w", dir, err)
		}
		if err := batch.Commit(syncOpt); err != nil {
			_ = batch.Close()
			return fmt.Errorf("%s commit: %w", dir, err)
		}
		_ = batch.Close()
		s.localMeta[dir] = &LocalMeta{
			CommittedVersion: version,
			LtHash:           s.loadedHashes.PerDB[dir].Clone(),
			ModuleLtHashes:   cloneModuleHashes(moduleHashes),
			ModuleStats:      cloneModuleStats(moduleStats),
		}
	}

	s.loadedHashes.Global = lthash.SumDBHashes(dataDBDirs, s.loadedHashes.PerDB)
	s.loadedHashes.BlockNumber = version
	s.committedVersion = version

	// The engine's accumulator described the databases this import has just replaced wholesale, so it is
	// replaced too. Without this the first block committed afterwards would be folded onto state that no
	// longer exists.
	if err := s.restartHashing(); err != nil {
		return fmt.Errorf("after import: %w", err)
	}

	// Imported data goes straight to Pebble, so no view describes it and the sealed view is still the
	// one open() installed. Sealing here is what leaves the store's committed version and its sealed
	// view naming the same block.
	if err := s.sealBaseline(); err != nil {
		return fmt.Errorf("seal imported version: %w", err)
	}
	return nil
}

// sealSeededVersion seals a data-free version on every store, recording seededVersion as the height each
// one has reached.
//
// It is how SetInitialVersion persists a seed. Every write goes through the view manager that owns its
// database, as a block's finalization writes do, so seeding needs no access to the databases themselves.
//
// Releasing the reservation matters as much as the writes: a view must be released before the next one
// can flush, so a seal that kept the baseline's reservation would stall every flush after it, and the
// checkpoint SetInitialVersion takes next would wait forever.
func (s *CommitStore) sealSeededVersion(seededVersion int64) error {
	blockView, err := s.commitStores(seededVersion)
	if err != nil {
		return fmt.Errorf("seal seeded version: %w", err)
	}

	for _, dbView := range blockView.Views() {
		if err := finalizeStore(dbView, seededVersion, nil, s.loadedHashes); err != nil {
			// Error is fatal; leaking reservations doesn't make it worse.
			return fmt.Errorf("%s finalize seeded version: %w", dbView.Name(), err)
		}
	}

	if err := s.replaceSealedView(blockView); err != nil {
		// Error is fatal; leaking reservations doesn't make it worse.
		return fmt.Errorf("install seeded version: %w", err)
	}
	if err := blockView.Release(); err != nil {
		return fmt.Errorf("release seeded version's reservations: %w", err)
	}
	return nil
}

// sealBaseline seals a data-free version on every store at the store's committed version and installs
// it as the sealed view, so that a view of the current block always exists.
func (s *CommitStore) sealBaseline() error {
	blockView, err := s.commitStores(s.committedVersion)
	if err != nil {
		return fmt.Errorf("seal baseline: %w", err)
	}

	for _, dbView := range blockView.Views() {
		if err := dbView.Finalize(nil); err != nil {
			// Error is fatal; leaking reservations doesn't make it worse.
			return fmt.Errorf("%s finalize baseline: %w", dbView.Name(), err)
		}
	}

	if err := s.replaceSealedView(blockView); err != nil {
		// Error is fatal; leaking reservations doesn't make it worse.
		return fmt.Errorf("install baseline: %w", err)
	}
	if err := blockView.Release(); err != nil {
		return fmt.Errorf("release baseline reservations: %w", err)
	}
	return nil
}

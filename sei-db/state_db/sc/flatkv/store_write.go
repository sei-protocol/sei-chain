package flatkv

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"go.opentelemetry.io/otel/metric"
)

// CommitBlock applies changesets and commits them at version in one call.
// Giga-only helper for committing all state changes of a block.
// TODO: make this async and pipelined
func (s *CommitStore) CommitBlock(version int64, changesets []*proto.NamedChangeSet) error {
	if err := s.ApplyChangeSets(version, changesets); err != nil {
		return fmt.Errorf("CommitBlock: apply version %d: %w", version, err)
	}
	if _, err := s.Commit(version); err != nil {
		return fmt.Errorf("CommitBlock: commit version %d: %w", version, err)
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

	// Step 3: Update in-memory committed state, only once every store accepted the seal.
	s.committedVersion = version
	s.committedLtHash = s.workingLtHash.Clone()

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

// sealBlock marks the block as closed for new writes, hashes it, and records each database's metadata.
//
// alreadyHave is the catch-up skip list: the height each store had already reached when replay started,
// or nil outside a replay. A store listed at or above version keeps the metadata it already has, since
// recording this block's height would move that store backwards.
func (s *CommitStore) sealBlock(version int64, alreadyHave map[string]int64) error {
	s.phaseTimer.SetPhase("commit_seal_stores")

	blockView, err := s.commitStores(version)
	if err != nil {
		return err
	}

	previous, err := s.lastSealed.get()
	if err != nil {
		return fmt.Errorf("read previous block's view: %w", err)
	}

	if err := s.hashSealedBlock(blockView, previous); err != nil {
		return fmt.Errorf("hash sealed block: %w", err)
	}
	if err := previous.release(); err != nil {
		return fmt.Errorf("release previous block's reservations: %w", err)
	}

	s.phaseTimer.SetPhase("commit_finalize_stores")
	for _, dbView := range blockView.viewSlice {
		if err := s.finalizeStore(dbView, version, alreadyHave); err != nil {
			return fmt.Errorf("finalize %s: %w", dbView.Name(), err)
		}
	}

	if err := s.lastSealed.set(blockView); err != nil {
		return fmt.Errorf("install block %d: %w", version, err)
	}
	if err := blockView.release(); err != nil {
		return fmt.Errorf("release this block's reservations: %w", err)
	}

	// Adopt the freshly persisted per-DB metadata only once every store has accepted it. A store that
	// kept its own metadata above keeps its in-memory copy too.
	for _, dir := range dataDBDirs {
		if alreadyHave[dir] >= version {
			continue
		}
		s.localMeta[dir] = &ktype.LocalMeta{
			CommittedVersion: version,
			LtHash:           s.perDBWorkingLtHash[dir].Clone(),
			ModuleLtHashes:   cloneModuleHashes(s.perDBModuleWorkingLtHash[dir]),
			ModuleStats:      cloneModuleStats(s.perDBModuleWorkingStats[dir]),
		}
	}
	return nil
}

// commitStores() seals the current block on every store as one view at version. The returned view
// carries the reservation each store's Commit() handed out, and the caller owns it.
func (s *CommitStore) commitStores(version int64) (*storeView, error) {
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
		return nil, err
	}
	storage, err := commit(s.storageStore)
	if err != nil {
		return nil, err
	}
	misc, err := commit(s.miscStore)
	if err != nil {
		return nil, err
	}
	return newStoreView(version, account, code, storage, misc)
}

// hashSealedBlock folds the block that was just sealed into the store's hashes.
//
// The new values are each data store's view diff. The old values are those same keys read back
// from previous, the view of the block before it.
func (s *CommitStore) hashSealedBlock(current *storeView, previous *storeView) error {
	s.phaseTimer.SetPhase("commit_compute_lt_hash")

	changed, err := s.changedValuesByStore(current, previous)
	if err != nil {
		return fmt.Errorf("gather changed values: %w", err)
	}
	res, err := s.ltCalc.Compute(
		changed,
		s.perDBWorkingLtHash,
		s.perDBModuleWorkingLtHash,
		s.perDBModuleWorkingStats)
	if err != nil {
		return fmt.Errorf("compute lt hash: %w", err)
	}

	s.perDBWorkingLtHash = res.PerDB
	s.perDBModuleWorkingLtHash = res.PerModule
	s.perDBModuleWorkingStats = res.PerModuleStats
	s.workingLtHash = res.Global
	return nil
}

// changedValuesByStore returns every key the block changed, with its new value and the value it held
// before, one set per data store.
//
// The stores are read concurrently on the misc pool; each one is an independent view diff followed
// by a batch read of the previous view.
//
// The store-wide root is rebuilt from scratch on every seal — HashCalculator.Compute sums the four
// per-database roots and never mixes in the previous store-wide value.
func (s *CommitStore) changedValuesByStore(current *storeView, previous *storeView) ([]lthash.DBPairs, error) {
	pairs := []struct {
		current  view.View
		previous view.View
	}{
		{current.accountStoreView, previous.accountStoreView},
		{current.codeStoreView, previous.codeStoreView},
		{current.storageStoreView, previous.storageStoreView},
		{current.miscStoreView, previous.miscStoreView},
	}

	changed := make([][]lthash.KVPairWithLastValue, len(pairs))
	errs := make([]error, len(pairs))

	var wg sync.WaitGroup
	for i, pair := range pairs {
		idx, currentView, previousView := i, pair.current, pair.previous
		wg.Add(1)
		s.miscPool.Submit(func() {
			defer wg.Done()
			changed[idx], errs[idx] = changedValues(currentView, previousView)
			if errs[idx] != nil {
				errs[idx] = fmt.Errorf("%s changed values: %w", currentView.Name(), errs[idx])
			}
		})
	}
	wg.Wait()

	out := make([]lthash.DBPairs, 0, len(pairs))
	for i, pair := range pairs {
		if errs[i] != nil {
			return nil, errs[i]
		}
		if len(changed[i]) == 0 {
			continue
		}
		out = append(out, lthash.DBPairs{Dir: pair.current.Name(), Pairs: changed[i]})
	}
	return out, nil
}

// changedValues returns one data store's changed keys, each with its new value and the value it held
// before, from the store's sealed diff and the view preceding it.
//
// A nil value in the diff is a deletion. Keys under the reserved metadata prefix are dropped: they are
// the store's bookkeeping, and folding them in would make the hash depend on its own recorded value.
func changedValues(current view.View, previous view.View) ([]lthash.KVPairWithLastValue, error) {
	diff, err := current.GetDiff()
	if err != nil {
		return nil, fmt.Errorf("read diff: %w", err)
	}
	if len(diff) == 0 {
		return nil, nil
	}

	changedKeys := make([][]byte, 0, len(diff))
	for key := range diff {
		if strings.HasPrefix(key, config.MetaKeyPrefix) {
			continue
		}
		changedKeys = append(changedKeys, []byte(key))
	}
	if len(changedKeys) == 0 {
		return nil, nil
	}

	var old map[string][]byte
	if previous != nil {
		if old, err = previous.BatchGet(changedKeys); err != nil {
			return nil, fmt.Errorf("read previous values: %w", err)
		}
	}

	out := make([]lthash.KVPairWithLastValue, 0, len(changedKeys))
	for _, key := range changedKeys {
		value := diff[string(key)]
		out = append(out, lthash.KVPairWithLastValue{
			Key:       key,
			Value:     value,
			LastValue: old[string(key)],
			Delete:    value == nil,
		})
	}
	return out, nil
}

// offerToSnapshotWriter() hands the most recently committed block to the writer, which decides whether
// it becomes a snapshot. The writer takes its own reservation, so this one lasts only for the call.
func (s *CommitStore) offerToSnapshotWriter() error {
	blockView, err := s.lastSealed.get()
	if err != nil {
		return fmt.Errorf("read latest sealed view: %w", err)
	}
	if err := s.snapshotWriter.Offer(blockView); err != nil {
		return err
	}
	if err := blockView.release(); err != nil {
		return fmt.Errorf("release latest sealed view: %w", err)
	}
	return nil
}

// replaceSealedView() installs blockView, discarding whatever was installed before. The startup
// lifecycle seals use it because they may install a block no later than the current one, which set()
// refuses. The caller keeps its own reservations.
func (s *CommitStore) replaceSealedView(blockView *storeView) error {
	if s.lastSealed != nil {
		if err := s.lastSealed.Close(); err != nil {
			return fmt.Errorf("retire previous sealed view: %w", err)
		}
		s.lastSealed = nil
	}

	installed, err := newAtomicStoreView(blockView)
	if err != nil {
		return fmt.Errorf("install sealed view at height %d: %w", blockView.blockHeight, err)
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
// stay there until the reservation is handed back — which is what anyone reading the databases directly,
// rather than through the stores, depends on.
func (s *CommitStore) flushLatestVersion() error {
	blockView, err := s.lastSealed.get()
	if err != nil {
		return fmt.Errorf("read latest sealed view: %w", err)
	}
	if err := blockView.awaitFlush(s.ctx); err != nil {
		return fmt.Errorf("await flush: %w", err)
	}
	if err := blockView.release(); err != nil {
		return fmt.Errorf("release latest sealed view: %w", err)
	}
	return nil
}

// finalizeStore finalizes one store's sealed block, recording the LocalMeta that describes it.
//
// A store that already reached this height records nothing. Its writes were skipped, so its hash still
// describes the later height it holds; writing this block's height alongside that hash would persist a
// pair that describes no single moment. Finalizing with an empty write set still makes the sealed
// version flushable, which is the only thing finalization is required to do.
func (s *CommitStore) finalizeStore(dbView view.View, version int64, alreadyHave map[string]int64) error {
	if alreadyHave[dbView.Name()] >= version {
		return dbView.Finalize(nil)
	}

	writes, err := encodeLocalMeta(
		version,
		s.perDBWorkingLtHash[dbView.Name()],
		s.perDBModuleWorkingLtHash[dbView.Name()],
		s.perDBModuleWorkingStats[dbView.Name()],
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
		moduleHashes := s.perDBModuleWorkingLtHash[dir]
		moduleStats := s.perDBModuleWorkingStats[dir]
		batch := db.NewBatch()
		err := writeLocalMetaToBatch(batch, version, s.perDBWorkingLtHash[dir], moduleHashes, moduleStats)
		if err != nil {
			_ = batch.Close()
			return fmt.Errorf("%s local meta: %w", dir, err)
		}
		if err := batch.Commit(syncOpt); err != nil {
			_ = batch.Close()
			return fmt.Errorf("%s commit: %w", dir, err)
		}
		_ = batch.Close()
		s.localMeta[dir] = &ktype.LocalMeta{
			CommittedVersion: version,
			LtHash:           s.perDBWorkingLtHash[dir].Clone(),
			ModuleLtHashes:   cloneModuleHashes(moduleHashes),
			ModuleStats:      cloneModuleStats(moduleStats),
		}
	}

	globalHash := lthash.New()
	for _, dir := range dataDBDirs {
		globalHash.MixIn(s.perDBWorkingLtHash[dir])
	}
	s.workingLtHash = globalHash
	s.committedVersion = version
	s.committedLtHash = s.workingLtHash.Clone()
	return nil
}

// sealSeededVersion seals a data-free version on every store, recording seededVersion as the height each
// one has reached.
//
// It is how SetInitialVersion persists a seed. Every write goes through the view manager that owns its
// database, as a block's finalization writes do, so seeding needs no access to the databases themselves.
//
// The reservation hand-back matters as much as the writes: a view must be released before the next one
// can flush, so a seal that kept the baseline's reservation would stall every flush after it, and the
// checkpoint SetInitialVersion takes next would wait forever.
func (s *CommitStore) sealSeededVersion(seededVersion int64) error {
	blockView, err := s.commitStores(seededVersion)
	if err != nil {
		return fmt.Errorf("seal seeded version: %w", err)
	}

	for _, dbView := range blockView.viewSlice {
		if err := s.finalizeStore(dbView, seededVersion, nil); err != nil {
			return fmt.Errorf("%s finalize seeded version: %w", dbView.Name(), err)
		}
	}

	if err := s.replaceSealedView(blockView); err != nil {
		return fmt.Errorf("install seeded version: %w", err)
	}
	if err := blockView.release(); err != nil {
		return fmt.Errorf("release seeded version's reservations: %w", err)
	}
	return nil
}

// sealBaseline seals an empty version on every store. Called at startup so that we always have a view
// of the "previous" block (simplifies logic significantly).
func (s *CommitStore) sealBaseline() error {
	blockView, err := s.commitStores(s.committedVersion)
	if err != nil {
		return fmt.Errorf("seal baseline: %w", err)
	}

	for _, dbView := range blockView.viewSlice {
		if err := dbView.Finalize(nil); err != nil {
			return fmt.Errorf("%s finalize baseline: %w", dbView.Name(), err)
		}
	}

	if err := s.replaceSealedView(blockView); err != nil {
		return fmt.Errorf("install baseline: %w", err)
	}
	if err := blockView.release(); err != nil {
		return fmt.Errorf("release baseline reservations: %w", err)
	}
	return nil
}

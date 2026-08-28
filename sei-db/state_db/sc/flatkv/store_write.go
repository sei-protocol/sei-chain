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

	// Periodic snapshot so WAL stays bounded and restarts are fast. A failure fails the commit: the
	// flush wait inside WriteSnapshot is where a dead store surfaces, and a block whose data will never
	// reach disk must not be reported as committed. The block is already durable in the WAL, so replay
	// reconciles whatever the caller's halt leaves behind.
	if s.config.SnapshotInterval > 0 && version%int64(s.config.SnapshotInterval) == 0 {
		s.phaseTimer.SetPhase("commit_write_snapshot")
		if err := s.WriteSnapshot(""); err != nil {
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
func (s *CommitStore) sealBlock(version int64, alreadyHave map[string]int64) (retErr error) {
	s.phaseTimer.SetPhase("commit_seal_stores")

	views := make(map[string]view.View, len(s.stores))
	defer func() {
		if retErr != nil {
			// An error in this function is non-recoverable. Outer scope is responsible for teardown.
			// The reservations this block took, and the previous block's still in lastSealed, are not
			// handed back here: they go away when the view managers close. A store kept alive past this
			// error never flushes again, since an unreleased view stalls every later one.
			return
		}
		// The new views are recorded even when the hand-back fails, so teardown can give them back.
		err := s.releaseLastSealed()
		s.lastSealed = views
		if err != nil {
			retErr = fmt.Errorf("release previous block's reservations: %w", err)
		}
	}()

	for _, store := range s.stores {
		start := time.Now()
		sealed, err := store.Commit()
		otelMetrics.CommitBatchLatency.Record(s.ctx, secondsSince(start),
			metric.WithAttributes(dbAttr(store.Name()), successAttr(err)))
		if err != nil {
			return fmt.Errorf("%s seal: %w", store.Name(), err)
		}
		views[sealed.Name()] = sealed
	}

	if err := s.hashSealedBlock(views); err != nil {
		return fmt.Errorf("hash sealed block: %w", err)
	}

	s.phaseTimer.SetPhase("commit_finalize_stores")
	for _, sealed := range views {
		if err := s.finalizeStore(sealed, version, alreadyHave); err != nil {
			return fmt.Errorf("finalize %s: %w", sealed.Name(), err)
		}
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

// hashSealedBlock folds the block that was just sealed into the store's hashes.
//
// The new values are each data store's view diff. The old values are those same keys read back
// from the previous block's view, which lastSealed still holds when this runs.
func (s *CommitStore) hashSealedBlock(sealed map[string]view.View) error {
	s.phaseTimer.SetPhase("commit_compute_lt_hash")

	changed, err := s.changedValuesByStore(sealed)
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
func (s *CommitStore) changedValuesByStore(sealed map[string]view.View) ([]lthash.DBPairs, error) {
	changed := make([][]lthash.KVPairWithLastValue, len(dataDBDirs))
	errs := make([]error, len(dataDBDirs))

	var wg sync.WaitGroup
	for i, dir := range dataDBDirs {
		idx, name := i, dir
		wg.Add(1)
		s.miscPool.Submit(func() {
			defer wg.Done()
			// A store committing its first block has no previous view, so every key in that block
			// is new. A missing entry yields nil, which changedValues reads as "no old values".
			changed[idx], errs[idx] = changedValues(sealed[name], s.lastSealed[name])
			if errs[idx] != nil {
				errs[idx] = fmt.Errorf("%s changed values: %w", name, errs[idx])
			}
		})
	}
	wg.Wait()

	out := make([]lthash.DBPairs, 0, len(dataDBDirs))
	for i, dir := range dataDBDirs {
		if errs[i] != nil {
			return nil, errs[i]
		}
		if len(changed[i]) == 0 {
			continue
		}
		out = append(out, lthash.DBPairs{Dir: dir, Pairs: changed[i]})
	}
	return out, nil
}

// changedValues returns one data store's changed keys, each with its new value and the value it held
// before, from the store's sealed diff and the view preceding it.
//
// A nil value in the diff is a deletion. Keys under the reserved metadata prefix are dropped: they are
// the store's bookkeeping, and folding them in would make the hash depend on its own recorded value.
func changedValues(sealed view.View, previous view.View) ([]lthash.KVPairWithLastValue, error) {
	diff, err := sealed.GetDiff()
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

// releaseLastSealed gives back the reservations recorded in lastSealed, which lets the stores resume
// writing out blocks later than the one those reservations were holding.
//
// Every reservation is handed back even if one of them fails, because a reservation left held stalls its
// store's flushes indefinitely. The failures are joined and returned.
func (s *CommitStore) releaseLastSealed() error {
	var errs []error
	for name, sealed := range s.lastSealed {
		if err := sealed.Release(); err != nil {
			errs = append(errs, fmt.Errorf("release sealed view for %s: %w", name, err))
		}
	}
	s.lastSealed = nil
	return errors.Join(errs...)
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
	for _, sealed := range s.lastSealed {
		if err := sealed.AwaitFlush(s.ctx); err != nil {
			return fmt.Errorf("await flush: %w", err)
		}
	}
	return nil
}

// finalizeStore finalizes one store's sealed block, recording the LocalMeta that describes it.
//
// A store that already reached this height records nothing. Its writes were skipped, so its hash still
// describes the later height it holds; writing this block's height alongside that hash would persist a
// pair that describes no single moment. Finalizing with an empty write set still makes the sealed
// version flushable, which is the only thing finalization is required to do.
func (s *CommitStore) finalizeStore(sealed view.View, version int64, alreadyHave map[string]int64) error {
	if alreadyHave[sealed.Name()] >= version {
		return sealed.Finalize(nil)
	}

	writes, err := encodeLocalMeta(
		version,
		s.perDBWorkingLtHash[sealed.Name()],
		s.perDBModuleWorkingLtHash[sealed.Name()],
		s.perDBModuleWorkingStats[sealed.Name()],
	)
	if err != nil {
		return fmt.Errorf("encode %s local meta at version %d: %w", sealed.Name(), version, err)
	}
	if err := sealed.Finalize(writes); err != nil {
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
	views := make(map[string]view.View, len(s.stores))

	for _, store := range s.stores {
		sealed, err := store.Commit()
		if err != nil {
			return fmt.Errorf("%s seal seeded version: %w", store.Name(), err)
		}
		views[sealed.Name()] = sealed

		if err := s.finalizeStore(sealed, seededVersion, nil); err != nil {
			return fmt.Errorf("%s finalize seeded version: %w", store.Name(), err)
		}
	}

	// The new views are recorded even when the hand-back fails, so teardown can give them back.
	err := s.releaseLastSealed()
	s.lastSealed = views
	if err != nil {
		return fmt.Errorf("release baseline reservations: %w", err)
	}
	return nil
}

// sealBaseline seals an empty version on every store. Called at startup so that we always have a view
// of the "previous" block (simplifies logic significantly).
func (s *CommitStore) sealBaseline() error {
	views := make(map[string]view.View, len(s.stores))

	for _, store := range s.stores {
		sealed, err := store.Commit()
		if err != nil {
			return fmt.Errorf("%s seal baseline: %w", store.Name(), err)
		}
		views[sealed.Name()] = sealed
		if err := sealed.Finalize(nil); err != nil {
			return fmt.Errorf("%s finalize baseline: %w", store.Name(), err)
		}
	}

	// The new views are recorded even when the hand-back fails, so teardown can give them back.
	err := s.releaseLastSealed()
	s.lastSealed = views
	if err != nil {
		return fmt.Errorf("release previous block's reservations: %w", err)
	}
	return nil
}

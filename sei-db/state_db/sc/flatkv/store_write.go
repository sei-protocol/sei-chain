package flatkv

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/snapshot"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
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
// Protocol: WAL → per-DB batch (with LocalMeta) → flush → update metaDB.
// On crash, catchup replays WAL to recover incomplete commits.
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

	// Committing a block that is already committed does nothing and reports success.
	//
	// This exists for Cosmos. RootHash commits the pending block so it has something to hash, and
	// rootmulti then calls Commit for that same block a moment later — see commitPendingBlock. Rather
	// than have the second call fail, it returns what the first one returned.
	//
	// Post-Cosmos this goes away: a single call will supply a block's writes and commit them, and there
	// will be no second commit to absorb.
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

	// Step 4: Clear per-block bookkeeping
	s.clearPendingBlock()

	// Step 5: Offer the block to the snapshot writer, which decides whether it becomes a snapshot and,
	// if so, writes it on its own goroutine. Periodic snapshots are what keep the WAL bounded and
	// restarts fast.
	//
	// A failure here fails the commit. The writer latches its first error and reports it from every
	// later call, so a checkpoint that failed with no caller to fail surfaces at the next commit
	// instead of being lost: a block whose data will never reach disk must not be reported as
	// committed. The block is already durable in the WAL, so replay reconciles whatever the caller's
	// halt leaves behind.
	//
	// lastSealed still holds this block's reservations for the duration of the call, which is all the
	// writer needs: it takes its own for as long as it keeps the block.
	if s.snapshotWriter != nil {
		s.phaseTimer.SetPhase("commit_offer_snapshot")
		if err := s.snapshotWriter.Offer(version, s.lastSealed); err != nil {
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

// FlushHashes blocks until the hasher has published a hash for every block committed so far. It is the
// synchronization point for a caller that wants PublishedHash to describe the version it just
// committed rather than however far behind the hasher is; block commit does not need it.
func (s *CommitStore) FlushHashes() error {
	if s.hasher == nil {
		return nil
	}
	return s.hasher.Flush()
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

	snapshots := make(map[string]snapshot.Snapshot, len(s.stores))
	defer func() {
		if retErr != nil {
			// An error in this function is non-recoverable. Outer scope is responsible for teardown.
			// The reservations this block took, and the previous block's still in lastSealed, are not
			// handed back here: they go away when the engines close. A store kept alive past this error
			// never flushes again, since an unreleased snapshot stalls every later one.
			return
		}
		// The new snapshots are recorded even when the hand-back fails, so teardown can give them back.
		err := s.releaseLastSealed()
		s.lastSealed = snapshots
		if err != nil {
			retErr = fmt.Errorf("release previous block's reservations: %w", err)
		}
	}()

	for _, store := range s.stores {
		start := time.Now()
		snap, err := store.Commit()
		otelMetrics.CommitBatchLatency.Record(s.ctx, secondsSince(start),
			metric.WithAttributes(dbAttr(store.Name()), successAttr(err)))
		if err != nil {
			return fmt.Errorf("%s seal: %w", store.Name(), err)
		}
		snapshots[snap.Name()] = snap
	}

	s.phaseTimer.SetPhase("commit_offer_hash")
	return s.offerHash(version, snapshots, alreadyHave)
}

// offerHash hands the sealed block to the hasher, which computes its lattice hash, records that hash on the
// block's snapshots, and publishes it.
//
// Reservations are taken on this block and on the one before it, because the hash is a delta: the prior value
// of every changed key is read from the preceding block, and holding that reservation is what keeps Pebble at
// that version while the read happens. Release it early and the read returns this block's value instead — a
// wrong hash, with no error. The hasher hands both sets back once it has read what it needs.
func (s *CommitStore) offerHash(
	version int64,
	current map[string]snapshot.Snapshot,
	alreadyHave map[string]int64,
) error {
	if s.hasher == nil {
		return fmt.Errorf("cannot hash version %d: store has no block hasher", version)
	}

	reservedCurrent, err := reserveSnapshots(current)
	if err != nil {
		return fmt.Errorf("reserve version %d for hashing: %w", version, err)
	}
	reservedPrevious, err := reserveSnapshots(s.lastSealed)
	if err != nil {
		return errors.Join(
			fmt.Errorf("reserve the block before version %d for hashing: %w", version, err),
			releaseSnapshots(reservedCurrent))
	}
	if err := s.hasher.Offer(version, reservedCurrent, reservedPrevious, alreadyHave); err != nil {
		return fmt.Errorf("hash version %d: %w", version, err)
	}
	return nil
}

// changedValuesByStore returns every key the block changed, with its new value and the value it held
// before, one set per data store.
//
// The stores are read concurrently on the misc pool; each one is an independent snapshot diff followed
// by a batch read of the previous snapshot.
//
// Only the four data stores are read: the metadata store holds store bookkeeping rather than state, and
// nothing it contains may reach a hash. That is load-bearing, not tidiness. The store-wide record is
// written once per block during catch-up and is transiently inconsistent while the databases sit at
// different heights (see loadGlobalMetadata); the whole reason that is harmless is that no hash reads
// it. A change that folded the stored store-wide LtHash into a computation, or added the metadata
// directory to dataDBDirs, would break that silently and make the inconsistency consensus-visible.
//
// The store-wide root is likewise rebuilt from scratch on every seal — HashCalculator.Compute sums the
// four per-database roots and never mixes in the previous store-wide value.
func changedValuesByStore(
	pool threading.Pool,
	current map[string]snapshot.Snapshot,
	previous map[string]snapshot.Snapshot,
) ([]lthash.DBPairs, error) {
	changed := make([][]lthash.KVPairWithLastValue, len(dataDBDirs))
	errs := make([]error, len(dataDBDirs))

	var wg sync.WaitGroup
	for i, dir := range dataDBDirs {
		idx, name := i, dir
		wg.Add(1)
		pool.Submit(func() {
			defer wg.Done()
			// A store committing its first block has no previous snapshot, so every key in that block
			// is new. A missing entry yields nil, which changedValues reads as "no old values".
			changed[idx], errs[idx] = changedValues(current[name], previous[name])
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
// before, from the store's sealed diff and the snapshot preceding it.
//
// A nil value in the diff is a deletion. Keys under the reserved metadata prefix are dropped: they are
// the store's bookkeeping, and folding them in would make the hash depend on its own recorded value.
func changedValues(sealed snapshot.Snapshot, previous snapshot.Snapshot) ([]lthash.KVPairWithLastValue, error) {
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
	for name, snap := range s.lastSealed {
		if err := snap.Release(); err != nil {
			errs = append(errs, fmt.Errorf("release sealed snapshot for %s: %w", name, err))
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
	for _, snap := range s.lastSealed {
		if err := snap.AwaitFlush(s.ctx); err != nil {
			return fmt.Errorf("await flush: %w", err)
		}
	}
	return nil
}

// rawKVPair is a raw physical key/value pair as stored on disk.
type rawKVPair struct {
	Key   []byte
	Value []byte
}

// FinalizeImport persists per-DB metadata (version + LtHash) and global
// metadata after all import data has been written. This must be called
// exactly once at the end of an import to make the data durable across restarts.
func (s *CommitStore) FinalizeImport(version int64, seed hasherSeed) error {
	syncOpt := types.WriteOptions{Sync: true}
	for _, dir := range dataDBDirs {
		db := s.rawDBFor(dir)
		moduleHashes := seed.perDBModuleLtHash[dir]
		moduleStats := seed.perDBModuleStats[dir]
		batch := db.NewBatch()
		err := writeLocalMetaToBatch(batch, version, seed.perDBLtHash[dir], moduleHashes, moduleStats)
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
			LtHash:           seed.perDBLtHash[dir].Clone(),
			ModuleLtHashes:   cloneModuleHashes(moduleHashes),
			ModuleStats:      cloneModuleStats(moduleStats),
		}
	}

	globalHash := lthash.New()
	for _, dir := range dataDBDirs {
		globalHash.MixIn(seed.perDBLtHash[dir])
	}
	s.committedVersion = version
	s.committedLtHash = globalHash.Clone()
	if err := s.commitGlobalMetadata(version, s.committedLtHash); err != nil {
		return fmt.Errorf("import global metadata: %w", err)
	}

	// The hasher must carry the imported hashes from here, or the next block would be measured against the
	// state the import replaced.
	checksum := s.committedLtHash.Checksum()
	seed.committed = BlockHash{Hash: checksum[:], BlockHeight: version}
	if err := s.hasher.Reseed(seed); err != nil {
		return fmt.Errorf("adopt imported hash state: %w", err)
	}
	return nil
}

// sealBaseline seals an empty version on every store. Called at startup so that we always have a snapshot
// of the "previous" block (simplifies logic significantly).
func (s *CommitStore) sealBaseline() (retErr error) {
	snapshots := make(map[string]snapshot.Snapshot, len(s.stores))
	defer func() {
		if retErr != nil {
			for _, snap := range snapshots {
				_ = snap.Release()
			}
		}
	}()

	for _, store := range s.stores {
		snap, err := store.Commit()
		if err != nil {
			return fmt.Errorf("%s seal baseline: %w", store.Name(), err)
		}
		snapshots[snap.Name()] = snap
		if err := snap.Finalize(nil); err != nil {
			return fmt.Errorf("%s finalize baseline: %w", store.Name(), err)
		}
	}

	// The new snapshots are recorded even when the hand-back fails, so teardown can give them back.
	err := s.releaseLastSealed()
	s.lastSealed = snapshots
	if err != nil {
		return fmt.Errorf("release previous block's reservations: %w", err)
	}
	return nil
}

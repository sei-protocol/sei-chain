package flatkv

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	"go.opentelemetry.io/otel/metric"
)

// Commit persists buffered writes and advances the version.
// Protocol: WAL → per-DB batch (with LocalMeta) → flush → update metaDB.
// On crash, catchup replays WAL to recover incomplete commits.
func (s *CommitStore) Commit() (version int64, err error) {
	start := time.Now()

	// TODO(concurrency): This takes a single coarse write lock for the whole
	// commit, so it also blocks readers/iterator construction during the WAL
	// fsync and the periodic auto-snapshot. That is fine today because commits
	// are not pipelined with reads (there is currently no pipelining at all).
	// When commit pipelining is introduced, replace this with a finer-grained
	// scheme.
	s.mu.Lock()
	defer s.mu.Unlock()

	pendingAccount := len(s.accountWrites)
	pendingCode := len(s.codeWrites)
	pendingStorage := len(s.storageWrites)
	pendingMisc := len(s.miscWrites)
	defer func() {
		otelMetrics.CommitLatency.Record(s.ctx, secondsSince(start),
			metric.WithAttributes(successAttr(err)))
		if err != nil && !errors.Is(err, errReadOnly) {
			logger.Error("FlatKV Commit failed",
				"version", version,
				"pendingAccount", pendingAccount,
				"pendingCode", pendingCode,
				"pendingStorage", pendingStorage,
				"pendingMisc", pendingMisc,
				"elapsed", time.Since(start),
				"err", err)
		}
	}()

	if s.readOnly {
		return 0, errReadOnly
	}
	// Auto-increment version
	version = s.committedVersion + 1

	// Step 1: Write Changelog (WAL) - source of truth (always sync)
	s.phaseTimer.SetPhase("commit_write_changelog")
	changelogEntry := proto.ChangelogEntry{
		Version:    version,
		Changesets: s.pendingChangeSets,
	}
	if err := s.changelog.Write(changelogEntry); err != nil {
		return version, fmt.Errorf("changelog write: %w", err)
	}

	// Step 2: Commit to each DB (data + LocalMeta.CommittedVersion atomically)
	if err := s.commitBatches(version); err != nil {
		return version, fmt.Errorf("db commit: %w", err)
	}

	// Step 3: Persist global metadata to metadata DB.
	// This must succeed before we update in-memory state; otherwise a
	// metadataDB write failure would leave committedVersion advanced while
	// the caller sees an error, making the store's internal state
	// inconsistent. Per-DB data is already committed (Step 2) and the WAL
	// (Step 1) is the source of truth, so a restart will self-heal via
	// catchup even if we fail here.
	s.phaseTimer.SetPhase("commit_write_metadata")
	committedLtHash := s.workingLtHash.Clone()
	if err := s.commitGlobalMetadata(version, committedLtHash); err != nil {
		return version, fmt.Errorf("metadata DB commit: %w", err)
	}

	// Step 4: Update in-memory committed state (only after metadata persisted)
	s.phaseTimer.SetPhase("commit_update_lt_hash")
	s.committedVersion = version
	s.committedLtHash = committedLtHash

	// Step 5: Clear pending buffers
	s.phaseTimer.SetPhase("commit_clear_pending_writes")
	s.clearPendingWrites()
	recordPendingWrites(s.ctx, accountDBDir, 0)
	recordPendingWrites(s.ctx, codeDBDir, 0)
	recordPendingWrites(s.ctx, storageDBDir, 0)
	recordPendingWrites(s.ctx, miscDBDir, 0)

	// Periodic snapshot so WAL stays bounded and restarts are fast.
	if s.config.SnapshotInterval > 0 && version%int64(s.config.SnapshotInterval) == 0 {
		s.phaseTimer.SetPhase("commit_write_snapshot")
		if err := s.WriteSnapshot(""); err != nil {
			logger.Error("auto snapshot failed", "version", version, "err", err)
		}
	}

	// Best-effort WAL truncation, throttled to amortize ReadDir cost.
	if version%1000 == 0 {
		s.tryTruncateWAL()
	}

	s.phaseTimer.SetPhase("commit_done")
	otelMetrics.CurrentVersion.Record(s.ctx, version)
	s.recordAllModuleStats()
	logger.Info("FlatKV Commit complete",
		"version", version,
		"totalWriteCount", pendingAccount+pendingCode+pendingStorage+pendingMisc,
		"elapsed", time.Since(start))
	return version, nil
}

// flushAllDBs flushes all DBs in parallel.
func (s *CommitStore) flushAllDBs() error {
	errs := make([]error, 4)
	var wg sync.WaitGroup
	wg.Add(4)
	names := [4]string{accountDBDir, codeDBDir, storageDBDir, miscDBDir}
	for i, db := range s.dataDBs() {
		s.miscPool.Submit(func() {
			defer wg.Done()
			start := time.Now()
			errs[i] = db.Flush()
			otelMetrics.FlushLatency.Record(s.ctx, secondsSince(start),
				metric.WithAttributes(dbAttr(names[i]), successAttr(errs[i])))
		})
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			return fmt.Errorf("%s flush: %w", names[i], err)
		}
	}
	return nil
}

func (s *CommitStore) clearPendingWrites() {
	s.accountWrites = make(map[string]*vtype.AccountData, len(s.accountWrites))
	s.codeWrites = make(map[string]*vtype.CodeData, len(s.codeWrites))
	s.storageWrites = make(map[string]*vtype.StorageData, len(s.storageWrites))
	s.miscWrites = make(map[string]*vtype.MiscData, len(s.miscWrites))
	s.pendingChangeSets = make([]*proto.NamedChangeSet, 0, len(s.pendingChangeSets))
}

// commitBatches commits pending writes to their respective DBs atomically.
// Each DB batch includes LocalMeta update for crash recovery.
// Batches are built serially, then committed in parallel.
// Also called by catchup to replay WAL without re-writing changelog.
func (s *CommitStore) commitBatches(version int64) error {
	syncOpt := types.WriteOptions{Sync: s.config.Fsync}

	type pendingCommit struct {
		dbDir string
		batch types.Batch
	}
	var pendingBuf [4]pendingCommit
	pending := pendingBuf[:0]
	defer func() {
		for _, p := range pending {
			_ = p.batch.Close()
		}
	}()

	specs := []struct {
		dbDir string
		phase string
		prep  func() (types.Batch, error)
	}{
		{accountDBDir, "commit_account_db_prepare", func() (types.Batch, error) {
			return prepareBatch(s.accountDB, s.accountWrites, version, s.localMeta[accountDBDir], s.perDBWorkingLtHash[accountDBDir], s.perDBModuleWorkingLtHash[accountDBDir], s.perDBModuleWorkingStats[accountDBDir], "accountDB")
		}},
		{codeDBDir, "commit_code_db_prepare", func() (types.Batch, error) {
			return prepareBatch(s.codeDB, s.codeWrites, version, s.localMeta[codeDBDir], s.perDBWorkingLtHash[codeDBDir], s.perDBModuleWorkingLtHash[codeDBDir], s.perDBModuleWorkingStats[codeDBDir], "codeDB")
		}},
		{storageDBDir, "commit_storage_db_prepare", func() (types.Batch, error) {
			return prepareBatch(s.storageDB, s.storageWrites, version, s.localMeta[storageDBDir], s.perDBWorkingLtHash[storageDBDir], s.perDBModuleWorkingLtHash[storageDBDir], s.perDBModuleWorkingStats[storageDBDir], "storageDB")
		}},
		{miscDBDir, "commit_misc_db_prepare", func() (types.Batch, error) {
			return prepareBatch(s.miscDB, s.miscWrites, version, s.localMeta[miscDBDir], s.perDBWorkingLtHash[miscDBDir], s.perDBModuleWorkingLtHash[miscDBDir], s.perDBModuleWorkingStats[miscDBDir], "miscDB")
		}},
	}

	for _, spec := range specs {
		s.phaseTimer.SetPhase(spec.phase)
		batch, err := spec.prep()
		if err != nil {
			return fmt.Errorf("%s commit: %w", spec.dbDir, err)
		}
		if batch != nil {
			pending = append(pending, pendingCommit{spec.dbDir, batch})
		}
	}

	if len(pending) == 0 {
		return nil
	}

	// Commit all batches in parallel.
	s.phaseTimer.SetPhase("commit_batches_parallel")
	errs := make([]error, len(pending))
	var wg sync.WaitGroup
	wg.Add(len(pending))
	for i, p := range pending {
		s.miscPool.Submit(func() {
			defer wg.Done()
			start := time.Now()
			errs[i] = p.batch.Commit(syncOpt)
			otelMetrics.CommitBatchLatency.Record(s.ctx, secondsSince(start),
				metric.WithAttributes(dbAttr(p.dbDir), successAttr(errs[i])))
		})
	}
	wg.Wait()

	for i, p := range pending {
		if errs[i] != nil {
			return fmt.Errorf("%s commit: %w", p.dbDir, errs[i])
		}
	}

	// Update in-memory local meta after all commits succeed. Per-module stats gauges are deliberately
	// NOT recorded here: commitBatches is also called by catchup (WAL replay on open, import, rollback,
	// and openReadOnly's historical read-only clone), and recording from every replayed entry would
	// publish stale/historical values into the process-global gauges, clobbering the live store's real
	// values. Only Commit's success path (an actual new block being committed) records via
	// recordAllModuleStats.
	for _, p := range pending {
		s.localMeta[p.dbDir] = &ktype.LocalMeta{
			CommittedVersion: version,
			LtHash:           s.perDBWorkingLtHash[p.dbDir].Clone(),
			ModuleLtHashes:   cloneModuleHashes(s.perDBModuleWorkingLtHash[p.dbDir]),
			ModuleStats:      cloneModuleStats(s.perDBModuleWorkingStats[p.dbDir]),
		}
	}
	return nil
}

// recordAllModuleStats records the current per-module key-count / byte totals for every data DB. Called
// only from Commit's success path — i.e. only for an actual new block being committed, never for open
// (LoadVersion/catchup), import (FinalizeImport), rollback, or a read-only historical replay — so the
// process-global gauges always reflect the live store, not a replay.
func (s *CommitStore) recordAllModuleStats() {
	for _, dir := range dataDBDirs {
		recordModuleStats(s.ctx, dir, s.perDBModuleWorkingStats[dir])
	}
}

func prepareBatch[T vtype.VType](
	db types.KeyValueDB,
	writes map[string]T,
	version int64,
	localMeta *ktype.LocalMeta,
	ltHash *lthash.LtHash,
	moduleHashes map[string]*lthash.LtHash,
	moduleStats map[string]lthash.ModuleStats,
	dbName string,
) (types.Batch, error) {
	if len(writes) == 0 && version <= localMeta.CommittedVersion {
		return nil, nil
	}

	batch := db.NewBatch()
	for keyStr, w := range writes {
		key := []byte(keyStr)
		if w.IsDelete() {
			if err := batch.Delete(key); err != nil {
				_ = batch.Close()
				return nil, fmt.Errorf("%s delete: %w", dbName, err)
			}
		} else {
			if err := batch.Set(key, w.Serialize()); err != nil {
				_ = batch.Close()
				return nil, fmt.Errorf("%s set: %w", dbName, err)
			}
		}
	}

	if err := writeLocalMetaToBatch(batch, version, ltHash, moduleHashes, moduleStats); err != nil {
		_ = batch.Close()
		return nil, fmt.Errorf("%s local meta: %w", dbName, err)
	}
	return batch, nil
}

// ReadOldValues implements lthash.OldValueReader. It returns the prior
// serialized value for each requested physical key of one data DB (dir),
// resolving pending same-block writes from memory and batch-reading the rest
// from disk.
//
// The returned bytes are the on-disk serialized form (or the serialized pending
// write). By the round-trip identity of the value serializers, these are the
// exact bytes that were folded into the LtHash when the key was last written,
// so unmixing them cancels that contribution precisely. A key that resolves to
// a pending deletion (or is absent) is mapped to a nil value: "resolved, but no
// bytes to unmix".
//
// Callers must hold s.mu (the commit path does): this reads the pending-write
// overlay maps concurrently across dirs, but each dir touches a distinct map.
func (s *CommitStore) ReadOldValues(dir string, physKeys map[string]struct{}) (map[string][]byte, error) {
	db, err := s.dataDBByDir(dir)
	if err != nil {
		return nil, err
	}

	old := make(map[string][]byte, len(physKeys))
	batch := make(map[string]types.BatchGetResult, len(physKeys))
	for key := range physKeys {
		if v, resolved := s.pendingOldSerialized(dir, key); resolved {
			old[key] = v
		} else {
			batch[key] = types.BatchGetResult{}
		}
	}

	if len(batch) > 0 {
		if err := db.BatchGet(batch); err != nil {
			return nil, fmt.Errorf("%s batch get: %w", dir, err)
		}
		for k, v := range batch {
			if v.Error != nil {
				return nil, fmt.Errorf("%s batch read error for key %x: %w", dir, k, v.Error)
			}
			if v.IsFound() {
				// v.Value may alias a pebble buffer reused after this call.
				old[k] = bytes.Clone(v.Value)
			}
		}
	}
	return old, nil
}

// pendingOldSerialized returns the serialized value of a key still buffered in
// this block's pending writes, to be used as its "old" value for the current
// apply. The bool reports whether the key was resolved from the pending overlay
// at all; a pending deletion resolves to (nil, true) since there is nothing to
// unmix (the prior committed value was already unmixed when the delete was
// applied earlier in the block).
func (s *CommitStore) pendingOldSerialized(dir, key string) ([]byte, bool) {
	switch dir {
	case accountDBDir:
		if v, ok := s.accountWrites[key]; ok {
			return serializedUnlessDelete(v), true
		}
	case codeDBDir:
		if v, ok := s.codeWrites[key]; ok {
			return serializedUnlessDelete(v), true
		}
	case storageDBDir:
		if v, ok := s.storageWrites[key]; ok {
			return serializedUnlessDelete(v), true
		}
	case miscDBDir:
		if v, ok := s.miscWrites[key]; ok {
			return serializedUnlessDelete(v), true
		}
	}
	return nil, false
}

// serializedUnlessDelete serializes v, or returns nil if v represents a
// deletion (nothing to unmix).
func serializedUnlessDelete[T vtype.VType](v T) []byte {
	if v.IsDelete() {
		return nil
	}
	return v.Serialize()
}

// dataDBByDir returns the underlying KeyValueDB for a data DB dir.
func (s *CommitStore) dataDBByDir(dir string) (types.KeyValueDB, error) {
	switch dir {
	case accountDBDir:
		return s.accountDB, nil
	case codeDBDir:
		return s.codeDB, nil
	case storageDBDir:
		return s.storageDB, nil
	case miscDBDir:
		return s.miscDB, nil
	}
	return nil, fmt.Errorf("unknown data DB dir %q", dir)
}

// rawKVPair is a raw physical key/value pair as stored on disk.
type rawKVPair struct {
	Key   []byte
	Value []byte
}

// FinalizeImport persists per-DB metadata (version + LtHash) and global
// metadata after all import data has been written. This must be called
// exactly once at the end of an import to make the data durable across restarts.
func (s *CommitStore) FinalizeImport(version int64) error {
	syncOpt := types.WriteOptions{Sync: true}
	for _, ndb := range s.namedDataDBs() {
		moduleHashes := s.perDBModuleWorkingLtHash[ndb.dir]
		moduleStats := s.perDBModuleWorkingStats[ndb.dir]
		batch := ndb.db.NewBatch()
		if err := writeLocalMetaToBatch(batch, version, s.perDBWorkingLtHash[ndb.dir], moduleHashes, moduleStats); err != nil {
			_ = batch.Close()
			return fmt.Errorf("%s local meta: %w", ndb.dir, err)
		}
		if err := batch.Commit(syncOpt); err != nil {
			_ = batch.Close()
			return fmt.Errorf("%s commit: %w", ndb.dir, err)
		}
		_ = batch.Close()
		s.localMeta[ndb.dir] = &ktype.LocalMeta{
			CommittedVersion: version,
			LtHash:           s.perDBWorkingLtHash[ndb.dir].Clone(),
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
	if err := s.commitGlobalMetadata(version, s.committedLtHash); err != nil {
		return fmt.Errorf("import global metadata: %w", err)
	}
	return nil
}

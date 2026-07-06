package flatkv

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
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
	pendingLegacy := len(s.legacyWrites)
	defer func() {
		otelMetrics.CommitLatency.Record(s.ctx, secondsSince(start),
			metric.WithAttributes(successAttr(err)))
		if err != nil && !errors.Is(err, errReadOnly) {
			logger.Error("FlatKV Commit failed",
				"version", version,
				"pendingAccount", pendingAccount,
				"pendingCode", pendingCode,
				"pendingStorage", pendingStorage,
				"pendingLegacy", pendingLegacy,
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
	recordPendingWrites(s.ctx, legacyDBDir, 0)

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
	logger.Info("FlatKV Commit complete",
		"version", version,
		"pendingAccount", pendingAccount,
		"pendingCode", pendingCode,
		"pendingStorage", pendingStorage,
		"pendingLegacy", pendingLegacy,
		"elapsed", time.Since(start))
	return version, nil
}

// flushAllDBs flushes all DBs in parallel.
func (s *CommitStore) flushAllDBs() error {
	errs := make([]error, 4)
	var wg sync.WaitGroup
	wg.Add(4)
	names := [4]string{accountDBDir, codeDBDir, storageDBDir, legacyDBDir}
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
	s.legacyWrites = make(map[string]*vtype.LegacyData, len(s.legacyWrites))
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
			return prepareBatch(s.accountDB, s.accountWrites, version, s.localMeta[accountDBDir], s.perDBWorkingLtHash[accountDBDir], "accountDB")
		}},
		{codeDBDir, "commit_code_db_prepare", func() (types.Batch, error) {
			return prepareBatch(s.codeDB, s.codeWrites, version, s.localMeta[codeDBDir], s.perDBWorkingLtHash[codeDBDir], "codeDB")
		}},
		{storageDBDir, "commit_storage_db_prepare", func() (types.Batch, error) {
			return prepareBatch(s.storageDB, s.storageWrites, version, s.localMeta[storageDBDir], s.perDBWorkingLtHash[storageDBDir], "storageDB")
		}},
		{legacyDBDir, "commit_legacy_db_prepare", func() (types.Batch, error) {
			return prepareBatch(s.legacyDB, s.legacyWrites, version, s.localMeta[legacyDBDir], s.perDBWorkingLtHash[legacyDBDir], "legacyDB")
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

	// Update in-memory local meta after all commits succeed.
	for _, p := range pending {
		s.localMeta[p.dbDir] = &ktype.LocalMeta{
			CommittedVersion: version,
			LtHash:           s.perDBWorkingLtHash[p.dbDir].Clone(),
		}
	}
	return nil
}

func prepareBatch[T vtype.VType](
	db types.KeyValueDB,
	writes map[string]T,
	version int64,
	localMeta *ktype.LocalMeta,
	ltHash *lthash.LtHash,
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

	if err := writeLocalMetaToBatch(batch, version, ltHash); err != nil {
		_ = batch.Close()
		return nil, fmt.Errorf("%s local meta: %w", dbName, err)
	}
	return batch, nil
}

// collectPendingReads partitions keys from changeMaps into those already
// buffered in pendingWrites (copied to old) and those needing a DB read
// (returned as a BatchGetResult map).
func collectPendingReads[T vtype.VType](
	pendingWrites map[string]T,
	old map[string]T,
	changeMaps ...map[string][]byte,
) map[string]types.BatchGetResult {
	totalKeys := 0
	for _, changes := range changeMaps {
		totalKeys += len(changes)
	}
	batch := make(map[string]types.BatchGetResult, totalKeys)
	for _, changes := range changeMaps {
		for key := range changes {
			if v, ok := pendingWrites[key]; ok {
				old[key] = v
			} else {
				batch[key] = types.BatchGetResult{}
			}
		}
	}
	return batch
}

// deserializeBatchResults converts raw BatchGetResults into typed values.
func deserializeBatchResults[T vtype.VType](
	batch map[string]types.BatchGetResult,
	old map[string]T,
	deserialize func([]byte) (T, error),
	dbName string,
) error {
	for k, v := range batch {
		if v.Error != nil {
			return fmt.Errorf("%s batch read error for key %x: %w", dbName, k, v.Error)
		}
		if v.IsFound() {
			val, err := deserialize(v.Value)
			if err != nil {
				return fmt.Errorf("failed to deserialize %s data: %w", dbName, err)
			}
			old[k] = val
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
func (s *CommitStore) FinalizeImport(version int64) error {
	syncOpt := types.WriteOptions{Sync: true}
	for _, ndb := range s.namedDataDBs() {
		batch := ndb.db.NewBatch()
		if err := writeLocalMetaToBatch(batch, version, s.perDBWorkingLtHash[ndb.dir]); err != nil {
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

// batchReadOldValues returns the prior value for every key in changesByType.
// Pending writes are resolved from memory; the rest are batch-read from disk
// in parallel.
func (s *CommitStore) batchReadOldValues(changesByType map[keys.EVMKeyKind]map[string][]byte) (
	storageOld map[string]*vtype.StorageData,
	accountOld map[string]*vtype.AccountData,
	codeOld map[string]*vtype.CodeData,
	legacyOld map[string]*vtype.LegacyData,
	err error,
) {
	start := time.Now()
	defer func() {
		otelMetrics.BatchReadOldValuesLatency.Record(s.ctx, secondsSince(start),
			metric.WithAttributes(successAttr(err)))
	}()

	storageOld = make(map[string]*vtype.StorageData, len(changesByType[keys.EVMKeyStorage]))
	accountOld = make(map[string]*vtype.AccountData, len(changesByType[keys.EVMKeyNonce])+len(changesByType[keys.EVMKeyCodeHash]))
	codeOld = make(map[string]*vtype.CodeData, len(changesByType[keys.EVMKeyCode]))
	legacyOld = make(map[string]*vtype.LegacyData, len(changesByType[keys.EVMKeyLegacy]))

	storageBatch := collectPendingReads(s.storageWrites, storageOld, changesByType[keys.EVMKeyStorage])
	// TODO: add balance changeMap when balance key is supported.
	accountBatch := collectPendingReads(s.accountWrites, accountOld, changesByType[keys.EVMKeyNonce], changesByType[keys.EVMKeyCodeHash])
	codeBatch := collectPendingReads(s.codeWrites, codeOld, changesByType[keys.EVMKeyCode])
	legacyBatch := collectPendingReads(s.legacyWrites, legacyOld, changesByType[keys.EVMKeyLegacy])

	type readJob struct {
		batch map[string]types.BatchGetResult
		db    types.KeyValueDB
	}
	jobs := [4]readJob{
		{storageBatch, s.storageDB},
		{accountBatch, s.accountDB},
		{codeBatch, s.codeDB},
		{legacyBatch, s.legacyDB},
	}
	readErrs := make([]error, 4)
	var wg sync.WaitGroup
	for i := range jobs {
		idx := i
		job := jobs[i]
		if len(job.batch) > 0 {
			wg.Add(1)
			s.miscPool.Submit(func() {
				defer wg.Done()
				readErrs[idx] = job.db.BatchGet(job.batch)
			})
		}
	}
	wg.Wait()

	if err = errors.Join(readErrs...); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to batch read old values: %w", err)
	}

	if err = deserializeBatchResults(storageBatch, storageOld, vtype.DeserializeStorageData, "storageDB"); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("deserialize storageDB old values: %w", err)
	}
	if err = deserializeBatchResults(accountBatch, accountOld, vtype.DeserializeAccountData, "accountDB"); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("deserialize accountDB old values: %w", err)
	}
	if err = deserializeBatchResults(codeBatch, codeOld, vtype.DeserializeCodeData, "codeDB"); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("deserialize codeDB old values: %w", err)
	}
	if err = deserializeBatchResults(legacyBatch, legacyOld, vtype.DeserializeLegacyData, "legacyDB"); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("deserialize legacyDB old values: %w", err)
	}

	return storageOld, accountOld, codeOld, legacyOld, nil
}

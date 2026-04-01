package flatkv

import (
	"errors"
	"fmt"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// ApplyChangeSets buffers EVM changesets and updates LtHash.
//
// LtHash is computed based on actual storage format (internal keys):
// - storageDB: key=addr||slot, value=storage_value
// - accountDB: key=addr, value=AccountValue (balance(32)||nonce(8)||codehash(32)
// - codeDB: key=addr, value=bytecode
// - legacyDB: key=full original key (with prefix), value=raw value
func (s *CommitStore) ApplyChangeSets(cs []*proto.NamedChangeSet) error {
	if s.readOnly {
		return errReadOnly
	}

	s.phaseTimer.SetPhase("apply_change_sets_batch_read")

	// Batch read all old values from DBs in parallel.
	storageOld, accountOld, codeOld, legacyOld, err := s.batchReadOldValues(cs)
	if err != nil {
		return fmt.Errorf("failed to batch read old values: %w", err)
	}

	s.phaseTimer.SetPhase("apply_change_sets_prepare")
	s.pendingChangeSets = append(s.pendingChangeSets, cs...)

	s.phaseTimer.SetPhase("apply_change_sets_sort")
	changesByType := sortChangeSets(cs)

	// Gather LTHash pairs.
	s.phaseTimer.SetPhase("apply_change_sets_gather_pairs")

	// Gather Account Pairs (special case since accounts have multiple fields)
	accountWrites, err := s.mergeAccountUpdates(
		changesByType[evm.EVMKeyNonce],
		changesByType[evm.EVMKeyCodeHash],
		nil, // TODO: update this when we add a balance key!
	)
	if err != nil {
		return fmt.Errorf("failed to gather account updates: %w", err)
	}
	accountPairs, err := s.gatherAccountPairs(accountWrites, accountOld)
	if err != nil {
		return fmt.Errorf("failed to gather account pairs: %w", err)
	}

	// Gather all of the other DBs pairs.
	storagePairs := s.gatherPairs(changesByType[evm.EVMKeyStorage], storageOld)
	codePairs := s.gatherPairs(changesByType[evm.EVMKeyCode], codeOld)
	legacyPairs := s.gatherPairs(changesByType[evm.EVMKeyLegacy], legacyOld)

	s.phaseTimer.SetPhase("apply_change_compute_lt_hash")

	// Per-DB LTHash updates
	type dbPairs struct {
		dir   string
		pairs []lthash.KVPairWithLastValue
	}
	for _, dp := range [4]dbPairs{
		{storageDBDir, storagePairs},
		{accountDBDir, accountPairs},
		{codeDBDir, codePairs},
		{legacyDBDir, legacyPairs},
	} {
		if len(dp.pairs) > 0 {
			newHash, _ := lthash.ComputeLtHash(s.perDBWorkingLtHash[dp.dir], dp.pairs)
			s.perDBWorkingLtHash[dp.dir] = newHash
		}
	}

	// Global LTHash = sum of per-DB hashes (homomorphic property).
	// Compute into a fresh hash and swap to avoid a transient empty state
	// on workingLtHash (safe for future pipelining / async callers).
	globalHash := lthash.New()
	for _, dir := range dataDBDirs {
		globalHash.MixIn(s.perDBWorkingLtHash[dir])
	}
	s.workingLtHash = globalHash

	s.phaseTimer.SetPhase("apply_change_done")
	return nil
}

// Sort the change sets by type.
func sortChangeSets(cs []*proto.NamedChangeSet) map[evm.EVMKeyKind]map[string][]byte {
	// TODO add the ability to detect and report/err for unexected types!

	result := make(map[evm.EVMKeyKind]map[string][]byte)

	for _, cs := range cs {
		if cs.Changeset.Pairs == nil {
			continue
		}
		for _, pair := range cs.Changeset.Pairs {
			kind, keyBytes := evm.ParseEVMKey(pair.Key)
			keyStr := string(keyBytes)

			kindMap, ok := result[kind]
			if !ok {
				kindMap = make(map[string][]byte)
				result[kind] = kindMap
			}

			kindMap[keyStr] = pair.Value
		}
	}

	return result
}

// Gather LtHash pairs for a DB. Not suitable for the storage DB, but ok for the others.
func (s *CommitStore) gatherPairs(
	changes map[string][]byte,
	oldValues map[string]types.BatchGetResult,
) []lthash.KVPairWithLastValue {

	var pairs []lthash.KVPairWithLastValue = make([]lthash.KVPairWithLastValue, 0, len(changes))

	for keyStr, newValue := range changes {

		var oldValue []byte
		if value, ok := oldValues[keyStr]; ok && value.IsFound() {
			// We've got a value in the database for this key, use it as the old value.
			oldValue = oldValue
		}

		pairs = append(pairs, lthash.KVPairWithLastValue{
			Key:       []byte(keyStr),
			Value:     newValue,
			LastValue: oldValue,
			Delete:    false, // TODO how to handle deletion here?
		})
	}

	return pairs
}

// Merge account updates down into a single update per account.
func (s *CommitStore) mergeAccountUpdates(
	nonceChanges map[string][]byte,
	codeHashChanges map[string][]byte,
	balanceChanges map[string][]byte,
) (map[string]*vtype.PendingAccountWrite, error) {

	updates := make(map[string]*vtype.PendingAccountWrite)

	if nonceChanges != nil {
		for key, nonceChange := range nonceChanges {
			nonce, err := vtype.ParseNonce(nonceChange)
			if err != nil {
				return nil, fmt.Errorf("invalid nonce value: %w", err)
			}
			// nil handled internally, no need to bootstrap map entries
			updates[key] = updates[key].SetNonce(nonce)
		}
	}

	if codeHashChanges != nil {
		for key, codeHashChange := range codeHashChanges {
			codeHash, err := vtype.ParseCodeHash(codeHashChange)
			if err != nil {
				return nil, fmt.Errorf("invalid codehash value: %w", err)
			}
			// nil handled internally, no need to bootstrap map entries
			updates[key] = updates[key].SetCodeHash(codeHash)
		}
	}

	if balanceChanges != nil {
		for key, balanceChange := range balanceChanges {
			balance, err := vtype.ParseBalance(balanceChange)
			if err != nil {
				return nil, fmt.Errorf("invalid balance value: %w", err)
			}
			// nil handled internally, no need to bootstrap map entries
			updates[key] = updates[key].SetBalance(balance)
		}
	}

	return updates, nil
}

// For each update being applied to an account, gather the new/old values for use by LtHash delta computation.
func (s *CommitStore) gatherAccountPairs(
	// Writes being performed. Writes to different account fields are combined per account.
	pendingWrites map[string]*vtype.PendingAccountWrite,
	// Account values from the database.
	databaseAccountValues map[string]types.BatchGetResult,
) ([]lthash.KVPairWithLastValue, error) {

	result := make([]lthash.KVPairWithLastValue, 0, len(pendingWrites))

	for addrStr, pendingWrite := range pendingWrites {
		var oldValue *vtype.AccountData

		if stagedWrite, ok := s.accountWrites[addrStr]; ok {
			// We've got a pending write staged in memory
			oldValue = stagedWrite
		} else if dbValue, ok := databaseAccountValues[addrStr]; ok {
			// This account is in the DB
			var err error
			oldValue, err = vtype.DeserializeAccountData(dbValue.Value)
			if err != nil {
				return nil, fmt.Errorf("invalid account data in DB: %w", err)
			}
		}

		newValue := pendingWrite.Merge(oldValue, s.committedVersion+1)

		result = append(result, lthash.KVPairWithLastValue{
			Key:       []byte(addrStr),
			Value:     newValue.Serialize(),
			LastValue: oldValue.Serialize(),
			Delete:    newValue.IsDelete(),
		})
	}

	return result, nil
}

// Commit persists buffered writes and advances the version.
// Protocol: WAL → per-DB batch (with LocalMeta) → flush → update metaDB.
// On crash, catchup replays WAL to recover incomplete commits.
func (s *CommitStore) Commit() (int64, error) {
	if s.readOnly {
		return 0, errReadOnly
	}
	// Auto-increment version
	version := s.committedVersion + 1

	// Step 1: Write Changelog (WAL) - source of truth (always sync)
	s.phaseTimer.SetPhase("commit_write_changelog")
	changelogEntry := proto.ChangelogEntry{
		Version:    version,
		Changesets: s.pendingChangeSets,
	}
	if err := s.changelog.Write(changelogEntry); err != nil {
		return 0, fmt.Errorf("changelog write: %w", err)
	}

	// Step 2: Commit to each DB (data + LocalMeta.CommittedVersion atomically)
	if err := s.commitBatches(version); err != nil {
		return 0, fmt.Errorf("db commit: %w", err)
	}

	// Step 3: Update in-memory committed state
	s.phaseTimer.SetPhase("commit_update_lt_hash")
	s.committedVersion = version
	s.committedLtHash = s.workingLtHash.Clone()

	// Step 4: Persist global metadata to metadata DB (always every block)
	s.phaseTimer.SetPhase("commit_write_metadata")
	if err := s.commitGlobalMetadata(version, s.committedLtHash); err != nil {
		return 0, fmt.Errorf("metadata DB commit: %w", err)
	}

	// Step 5: Clear pending buffers
	s.phaseTimer.SetPhase("commit_clear_pending_writes")
	s.clearPendingWrites()

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
	logger.Info("Committed version", "version", version)
	return version, nil
}

// flushAllDBs flushes all DBs in parallel.
func (s *CommitStore) flushAllDBs() error {
	errs := make([]error, 4)
	var wg sync.WaitGroup
	wg.Add(4)
	for i, db := range []types.KeyValueDB{s.accountDB, s.codeDB, s.storageDB, s.legacyDB} {
		s.miscPool.Submit(func() {
			defer wg.Done()
			errs[i] = db.Flush()
		})
	}
	wg.Wait()
	names := [4]string{"accountDB", "codeDB", "storageDB", "legacyDB"}
	for i, err := range errs {
		if err != nil {
			return fmt.Errorf("%s flush: %w", names[i], err)
		}
	}
	return nil
}

// clearPendingWrites clears all pending write buffers
func (s *CommitStore) clearPendingWrites() {
	s.accountWrites = make(map[string]*vtype.AccountData)
	s.codeWrites = make(map[string]*vtype.CodeData)
	s.storageWrites = make(map[string]*vtype.StorageData)
	s.legacyWrites = make(map[string]*vtype.LegacyData)
	s.pendingChangeSets = make([]*proto.NamedChangeSet, 0)
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
	var pending []pendingCommit

	// Commit to accountDB
	// accountDB uses AccountValue structure: key=addr(20), value=balance(32)||nonce(8)||codehash(32)
	if len(s.accountWrites) > 0 || version > s.localMeta[accountDBDir].CommittedVersion {
		s.phaseTimer.SetPhase("commit_account_db_prepare")
		batch := s.accountDB.NewBatch()
		defer func() { _ = batch.Close() }()

		for keyStr, accountWrite := range s.accountWrites {
			key := []byte(keyStr) // TODO verify this is correct!
			if accountWrite.IsDelete() {
				if err := batch.Delete(key); err != nil {
					return fmt.Errorf("accountDB delete: %w", err)
				}
			} else {
				if err := batch.Set(key, accountWrite.Serialize()); err != nil {
					return fmt.Errorf("accountDB set: %w", err)
				}
			}
		}

		if err := writeLocalMetaToBatch(batch, version, s.perDBWorkingLtHash[accountDBDir]); err != nil {
			return fmt.Errorf("accountDB local meta: %w", err)
		}
		pending = append(pending, pendingCommit{accountDBDir, batch})
	}

	batch, err := s.prepareBatchCodeDB(version)
	if err != nil {
		return fmt.Errorf("codeDB commit: %w", err)
	}
	if batch != nil {
		pending = append(pending, pendingCommit{codeDBDir, batch})
	}

	batch, err = s.prepareBatchStorageDB(version)
	if err != nil {
		return fmt.Errorf("storageDB commit: %w", err)
	}
	if batch != nil {
		pending = append(pending, pendingCommit{storageDBDir, batch})
	}

	batch, err = s.prepareBatchLegacyDB(version)
	if err != nil {
		return fmt.Errorf("legacyDB commit: %w", err)
	}
	if batch != nil {
		pending = append(pending, pendingCommit{legacyDBDir, batch})
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
			errs[i] = p.batch.Commit(syncOpt)
			wg.Done()
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
		s.localMeta[p.dbDir] = &LocalMeta{
			CommittedVersion: version,
			LtHash:           s.perDBWorkingLtHash[p.dbDir].Clone(),
		}
	}
	return nil
}

// Prepare a batch of writes for the codeDB.
func (s *CommitStore) prepareBatchCodeDB(version int64) (types.Batch, error) {
	if len(s.codeWrites) == 0 && version <= s.localMeta[codeDBDir].CommittedVersion {
		return nil, nil
	}

	s.phaseTimer.SetPhase("commit_code_db_prepare")

	batch := s.codeDB.NewBatch()

	for keyStr, cw := range s.codeWrites {
		key := []byte(keyStr)
		if cw.IsDelete() {
			if err := batch.Delete(key); err != nil {
				_ = batch.Close()
				return nil, fmt.Errorf("codeDB delete: %w", err)
			}
		} else {
			if err := batch.Set(key, cw.Serialize()); err != nil {
				_ = batch.Close()
				return nil, fmt.Errorf("codeDB set: %w", err)
			}
		}
	}

	if err := writeLocalMetaToBatch(batch, version, s.perDBWorkingLtHash[codeDBDir]); err != nil {
		_ = batch.Close()
		return nil, fmt.Errorf("codeDB local meta: %w", err)
	}

	return batch, nil
}

// Prepare a batch of writes for the storageDB.
func (s *CommitStore) prepareBatchStorageDB(version int64) (types.Batch, error) {
	if len(s.storageWrites) == 0 && version <= s.localMeta[storageDBDir].CommittedVersion {
		return nil, nil
	}

	s.phaseTimer.SetPhase("commit_storage_db_prepare")

	batch := s.storageDB.NewBatch()

	for keyStr, sw := range s.storageWrites {
		key := []byte(keyStr)
		if sw.IsDelete() {
			if err := batch.Delete(key); err != nil {
				_ = batch.Close()
				return nil, fmt.Errorf("storageDB delete: %w", err)
			}
		} else {
			if err := batch.Set(key, sw.Serialize()); err != nil {
				_ = batch.Close()
				return nil, fmt.Errorf("storageDB set: %w", err)
			}
		}
	}

	if err := writeLocalMetaToBatch(batch, version, s.perDBWorkingLtHash[storageDBDir]); err != nil {
		_ = batch.Close()
		return nil, fmt.Errorf("storageDB local meta: %w", err)
	}

	return batch, nil
}

// Prepare a batch of writes for the legacyDB.
func (s *CommitStore) prepareBatchLegacyDB(version int64) (types.Batch, error) {
	if len(s.legacyWrites) == 0 && version <= s.localMeta[legacyDBDir].CommittedVersion {
		return nil, nil
	}

	s.phaseTimer.SetPhase("commit_legacy_db_prepare")

	batch := s.legacyDB.NewBatch()

	for keyStr, lw := range s.legacyWrites {
		key := []byte(keyStr)
		if lw.IsDelete() {
			if err := batch.Delete(key); err != nil {
				_ = batch.Close()
				return nil, fmt.Errorf("legacyDB delete: %w", err)
			}
		} else {
			if err := batch.Set(key, lw.Serialize()); err != nil {
				_ = batch.Close()
				return nil, fmt.Errorf("legacyDB set: %w", err)
			}
		}
	}

	if err := writeLocalMetaToBatch(batch, version, s.perDBWorkingLtHash[legacyDBDir]); err != nil {
		_ = batch.Close()
		return nil, fmt.Errorf("legacyDB local meta: %w", err)
	}

	return batch, nil
}

// batchReadOldValues scans all changeset pairs and returns one result map per
// DB containing the "old value" for each key. Keys that already have uncommitted
// pending writes (from a prior ApplyChangeSets call in the same block) are
// resolved from those pending writes directly and excluded from the DB batch
// read, avoiding unnecessary I/O and cache pollution.
func (s *CommitStore) batchReadOldValues(cs []*proto.NamedChangeSet) (
	storageOld map[string]types.BatchGetResult,
	accountOld map[string]types.BatchGetResult,
	codeOld map[string]types.BatchGetResult,
	legacyOld map[string]types.BatchGetResult,
	err error,
) {
	storageOld = make(map[string]types.BatchGetResult)
	accountOld = make(map[string]types.BatchGetResult)
	codeOld = make(map[string]types.BatchGetResult)
	legacyOld = make(map[string]types.BatchGetResult)

	// Separate maps for keys that need a DB read (no pending write).
	storageBatch := make(map[string]types.BatchGetResult)
	accountBatch := make(map[string]types.BatchGetResult)
	codeBatch := make(map[string]types.BatchGetResult)
	legacyBatch := make(map[string]types.BatchGetResult)

	// Partition changeset keys: resolve from pending writes when available
	// (prior ApplyChangeSets call in the same block), otherwise queue for
	// a DB batch read.
	for _, namedCS := range cs {
		if namedCS.Changeset.Pairs == nil {
			continue
		}
		for _, pair := range namedCS.Changeset.Pairs {
			kind, keyBytes := evm.ParseEVMKey(pair.Key)
			switch kind {
			case evm.EVMKeyStorage:
				k := string(keyBytes)
				if _, done := storageOld[k]; done {
					continue
				}
				if pw, ok := s.storageWrites[k]; ok {
					if pw.IsDelete() {
						storageOld[k] = types.BatchGetResult{Value: nil}
					} else {
						storageOld[k] = types.BatchGetResult{Value: pw.Serialize()}
					}
				} else {
					storageBatch[k] = types.BatchGetResult{}
				}

			case evm.EVMKeyNonce, evm.EVMKeyCodeHash:
				addr, ok := AddressFromBytes(keyBytes)
				if !ok {
					continue
				}
				k := string(addr[:])
				if _, done := accountOld[k]; done {
					continue
				}
				if accountWrite, ok := s.accountWrites[k]; ok {
					accountOld[k] = types.BatchGetResult{Value: accountWrite.Serialize()}
				} else {
					accountBatch[k] = types.BatchGetResult{}
				}

			case evm.EVMKeyCode:
				k := string(keyBytes)
				if _, done := codeOld[k]; done {
					continue
				}
				if pw, ok := s.codeWrites[k]; ok {
					if pw.IsDelete() {
						codeOld[k] = types.BatchGetResult{Value: nil}
					} else {
						codeOld[k] = types.BatchGetResult{Value: pw.Serialize()}
					}
				} else {
					codeBatch[k] = types.BatchGetResult{}
				}

			case evm.EVMKeyLegacy:
				k := string(keyBytes)
				if _, done := legacyOld[k]; done {
					continue
				}
				if pw, ok := s.legacyWrites[k]; ok {
					if pw.IsDelete() {
						legacyOld[k] = types.BatchGetResult{Value: nil}
					} else {
						legacyOld[k] = types.BatchGetResult{Value: pw.Serialize()}
					}
				} else {
					legacyBatch[k] = types.BatchGetResult{}
				}
			}
		}
	}

	// Issue parallel BatchGet calls only for keys that need a DB read.
	var wg sync.WaitGroup
	var storageErr, accountErr, codeErr, legacyErr error

	if len(storageBatch) > 0 {
		wg.Add(1)
		s.miscPool.Submit(func() {
			defer wg.Done()
			storageErr = s.storageDB.BatchGet(storageBatch)
		})
	}

	if len(accountBatch) > 0 {
		wg.Add(1)
		s.miscPool.Submit(func() {
			defer wg.Done()
			accountErr = s.accountDB.BatchGet(accountBatch)
		})
	}

	if len(codeBatch) > 0 {
		wg.Add(1)
		s.miscPool.Submit(func() {
			defer wg.Done()
			codeErr = s.codeDB.BatchGet(codeBatch)
		})
	}

	if len(legacyBatch) > 0 {
		wg.Add(1)
		s.miscPool.Submit(func() {
			defer wg.Done()
			legacyErr = s.legacyDB.BatchGet(legacyBatch)
		})
	}

	wg.Wait()
	if err = errors.Join(storageErr, accountErr, codeErr, legacyErr); err != nil {
		return
	}

	// Merge DB results into the result maps, failing on any per-key errors.
	// BatchGet converts ErrNotFound into nil Value (no error), but surfaces
	// real read errors.
	for k, v := range storageBatch {
		if v.Error != nil {
			return nil, nil, nil, nil, fmt.Errorf("storageDB batch read error for key %x: %w", k, v.Error)
		}
		storageOld[k] = v
	}
	for k, v := range accountBatch {
		if v.Error != nil {
			return nil, nil, nil, nil, fmt.Errorf("accountDB batch read error for key %x: %w", k, v.Error)
		}
		accountOld[k] = v
	}
	for k, v := range codeBatch {
		if v.Error != nil {
			return nil, nil, nil, nil, fmt.Errorf("codeDB batch read error for key %x: %w", k, v.Error)
		}
		codeOld[k] = v
	}
	for k, v := range legacyBatch {
		if v.Error != nil {
			return nil, nil, nil, nil, fmt.Errorf("legacyDB batch read error for key %x: %w", k, v.Error)
		}
		legacyOld[k] = v
	}

	return
}

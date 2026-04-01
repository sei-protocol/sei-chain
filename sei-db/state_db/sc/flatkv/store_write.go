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
func (s *CommitStore) ApplyChangeSets(changeSets []*proto.NamedChangeSet) error {
	if s.readOnly {
		return errReadOnly
	}

	///////////
	// Setup //
	///////////
	s.phaseTimer.SetPhase("apply_change_sets_prepare")
	s.pendingChangeSets = append(s.pendingChangeSets, changeSets...)

	changesByType := sortChangeSets(changeSets)

	////////////////////
	// Batch Read Old //
	////////////////////
	s.phaseTimer.SetPhase("apply_change_sets_batch_read")

	storageOld, accountOld, codeOld, legacyOld, err := s.batchReadOldValues(changesByType)
	if err != nil {
		return fmt.Errorf("failed to batch read old values: %w", err)
	}

	//////////////////
	// Gather Pairs //
	//////////////////
	s.phaseTimer.SetPhase("apply_change_sets_gather_pairs")

	// Gather account pairs
	accountWrites, err := s.mergeAccountUpdates(
		changesByType[evm.EVMKeyNonce],
		changesByType[evm.EVMKeyCodeHash],
		nil, // TODO: update this when we add a balance key!
	)
	if err != nil {
		return fmt.Errorf("failed to gather account updates: %w", err)
	}
	newAccountValues := s.deriveNewAccountValues(accountWrites, accountOld)
	accountPairs := gatherLTHashPairs(newAccountValues, accountOld)

	// Gather storage pairs
	storageChanges, err := parseChanges(changesByType[evm.EVMKeyStorage], vtype.DeserializeStorageData)
	if err != nil {
		return fmt.Errorf("failed to parse storage changes: %w", err)
	}
	storagePairs := gatherLTHashPairs(storageChanges, storageOld)

	// Gather code pairs
	codeChanges, err := parseChanges(changesByType[evm.EVMKeyCode], vtype.DeserializeCodeData)
	if err != nil {
		return fmt.Errorf("failed to parse code changes: %w", err)
	}
	codePairs := gatherLTHashPairs(codeChanges, codeOld)

	// Gather legacy pairs
	legacyChanges, err := parseChanges(changesByType[evm.EVMKeyLegacy], vtype.DeserializeLegacyData)
	if err != nil {
		return fmt.Errorf("failed to parse legacy changes: %w", err)
	}
	legacyPairs := gatherLTHashPairs(legacyChanges, legacyOld)

	////////////////////
	// Compute LTHash //
	////////////////////
	s.phaseTimer.SetPhase("apply_change_compute_lt_hash")

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

// Parse into the VType.
func parseChanges[T vtype.VType](
	rawChanges map[string][]byte,
	builder vtype.VTypeBuilder[T],
) (map[string]T, error) {

	result := make(map[string]T)

	for keyStr, rawChange := range rawChanges {
		value, err := builder(rawChange)
		if err != nil {
			return nil, fmt.Errorf("failed to parse value for key %s: %w", keyStr, err)
		}
		result[keyStr] = value
	}

	return result, nil
}

// Gather LtHash pairs for a DB.
func gatherLTHashPairs[T vtype.VType](
	newValues map[string]T,
	oldValues map[string]T,
) []lthash.KVPairWithLastValue {

	var pairs []lthash.KVPairWithLastValue = make([]lthash.KVPairWithLastValue, 0, len(newValues))

	for keyStr, newValue := range newValues {
		var oldValue = oldValues[keyStr]

		var newBytes []byte
		if !newValue.IsDelete() {
			newBytes = newValue.Serialize()

		}

		var oldBytes []byte
		if !oldValue.IsDelete() {
			oldBytes = oldValue.Serialize()
		}

		pairs = append(pairs, lthash.KVPairWithLastValue{
			Key:       []byte(keyStr),
			Value:     newBytes,
			LastValue: oldBytes,
			Delete:    newValue.IsDelete(),
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

// Combine the pending account writes with prior values to determine the new account values.
//
// We need to take this step because accounts are split into multiple fields, and its possible to overwrite just a
// single field (thus requring us to copy the unmodified fields from the prior value).
func (s *CommitStore) deriveNewAccountValues(
	pendingWrites map[string]*vtype.PendingAccountWrite,
	databaseAccountData map[string]*vtype.AccountData,
) map[string]*vtype.AccountData {

	result := make(map[string]*vtype.AccountData)

	for addrStr, pendingWrite := range pendingWrites {
		var oldValue *vtype.AccountData
		if stagedWrite, ok := s.accountWrites[addrStr]; ok {
			// We've got a pending write staged in memory
			oldValue = stagedWrite
		} else if dbValue, ok := databaseAccountData[addrStr]; ok {
			// This account is in the DB
			oldValue = dbValue
		}

		newValue := pendingWrite.Merge(oldValue, s.committedVersion+1)
		result[addrStr] = newValue
	}
	return result
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
func (s *CommitStore) batchReadOldValues(changesByType map[evm.EVMKeyKind]map[string][]byte) (
	storageOld map[string]*vtype.StorageData,
	accountOld map[string]*vtype.AccountData,
	codeOld map[string]*vtype.CodeData,
	legacyOld map[string]*vtype.LegacyData,
	err error,
) {
	storageOld = make(map[string]*vtype.StorageData)
	accountOld = make(map[string]*vtype.AccountData)
	codeOld = make(map[string]*vtype.CodeData)
	legacyOld = make(map[string]*vtype.LegacyData)

	// Issue reads to each DB if we don't already have the old value in memory.
	var wg sync.WaitGroup
	var storageErr, accountErr, codeErr, legacyErr error

	// EVM storage
	storageBatch := make(map[string]types.BatchGetResult)
	for key, _ := range changesByType[evm.EVMKeyStorage] {
		if _, ok := s.storageWrites[key]; ok {
			// We've got the old value in the pending writes buffer.
			storageOld[key] = s.storageWrites[key]
		} else {
			// Schedule a read for this key.
			storageBatch[key] = types.BatchGetResult{}
		}
	}
	if len(storageBatch) > 0 {
		wg.Add(1)
		s.miscPool.Submit(func() {
			defer wg.Done()
			storageErr = s.storageDB.BatchGet(storageBatch)
		})
	}

	// Accounts
	accountBatch := make(map[string]types.BatchGetResult)
	for key, _ := range changesByType[evm.EVMKeyNonce] {
		if _, ok := s.accountWrites[key]; ok {
			// We've got the old value in the pending writes buffer.
			accountOld[key] = s.accountWrites[key]
		} else {
			// Schedule a read for this key.
			accountBatch[key] = types.BatchGetResult{}
		}
	}
	for key, _ := range changesByType[evm.EVMKeyCodeHash] {
		if _, ok := s.accountWrites[key]; ok {
			// We've got the old value in the pending writes buffer.
			accountOld[key] = s.accountWrites[key]
		} else {
			// Schedule a read for this key.
			accountBatch[key] = types.BatchGetResult{}
		}
	}
	// TODO: when we eventually add a balance key, we will need to add it to the accountBatch map here.
	if len(accountBatch) > 0 {
		wg.Add(1)
		s.miscPool.Submit(func() {
			defer wg.Done()
			accountErr = s.accountDB.BatchGet(accountBatch)
		})
	}

	// EVM bytecode
	codeBatch := make(map[string]types.BatchGetResult)
	for key, _ := range changesByType[evm.EVMKeyCode] {
		if _, ok := s.codeWrites[key]; ok {
			// We've got the old value in the pending writes buffer.
			codeOld[key] = s.codeWrites[key]
		} else {
			// Schedule a read for this key.
			codeBatch[key] = types.BatchGetResult{}
		}
	}
	if len(codeBatch) > 0 {
		wg.Add(1)
		s.miscPool.Submit(func() {
			defer wg.Done()
			codeErr = s.codeDB.BatchGet(codeBatch)
		})
	}

	// Legacy data
	legacyBatch := make(map[string]types.BatchGetResult)
	for key, _ := range changesByType[evm.EVMKeyLegacy] {
		if _, ok := s.legacyWrites[key]; ok {
			// We've got the old value in the pending writes buffer.
			legacyOld[key] = s.legacyWrites[key]
		} else {
			// Schedule a read for this key.
			legacyBatch[key] = types.BatchGetResult{}
		}
	}
	if len(legacyBatch) > 0 {
		wg.Add(1)
		s.miscPool.Submit(func() {
			defer wg.Done()
			legacyErr = s.legacyDB.BatchGet(legacyBatch)
		})
	}

	// Wait for all reads to complete.
	wg.Wait()
	if err = errors.Join(storageErr, accountErr, codeErr, legacyErr); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to batch read old values: %w", err)
	}

	// Merge DB results into the result maps.

	// Storage
	for k, v := range storageBatch {
		if v.Error != nil {
			return nil, nil, nil, nil, fmt.Errorf("storageDB batch read error for key %x: %w", k, v.Error)
		}
		if v.IsFound() {
			storageOld[k], err = vtype.DeserializeStorageData(v.Value)
			if err != nil {
				return nil, nil, nil, nil, fmt.Errorf("failed to deserialize storage data: %w", err)
			}
		}
	}

	// Accounts
	for k, v := range accountBatch {
		if v.Error != nil {
			return nil, nil, nil, nil, fmt.Errorf("accountDB batch read error for key %x: %w", k, v.Error)
		}
		if v.IsFound() {
			accountOld[k], err = vtype.DeserializeAccountData(v.Value)
			if err != nil {
				return nil, nil, nil, nil, fmt.Errorf("failed to deserialize account data: %w", err)
			}
		}
	}

	// EVM bytecode
	for k, v := range codeBatch {
		if v.Error != nil {
			return nil, nil, nil, nil, fmt.Errorf("codeDB batch read error for key %x: %w", k, v.Error)
		}
		if v.IsFound() {
			codeOld[k], err = vtype.DeserializeCodeData(v.Value)
			if err != nil {
				return nil, nil, nil, nil, fmt.Errorf("failed to deserialize code data: %w", err)
			}
		}
	}

	// Legacy data
	for k, v := range legacyBatch {
		if v.Error != nil {
			return nil, nil, nil, nil, fmt.Errorf("legacyDB batch read error for key %x: %w", k, v.Error)
		}
		if v.IsFound() {
			legacyOld[k], err = vtype.DeserializeLegacyData(v.Value)
			if err != nil {
				return nil, nil, nil, nil, fmt.Errorf("failed to deserialize legacy data: %w", err)
			}
		}
	}

	return storageOld, accountOld, codeOld, legacyOld, nil
}

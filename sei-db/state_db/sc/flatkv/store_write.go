package flatkv

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl/proto"
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

	// Collect LtHash pairs per DB (using internal key format)
	var storagePairs []lthash.KVPairWithLastValue
	var codePairs []lthash.KVPairWithLastValue
	var legacyPairs []lthash.KVPairWithLastValue
	// Account pairs are collected at the end after all account changes are processed

	// Pre-capture raw encoded account bytes so LtHash delta uses the correct
	// baseline across multiple ApplyChangeSets calls before Commit.
	// nil means the account didn't exist (no phantom MixOut for new accounts).
	oldAccountRawValues := make(map[string][]byte)

	s.phaseTimer.SetPhase("apply_change_sets_collect_storage_pairs")

	// For each entry in the change set, accumulate changes for the appropriate DB.
	for _, namedCS := range cs {
		if namedCS.Changeset.Pairs == nil {
			continue
		}

		for _, pair := range namedCS.Changeset.Pairs {
			// Parse memiavl key to determine type
			kind, keyBytes := evm.ParseEVMKey(pair.Key)
			if kind == evm.EVMKeyUnknown {
				// Skip non-EVM keys silently
				continue // NO!
			}

			// Route to appropriate DB based on key type
			switch kind {
			case evm.EVMKeyStorage:
				storagePairs = s.applyEvmStorageChange(keyBytes, pair, storageOld, storagePairs)
			case evm.EVMKeyNonce, evm.EVMKeyCodeHash:
				err := s.applyEvmAccountFieldChange(kind, keyBytes, pair, accountOld, oldAccountRawValues)
				if err != nil {
					return fmt.Errorf("failed to apply EVM account field change: %w", err)
				}
			case evm.EVMKeyCode:
				codePairs = s.applyEvmCodeChange(keyBytes, pair, codeOld, codePairs)
			case evm.EVMKeyLegacy:
				legacyPairs = s.applyEvmLegacyChange(keyBytes, pair, legacyOld, legacyPairs)
			}
		}
	}

	s.phaseTimer.SetPhase("apply_change_sets_collect_account_pairs")

	accountPairs := make([]lthash.KVPairWithLastValue, 0, len(oldAccountRawValues))
	for addrStr, oldRaw := range oldAccountRawValues {
		paw, ok := s.accountWrites[addrStr]
		if !ok {
			continue
		}

		var encodedValue []byte
		if !paw.isDelete {
			encodedValue = paw.value.Encode()
		}
		accountPairs = append(accountPairs, lthash.KVPairWithLastValue{
			Key:       AccountKey(paw.addr),
			Value:     encodedValue,
			LastValue: oldRaw,
			Delete:    paw.isDelete,
		})
	}

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

// Apply a single change to the evm storage db.
func (s *CommitStore) applyEvmStorageChange(
	// The key with the prefix stripped.
	keyBytes []byte,
	// The change to apply.
	pair *iavl.KVPair,
	// This map stores the old value to the key prior to this change. This function updates it
	// with the new value, so that the next change will see this value as the previous value.
	storageOld map[string]types.BatchGetResult,
	// This slice stores both the new and old values for each key modified in this block.
	storagePairs []lthash.KVPairWithLastValue,
) []lthash.KVPairWithLastValue {
	keyStr := string(keyBytes)
	oldValue := storageOld[keyStr].Value

	if pair.Delete {
		s.storageWrites[keyStr] = &pendingKVWrite{
			key:      keyBytes,
			isDelete: true,
		}
		storageOld[keyStr] = types.BatchGetResult{Value: nil}
	} else {
		s.storageWrites[keyStr] = &pendingKVWrite{
			key:   keyBytes,
			value: pair.Value,
		}
		storageOld[keyStr] = types.BatchGetResult{Value: pair.Value}
	}

	return append(storagePairs, lthash.KVPairWithLastValue{
		Key:       keyBytes,
		Value:     pair.Value,
		LastValue: oldValue,
		Delete:    pair.Delete,
	})
}

// Apply a single change to the evm code db.
func (s *CommitStore) applyEvmCodeChange(
	// The key with the prefix stripped (addr, 20 bytes).
	keyBytes []byte,
	// The change to apply.
	pair *iavl.KVPair,
	// This map stores the old value to the key prior to this change. This function updates it
	// with the new value, so that the next change will see this value as the previous value.
	codeOld map[string]types.BatchGetResult,
	// This slice stores both the new and old values for each key modified in this block.
	codePairs []lthash.KVPairWithLastValue,
) []lthash.KVPairWithLastValue {
	keyStr := string(keyBytes)
	oldValue := codeOld[keyStr].Value

	newCodeData := vtype.NewCodeData().SetBlockHeight(s.committedVersion + 1)

	if pair.Delete {
		newCodeData.SetBytecode([]byte{})
		codeOld[keyStr] = types.BatchGetResult{Value: nil}
	} else {
		newCodeData.SetBytecode(pair.Value)
		codeOld[keyStr] = types.BatchGetResult{Value: pair.Value}
	}

	s.codeWrites[keyStr] = newCodeData

	return append(codePairs, lthash.KVPairWithLastValue{
		Key:       keyBytes,
		Value:     pair.Value,
		LastValue: oldValue,
		Delete:    pair.Delete,
	})
}

// Apply a single change to the evm legacy db.
func (s *CommitStore) applyEvmLegacyChange(
	// The key with the prefix stripped.
	keyBytes []byte,
	// The change to apply.
	pair *iavl.KVPair,
	// This map stores the old value to the key prior to this change. This function updates it
	// with the new value, so that the next change will see this value as the previous value.
	legacyOld map[string]types.BatchGetResult,
	// This slice stores both the new and old values for each key modified in this block.
	legacyPairs []lthash.KVPairWithLastValue,
) []lthash.KVPairWithLastValue {
	keyStr := string(keyBytes)
	oldValue := legacyOld[keyStr].Value

	if pair.Delete {
		s.legacyWrites[keyStr] = &pendingKVWrite{
			key:      keyBytes,
			isDelete: true,
		}
		legacyOld[keyStr] = types.BatchGetResult{Value: nil}
	} else {
		s.legacyWrites[keyStr] = &pendingKVWrite{
			key:   keyBytes,
			value: pair.Value,
		}
		legacyOld[keyStr] = types.BatchGetResult{Value: pair.Value}
	}

	return append(legacyPairs, lthash.KVPairWithLastValue{
		Key:       keyBytes,
		Value:     pair.Value,
		LastValue: oldValue,
		Delete:    pair.Delete,
	})
}

// Apply a single nonce or codehash change to the account db.
func (s *CommitStore) applyEvmAccountFieldChange(
	// Whether this is a nonce or codehash change.
	kind evm.EVMKeyKind,
	// The key with the prefix stripped (addr, 20 bytes).
	keyBytes []byte,
	// The change to apply.
	pair *iavl.KVPair,
	// Old account values.
	accountOld map[string]types.BatchGetResult,
	// Snapshots of old encoded account bytes for LtHash delta computation.
	// This function populates entries the first time each address is seen.
	oldAccountRawValues map[string][]byte,
) error {
	addr, ok := AddressFromBytes(keyBytes)
	if !ok {
		return fmt.Errorf("invalid address length %d for key kind %d", len(keyBytes), kind)
	}
	addrStr := string(addr[:])
	addrKey := string(AccountKey(addr))

	// Snapshot the old encoded bytes the first time we touch this address,
	// so the LtHash delta uses the correct baseline across multiple
	// ApplyChangeSets calls before Commit.
	if _, seen := oldAccountRawValues[addrStr]; !seen {
		if paw, ok := s.accountWrites[addrStr]; ok {
			if paw.isDelete {
				oldAccountRawValues[addrStr] = nil
			} else {
				oldAccountRawValues[addrStr] = paw.value.Encode()
			}
		} else if result, ok := accountOld[addrKey]; ok {
			oldAccountRawValues[addrStr] = result.Value
		} else {
			oldAccountRawValues[addrStr] = nil
		}
	}

	paw := s.accountWrites[addrStr]
	if paw == nil {
		var existingValue AccountValue
		result := accountOld[addrKey]
		if result.IsFound() && result.Value != nil {
			av, err := DecodeAccountValue(result.Value)
			if err != nil {
				return fmt.Errorf("corrupted AccountValue for addr %x: %w", addr, err)
			}
			existingValue = av
		}
		paw = &pendingAccountWrite{addr: addr, value: existingValue}
		s.accountWrites[addrStr] = paw
	}

	if pair.Delete {
		if kind == evm.EVMKeyNonce {
			paw.value.Nonce = 0
		} else {
			paw.value.CodeHash = CodeHash{}
		}
		paw.isDelete = paw.value.IsEmpty()
	} else {
		if kind == evm.EVMKeyNonce {
			if len(pair.Value) != NonceLen {
				return fmt.Errorf("invalid nonce value length: got %d, expected %d",
					len(pair.Value), NonceLen)
			}
			paw.value.Nonce = binary.BigEndian.Uint64(pair.Value)
		} else {
			if len(pair.Value) != CodeHashLen {
				return fmt.Errorf("invalid codehash value length: got %d, expected %d",
					len(pair.Value), CodeHashLen)
			}
			copy(paw.value.CodeHash[:], pair.Value)
		}
		paw.isDelete = paw.value.IsEmpty()
	}
	return nil
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
		err := s.miscPool.Submit(s.ctx, func() {
			errs[i] = db.Flush()
			wg.Done()
		})
		if err != nil {
			return fmt.Errorf("failed to submit flush: %w", err)
		}
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
	s.accountWrites = make(map[string]*pendingAccountWrite)
	s.codeWrites = make(map[string]*vtype.CodeData)
	s.storageWrites = make(map[string]*pendingKVWrite)
	s.legacyWrites = make(map[string]*pendingKVWrite)
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

		for _, paw := range s.accountWrites {
			key := AccountKey(paw.addr)
			if paw.isDelete {
				if err := batch.Delete(key); err != nil {
					return fmt.Errorf("accountDB delete: %w", err)
				}
			} else {
				encoded := EncodeAccountValue(paw.value)
				if err := batch.Set(key, encoded); err != nil {
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
	pending = append(pending, pendingCommit{codeDBDir, batch})

	// Commit to codeDB, storageDB, legacyDB (identical logic per KV DB).
	kvDBs := [...]struct {
		dir    string
		phase  string
		writes map[string]*pendingKVWrite
		db     types.KeyValueDB
	}{
		{storageDBDir, "commit_storage_db_prepare", s.storageWrites, s.storageDB},
		{legacyDBDir, "commit_legacy_db_prepare", s.legacyWrites, s.legacyDB},
	}
	for _, spec := range kvDBs {
		if len(spec.writes) == 0 && version <= s.localMeta[spec.dir].CommittedVersion {
			continue
		}
		s.phaseTimer.SetPhase(spec.phase)
		batch := spec.db.NewBatch()
		defer func(b types.Batch) { _ = b.Close() }(batch)

		for _, pw := range spec.writes {
			if pw.isDelete {
				if err := batch.Delete(pw.key); err != nil {
					return fmt.Errorf("%s delete: %w", spec.dir, err)
				}
			} else {
				if err := batch.Set(pw.key, pw.value); err != nil {
					return fmt.Errorf("%s set: %w", spec.dir, err)
				}
			}
		}

		if err := writeLocalMetaToBatch(batch, version, s.perDBWorkingLtHash[spec.dir]); err != nil {
			return fmt.Errorf("%s local meta: %w", spec.dir, err)
		}
		pending = append(pending, pendingCommit{spec.dir, batch})
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
		err := s.miscPool.Submit(s.ctx, func() {
			errs[i] = p.batch.Commit(syncOpt)
			wg.Done()
		})
		if err != nil {
			return fmt.Errorf("failed to submit commit: %w", err)
		}
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

	pendingKVResult := func(pw *pendingKVWrite) types.BatchGetResult {
		if pw.isDelete {
			return types.BatchGetResult{Value: nil}
		}
		return types.BatchGetResult{Value: pw.value}
	}

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
					storageOld[k] = pendingKVResult(pw)
				} else {
					storageBatch[k] = types.BatchGetResult{}
				}

			case evm.EVMKeyNonce, evm.EVMKeyCodeHash:
				addr, ok := AddressFromBytes(keyBytes)
				if !ok {
					continue
				}
				k := string(AccountKey(addr))
				if _, done := accountOld[k]; done {
					continue
				}
				if paw, ok := s.accountWrites[k]; ok {
					accountOld[k] = types.BatchGetResult{Value: EncodeAccountValue(paw.value)}
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
					legacyOld[k] = pendingKVResult(pw)
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
		err = s.miscPool.Submit(s.ctx, func() {
			defer wg.Done()
			storageErr = s.storageDB.BatchGet(storageBatch)
		})
		if err != nil {
			err = fmt.Errorf("failed to submit batch get: %w", err)
			return
		}
	}

	if len(accountBatch) > 0 {
		wg.Add(1)
		err = s.miscPool.Submit(s.ctx, func() {
			defer wg.Done()
			accountErr = s.accountDB.BatchGet(accountBatch)
		})
		if err != nil {
			err = fmt.Errorf("failed to submit batch get: %w", err)
			return
		}
	}

	if len(codeBatch) > 0 {
		wg.Add(1)
		err = s.miscPool.Submit(s.ctx, func() {
			defer wg.Done()
			codeErr = s.codeDB.BatchGet(codeBatch)
		})
		if err != nil {
			err = fmt.Errorf("failed to submit batch get: %w", err)
			return
		}
	}

	if len(legacyBatch) > 0 {
		wg.Add(1)
		err = s.miscPool.Submit(s.ctx, func() {
			defer wg.Done()
			legacyErr = s.legacyDB.BatchGet(legacyBatch)
		})
		if err != nil {
			err = fmt.Errorf("failed to submit batch get: %w", err)
			return
		}
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
			err = fmt.Errorf("storageDB batch read error for key %x: %w", k, v.Error)
			return
		}
		storageOld[k] = v
	}
	for k, v := range accountBatch {
		if v.Error != nil {
			err = fmt.Errorf("accountDB batch read error for key %x: %w", k, v.Error)
			return
		}
		accountOld[k] = v
	}
	for k, v := range codeBatch {
		if v.Error != nil {
			err = fmt.Errorf("codeDB batch read error for key %x: %w", k, v.Error)
			return
		}
		codeOld[k] = v
	}
	for k, v := range legacyBatch {
		if v.Error != nil {
			err = fmt.Errorf("legacyDB batch read error for key %x: %w", k, v.Error)
			return
		}
		legacyOld[k] = v
	}

	return
}

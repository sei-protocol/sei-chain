package flatkv

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/dbcache"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// accountFieldChanges collects nonce/codehash mutations for a single address
// within one ApplyChangeSets call. Fields are applied on top of the old
// AccountValue read from the snapshot.
type accountFieldChanges struct {
	addr    Address
	updates []accountFieldUpdate
}

type accountFieldUpdate struct {
	kind   evm.EVMKeyKind
	value  []byte
	delete bool
}

// ApplyChangeSets writes EVM changesets to the cache layer and updates LtHash.
//
// LtHash is computed based on actual storage format (internal keys):
// - storageDB: key=addr||slot, value=storage_value
// - accountDB: key=addr, value=AccountValue (balance(32)||nonce(8)||codehash(32))
// - codeDB: key=addr, value=bytecode
// - legacyDB: key=full original key (with prefix), value=raw value
func (s *CommitStore) ApplyChangeSets(cs []*proto.NamedChangeSet) (retErr error) {
	if s.readOnly {
		return errReadOnly
	}

	// ---------------------------------------------------------------
	// 1. Snapshot all data DBs to capture pre-write state for LtHash.
	// ---------------------------------------------------------------
	s.phaseTimer.SetPhase("snapshot")

	storageSnap, err := s.storageDB.Snapshot()
	if err != nil {
		return fmt.Errorf("storage snapshot: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, storageSnap.Release()) }()

	accountSnap, err := s.accountDB.Snapshot()
	if err != nil {
		return fmt.Errorf("account snapshot: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, accountSnap.Release()) }()

	codeSnap, err := s.codeDB.Snapshot()
	if err != nil {
		return fmt.Errorf("code snapshot: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, codeSnap.Release()) }()

	legacySnap, err := s.legacyDB.Snapshot()
	if err != nil {
		return fmt.Errorf("legacy snapshot: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, legacySnap.Release()) }()

	// ---------------------------------------------------------------
	// 2. Parse changeset: sort by DB, dedup, collect old-value keys.
	// ---------------------------------------------------------------
	s.phaseTimer.SetPhase("apply_parse")

	// storage/code/legacy: last write per key wins.
	storageUpdates := make(map[string]dbcache.CacheUpdate)
	codeUpdates := make(map[string]dbcache.CacheUpdate)
	legacyUpdates := make(map[string]dbcache.CacheUpdate)

	// accounts: merge field-level changes per address.
	accountChanges := make(map[string]*accountFieldChanges)

	// Keys to batch-read from snapshots for old values.
	storageOldKeys := make(map[string]types.BatchGetResult)
	codeOldKeys := make(map[string]types.BatchGetResult)
	legacyOldKeys := make(map[string]types.BatchGetResult)
	accountOldKeys := make(map[string]types.BatchGetResult)

	for _, namedCS := range cs {
		if namedCS.Changeset.Pairs == nil {
			continue
		}

		for _, pair := range namedCS.Changeset.Pairs {
			kind, keyBytes := evm.ParseEVMKey(pair.Key)
			if kind == evm.EVMKeyUnknown {
				continue
			}

			switch kind {
			case evm.EVMKeyStorage:
				keyStr := string(keyBytes)
				var value []byte
				if !pair.Delete {
					value = pair.Value
				}
				storageUpdates[keyStr] = dbcache.CacheUpdate{Key: keyBytes, Value: value}
				if _, exists := storageOldKeys[keyStr]; !exists {
					storageOldKeys[keyStr] = types.BatchGetResult{}
				}

			case evm.EVMKeyNonce, evm.EVMKeyCodeHash:
				addr, ok := AddressFromBytes(keyBytes)
				if !ok {
					return fmt.Errorf("invalid address length %d for key kind %d", len(keyBytes), kind)
				}
				addrStr := string(addr[:])
				afc := accountChanges[addrStr]
				if afc == nil {
					afc = &accountFieldChanges{addr: addr}
					accountChanges[addrStr] = afc
				}
				afc.updates = append(afc.updates, accountFieldUpdate{
					kind:   kind,
					value:  pair.Value,
					delete: pair.Delete,
				})
				addrKey := string(AccountKey(addr))
				if _, exists := accountOldKeys[addrKey]; !exists {
					accountOldKeys[addrKey] = types.BatchGetResult{}
				}

			case evm.EVMKeyCode:
				keyStr := string(keyBytes)
				var value []byte
				if !pair.Delete {
					value = pair.Value
				}
				codeUpdates[keyStr] = dbcache.CacheUpdate{Key: keyBytes, Value: value}
				if _, exists := codeOldKeys[keyStr]; !exists {
					codeOldKeys[keyStr] = types.BatchGetResult{}
				}

			case evm.EVMKeyLegacy:
				keyStr := string(keyBytes)
				var value []byte
				if !pair.Delete {
					value = pair.Value
				}
				legacyUpdates[keyStr] = dbcache.CacheUpdate{Key: keyBytes, Value: value}
				if _, exists := legacyOldKeys[keyStr]; !exists {
					legacyOldKeys[keyStr] = types.BatchGetResult{}
				}
			}
		}
	}

	// ---------------------------------------------------------------
	// 3. Batch read old values from snapshots in parallel.
	// ---------------------------------------------------------------
	s.phaseTimer.SetPhase("apply_batch_read_old")

	var wg sync.WaitGroup
	var storageReadErr, accountReadErr, codeReadErr, legacyReadErr error

	if len(storageOldKeys) > 0 {
		wg.Add(1)
		if err := s.miscPool.Submit(s.ctx, func() {
			defer wg.Done()
			storageReadErr = storageSnap.BatchGet(storageOldKeys)
		}); err != nil {
			return fmt.Errorf("submit storage batch read: %w", err)
		}
	}
	if len(accountOldKeys) > 0 {
		wg.Add(1)
		if err := s.miscPool.Submit(s.ctx, func() {
			defer wg.Done()
			accountReadErr = accountSnap.BatchGet(accountOldKeys)
		}); err != nil {
			return fmt.Errorf("submit account batch read: %w", err)
		}
	}
	if len(codeOldKeys) > 0 {
		wg.Add(1)
		if err := s.miscPool.Submit(s.ctx, func() {
			defer wg.Done()
			codeReadErr = codeSnap.BatchGet(codeOldKeys)
		}); err != nil {
			return fmt.Errorf("submit code batch read: %w", err)
		}
	}
	if len(legacyOldKeys) > 0 {
		wg.Add(1)
		if err := s.miscPool.Submit(s.ctx, func() {
			defer wg.Done()
			legacyReadErr = legacySnap.BatchGet(legacyOldKeys)
		}); err != nil {
			return fmt.Errorf("submit legacy batch read: %w", err)
		}
	}

	wg.Wait()
	if err := errors.Join(storageReadErr, accountReadErr, codeReadErr, legacyReadErr); err != nil {
		return fmt.Errorf("batch read old values: %w", err)
	}

	// ---------------------------------------------------------------
	// 4. Process accounts: decode old, apply field changes, encode new.
	// ---------------------------------------------------------------
	s.phaseTimer.SetPhase("apply_process_accounts")

	accountCacheUpdates := make([]dbcache.CacheUpdate, 0, len(accountChanges))
	accountPairs := make([]lthash.KVPairWithLastValue, 0, len(accountChanges))

	for _, afc := range accountChanges {
		var oldEncoded []byte
		var av AccountValue
		if result := accountOldKeys[string(AccountKey(afc.addr))]; result.IsFound() {
			oldEncoded = result.Value
			decoded, err := DecodeAccountValue(result.Value)
			if err != nil {
				return fmt.Errorf("corrupted AccountValue for addr %x: %w", afc.addr, err)
			}
			av = decoded
		}

		for _, u := range afc.updates {
			if u.delete {
				if u.kind == evm.EVMKeyNonce {
					av.ClearNonce()
				} else {
					av.ClearCodeHash()
				}
			} else {
				if u.kind == evm.EVMKeyNonce {
					if len(u.value) != NonceLen {
						return fmt.Errorf("invalid nonce value length: got %d, expected %d",
							len(u.value), NonceLen)
					}
					av.Nonce = binary.BigEndian.Uint64(u.value)
				} else {
					if len(u.value) != CodeHashLen {
						return fmt.Errorf("invalid codehash value length: got %d, expected %d",
							len(u.value), CodeHashLen)
					}
					copy(av.CodeHash[:], u.value)
				}
			}
		}

		newEncoded := EncodeAccountValue(av)
		accountCacheUpdates = append(accountCacheUpdates, dbcache.CacheUpdate{
			Key:   AccountKey(afc.addr),
			Value: newEncoded,
		})
		accountPairs = append(accountPairs, lthash.KVPairWithLastValue{
			Key:       AccountKey(afc.addr),
			Value:     newEncoded,
			LastValue: oldEncoded,
			Delete:    false, // account rows are never physically deleted
		})
	}

	// ---------------------------------------------------------------
	// 5. Write to all caches via BatchSet.
	// ---------------------------------------------------------------
	s.phaseTimer.SetPhase("apply_write_caches")

	if len(storageUpdates) > 0 {
		updates := make([]dbcache.CacheUpdate, 0, len(storageUpdates))
		for _, u := range storageUpdates {
			updates = append(updates, u)
		}
		if err := s.storageDB.BatchSet(updates); err != nil {
			return fmt.Errorf("storage batch set: %w", err)
		}
	}
	if len(accountCacheUpdates) > 0 {
		if err := s.accountDB.BatchSet(accountCacheUpdates); err != nil {
			return fmt.Errorf("account batch set: %w", err)
		}
	}
	if len(codeUpdates) > 0 {
		updates := make([]dbcache.CacheUpdate, 0, len(codeUpdates))
		for _, u := range codeUpdates {
			updates = append(updates, u)
		}
		if err := s.codeDB.BatchSet(updates); err != nil {
			return fmt.Errorf("code batch set: %w", err)
		}
	}
	if len(legacyUpdates) > 0 {
		updates := make([]dbcache.CacheUpdate, 0, len(legacyUpdates))
		for _, u := range legacyUpdates {
			updates = append(updates, u)
		}
		if err := s.legacyDB.BatchSet(updates); err != nil {
			return fmt.Errorf("legacy batch set: %w", err)
		}
	}

	// ---------------------------------------------------------------
	// 6. Compute LtHash from old (snapshot) and new (changeset) values.
	// ---------------------------------------------------------------
	s.phaseTimer.SetPhase("apply_compute_lt_hash")

	var allPairs []lthash.KVPairWithLastValue

	for keyStr, update := range storageUpdates {
		allPairs = append(allPairs, lthash.KVPairWithLastValue{
			Key:       update.Key,
			Value:     update.Value,
			LastValue: storageOldKeys[keyStr].Value,
			Delete:    update.IsDelete(),
		})
	}

	allPairs = append(allPairs, accountPairs...)

	for keyStr, update := range codeUpdates {
		allPairs = append(allPairs, lthash.KVPairWithLastValue{
			Key:       update.Key,
			Value:     update.Value,
			LastValue: codeOldKeys[keyStr].Value,
			Delete:    update.IsDelete(),
		})
	}

	for keyStr, update := range legacyUpdates {
		allPairs = append(allPairs, lthash.KVPairWithLastValue{
			Key:       update.Key,
			Value:     update.Value,
			LastValue: legacyOldKeys[keyStr].Value,
			Delete:    update.IsDelete(),
		})
	}

	if len(allPairs) > 0 {
		newLtHash, _ := lthash.ComputeLtHash(s.workingLtHash, allPairs)
		s.workingLtHash = newLtHash
	}

	// ---------------------------------------------------------------
	// 7. Append to pending changesets (for WAL).
	// ---------------------------------------------------------------
	s.pendingChangeSets = append(s.pendingChangeSets, cs...)

	s.phaseTimer.SetPhase("apply_done")
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

// clearPendingWrites clears pending changeset buffers.
func (s *CommitStore) clearPendingWrites() {
	s.pendingChangeSets = make([]*proto.NamedChangeSet, 0)
}

// flushAllDBs is a no-op stub. The cache layer handles flushing via GC;
// this is retained for callers (e.g. catchup) that still reference it.
//
// TODO: remove once catchup is updated to use the cache layer directly.
func (s *CommitStore) flushAllDBs() error {
	return nil
}

// commitBatches updates LocalMeta version markers in each data DB.
// Data writes are handled by ApplyChangeSets via the cache layer;
// this function only persists the version watermark for crash recovery.
//
// TODO: revisit LocalMeta semantics — with the cache layer handling writes,
// the atomic data+meta batch guarantee no longer applies. Consider whether
// LocalMeta should move into the cache or be replaced by a different mechanism.
func (s *CommitStore) commitBatches(version int64) error {
	type pendingCommit struct {
		dbDir string
		db    dbcache.Cache
	}

	allDBs := []pendingCommit{
		{accountDBDir, s.accountDB},
		{codeDBDir, s.codeDB},
		{storageDBDir, s.storageDB},
		{legacyDBDir, s.legacyDB},
	}

	for _, p := range allDBs {
		if version <= s.localMeta[p.dbDir].CommittedVersion {
			continue
		}
		newLocalMeta := &LocalMeta{CommittedVersion: version}
		p.db.Set(DBLocalMetaKey, MarshalLocalMeta(newLocalMeta))
		s.localMeta[p.dbDir] = newLocalMeta
	}

	return nil
}

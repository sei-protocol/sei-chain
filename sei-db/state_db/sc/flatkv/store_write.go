package flatkv

import (
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/dbcache"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// hashWorkItem is sent from the main thread to the hash worker goroutine via
// hashWorkChan. It carries everything needed to complete the second half of
// ApplyChangeSets asynchronously: reading old values for non-account DBs from
// snapshots, computing the LtHash delta, persisting per-DB hashes, and
// releasing the snapshots.
type hashWorkItem struct {
	// Changeset grouped by DB. Account read keys are already filled (read on
	// main thread); storage/code/legacy read keys have keys populated but
	// values not yet read -- the worker performs those BatchGet calls.
	parsed changesetByDB

	// Merged account values (nonce + codehash applied to old value), used for
	// LtHash account pairs.
	accounts map[string]mergedAccount

	// Point-in-time DB snapshots for batch reads and SetHash. Ownership is
	// exclusive to the worker; it must Release all snapshots when done.
	snapshots map[string]dbcache.CacheSnapshot

	// Receives the 32-byte Blake3 root hash or an error.
	resultCh chan HashResult
}

// accountFieldOp represents a single nonce or codehash change for an address.
type accountFieldOp struct {
	kind     evm.EVMKeyKind // EVMKeyNonce or EVMKeyCodeHash
	value    []byte         // nil when isDelete is true
	isDelete bool
}

// changesetByDB holds changeset data grouped by DB.
type changesetByDB struct {
	// Key = internal DB key as string. Last write wins for duplicate keys.
	storageUpdates map[string]dbcache.CacheUpdate
	codeUpdates    map[string]dbcache.CacheUpdate
	legacyUpdates  map[string]dbcache.CacheUpdate

	// Key = addr string, value = ordered list of field-level operations.
	accountOps map[string][]accountFieldOp

	// Read keys for snapshot BatchGet (populated during parse, filled during read).
	storageReadKeys map[string]types.BatchGetResult
	accountReadKeys map[string]types.BatchGetResult
	codeReadKeys    map[string]types.BatchGetResult
	legacyReadKeys  map[string]types.BatchGetResult
}

// mergedAccount is the result of applying field-level ops to an existing account value.
type mergedAccount struct {
	addr   Address
	update dbcache.CacheUpdate
	oldRaw []byte // raw encoded bytes from snapshot (nil if account didn't exist)
}

// ApplyChangeSets writes EVM changesets through the cache interface and
// asynchronously computes the LtHash.
//
// The main thread performs: snapshot, parse, account-only batch read, merge
// accounts, and cache writes. It then enqueues the remaining work (non-account
// batch reads, LtHash computation, snapshot SetHash/Release) onto a background
// goroutine.
//
// Returns a channel that will receive exactly one HashResult containing the
// 32-byte Blake3 root hash (or an error from the hash worker).
func (s *CommitStore) ApplyChangeSets(cs []*proto.NamedChangeSet) (_ <-chan HashResult, retErr error) {
	if s.readOnly {
		return nil, errReadOnly
	}

	// Phase 1: Snapshot all DBs.
	s.phaseTimer.SetPhase("apply_snapshot")
	snapshots, err := s.snapshotDBs()
	if err != nil {
		return nil, fmt.Errorf("snapshot DBs: %w", err)
	}
	defer func() {
		if retErr != nil {
			s.releaseSnapshots(snapshots)
		}
	}()

	// Phase 2: Parse and group changesets by DB.
	s.phaseTimer.SetPhase("apply_parse")
	parsed, err := s.parseChangeSets(cs)
	if err != nil {
		return nil, fmt.Errorf("parse changesets: %w", err)
	}

	// Phase 3: Batch read old account values only (needed for merge).
	// Storage/code/legacy reads are deferred to the hash worker.
	s.phaseTimer.SetPhase("apply_snapshot_read")
	if len(parsed.accountReadKeys) > 0 {
		if err := snapshots[accountDBDir].BatchGet(parsed.accountReadKeys); err != nil {
			return nil, fmt.Errorf("account batch read: %w", err)
		}
	}

	// Phase 4: Merge account field changes with old account values.
	s.phaseTimer.SetPhase("apply_account_merge")
	accounts, err := s.mergeAccountChanges(&parsed)
	if err != nil {
		return nil, fmt.Errorf("merge accounts: %w", err)
	}

	// Phase 5: Write all changes to caches.
	s.phaseTimer.SetPhase("apply_cache_write")
	if err := s.writeToCaches(&parsed, accounts); err != nil {
		return nil, fmt.Errorf("cache write: %w", err)
	}

	// Phase 6: Enqueue hash work to background goroutine.
	// The worker will: batch-read storage/code/legacy, compute LtHash,
	// SetHash on snapshots, and release them.
	s.phaseTimer.SetPhase("apply_enqueue_hash")
	resultCh := make(chan HashResult, 1)
	s.hashWG.Add(1)
	s.hashWorkChan <- hashWorkItem{
		parsed:    parsed,
		accounts:  accounts,
		snapshots: snapshots,
		resultCh:  resultCh,
	}
	snapshots = nil // ownership transferred to worker; prevent double-release in defer

	s.pendingChangeSets = append(s.pendingChangeSets, cs...)

	s.phaseTimer.SetPhase("apply_done")

	return resultCh, nil
}

// snapshotDBs takes a snapshot of every DB cache (data DBs + metadata).
func (s *CommitStore) snapshotDBs() (map[string]dbcache.CacheSnapshot, error) {
	snapshots := make(map[string]dbcache.CacheSnapshot, 5)

	type dbEntry struct {
		dir   string
		cache dbcache.Cache
	}
	dbs := [5]dbEntry{
		{accountDBDir, s.accountDB},
		{codeDBDir, s.codeDB},
		{storageDBDir, s.storageDB},
		{legacyDBDir, s.legacyDB},
		{metadataDir, s.metadataDB},
	}

	for _, db := range dbs {
		snap, err := db.cache.Snapshot()
		if err != nil {
			for _, acquired := range snapshots {
				_ = acquired.Release()
			}
			return nil, fmt.Errorf("snapshot %s: %w", db.dir, err)
		}
		snapshots[db.dir] = snap
	}

	return snapshots, nil
}

// releaseSnapshots releases all snapshots in the map. Errors are logged but not returned.
func (s *CommitStore) releaseSnapshots(snapshots map[string]dbcache.CacheSnapshot) {
	for dir, snap := range snapshots {
		if err := snap.Release(); err != nil {
			logger.Error("failed to release snapshot", "db", dir, "err", err)
		}
	}
}

// parseChangeSets iterates all changeset pairs and groups them by target DB.
func (s *CommitStore) parseChangeSets(cs []*proto.NamedChangeSet) (changesetByDB, error) {
	p := changesetByDB{
		storageUpdates:  make(map[string]dbcache.CacheUpdate),
		codeUpdates:     make(map[string]dbcache.CacheUpdate),
		legacyUpdates:   make(map[string]dbcache.CacheUpdate),
		accountOps:      make(map[string][]accountFieldOp),
		storageReadKeys: make(map[string]types.BatchGetResult),
		accountReadKeys: make(map[string]types.BatchGetResult),
		codeReadKeys:    make(map[string]types.BatchGetResult),
		legacyReadKeys:  make(map[string]types.BatchGetResult),
	}

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
				p.storageUpdates[keyStr] = dbcache.CacheUpdate{Key: keyBytes, Value: value}
				p.storageReadKeys[keyStr] = types.BatchGetResult{}

			case evm.EVMKeyNonce:
				if !pair.Delete {
					if len(pair.Value) != NonceLen {
						return p, fmt.Errorf("invalid nonce value length: got %d, expected %d",
							len(pair.Value), NonceLen)
					}
				}
				addr, ok := AddressFromBytes(keyBytes)
				if !ok {
					return p, fmt.Errorf("invalid address length %d for nonce key", len(keyBytes))
				}
				addrStr := string(addr[:])
				p.accountOps[addrStr] = append(p.accountOps[addrStr], accountFieldOp{
					kind:     kind,
					value:    pair.Value,
					isDelete: pair.Delete,
				})
				p.accountReadKeys[addrStr] = types.BatchGetResult{}

			case evm.EVMKeyCodeHash:
				if !pair.Delete {
					if len(pair.Value) != CodeHashLen {
						return p, fmt.Errorf("invalid codehash value length: got %d, expected %d",
							len(pair.Value), CodeHashLen)
					}
				}
				addr, ok := AddressFromBytes(keyBytes)
				if !ok {
					return p, fmt.Errorf("invalid address length %d for codehash key", len(keyBytes))
				}
				addrStr := string(addr[:])
				p.accountOps[addrStr] = append(p.accountOps[addrStr], accountFieldOp{
					kind:     kind,
					value:    pair.Value,
					isDelete: pair.Delete,
				})
				p.accountReadKeys[addrStr] = types.BatchGetResult{}

			case evm.EVMKeyCode:
				keyStr := string(keyBytes)
				var value []byte
				if !pair.Delete {
					value = pair.Value
				}
				p.codeUpdates[keyStr] = dbcache.CacheUpdate{Key: keyBytes, Value: value}
				p.codeReadKeys[keyStr] = types.BatchGetResult{}

			case evm.EVMKeyLegacy:
				keyStr := string(keyBytes)
				var value []byte
				if !pair.Delete {
					value = pair.Value
				}
				p.legacyUpdates[keyStr] = dbcache.CacheUpdate{Key: keyBytes, Value: value}
				p.legacyReadKeys[keyStr] = types.BatchGetResult{}
			}
		}
	}

	return p, nil
}

// snapshotBatchRead issues parallel BatchGet calls against data DB snapshots.
func (s *CommitStore) snapshotBatchRead(
	snapshots map[string]dbcache.CacheSnapshot,
	p *changesetByDB,
) error {
	type readJob struct {
		dir  string
		keys map[string]types.BatchGetResult
	}

	jobs := [4]readJob{
		{storageDBDir, p.storageReadKeys},
		{accountDBDir, p.accountReadKeys},
		{codeDBDir, p.codeReadKeys},
		{legacyDBDir, p.legacyReadKeys},
	}

	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i, job := range jobs {
		if len(job.keys) == 0 {
			continue
		}
		wg.Add(1)
		err := s.miscPool.Submit(s.ctx, func() {
			defer wg.Done()
			errs[i] = snapshots[job.dir].BatchGet(job.keys)
		})
		if err != nil {
			return fmt.Errorf("submit %s batch read: %w", job.dir, err)
		}
	}
	wg.Wait()

	for i, job := range jobs {
		if errs[i] != nil {
			return fmt.Errorf("%s batch read: %w", job.dir, errs[i])
		}
	}
	return nil
}

// snapshotBatchReadNonAccounts issues parallel BatchGet calls against the
// storage, code, and legacy DB snapshots. Called by the hash worker goroutine.
func (s *CommitStore) snapshotBatchReadNonAccounts(
	snapshots map[string]dbcache.CacheSnapshot,
	p *changesetByDB,
) error {
	type readJob struct {
		dir  string
		keys map[string]types.BatchGetResult
	}

	jobs := [3]readJob{
		{storageDBDir, p.storageReadKeys},
		{codeDBDir, p.codeReadKeys},
		{legacyDBDir, p.legacyReadKeys},
	}

	var wg sync.WaitGroup
	errs := make([]error, 3)
	for i, job := range jobs {
		if len(job.keys) == 0 {
			continue
		}
		wg.Add(1)
		err := s.miscPool.Submit(s.ctx, func() {
			defer wg.Done()
			errs[i] = snapshots[job.dir].BatchGet(job.keys)
		})
		if err != nil {
			return fmt.Errorf("submit %s batch read: %w", job.dir, err)
		}
	}
	wg.Wait()

	for i, job := range jobs {
		if errs[i] != nil {
			return fmt.Errorf("%s batch read: %w", job.dir, errs[i])
		}
	}
	return nil
}

// hashWorkerLoop processes hash work items from hashWorkChan sequentially.
// It is the sole writer of perDBWorkingLtHash and workingLtHash.
func (s *CommitStore) hashWorkerLoop() {
	defer close(s.hashDone)
	for item := range s.hashWorkChan {
		result := s.processHashWorkItem(item)
		item.resultCh <- result
		s.hashWG.Done()
	}
}

// processHashWorkItem reads old values for non-account DBs, computes LtHash,
// sets per-DB hashes on snapshots, and releases all snapshots.
func (s *CommitStore) processHashWorkItem(item hashWorkItem) HashResult {
	if err := s.snapshotBatchReadNonAccounts(item.snapshots, &item.parsed); err != nil {
		s.releaseSnapshots(item.snapshots)
		return HashResult{Err: fmt.Errorf("hash worker batch read: %w", err)}
	}

	s.computePerDBLtHash(&item.parsed, item.accounts)

	for dir, snap := range item.snapshots {
		if dir == metadataDir {
			continue
		}
		if err := snap.SetHash(s.perDBWorkingLtHash[dir].Marshal()); err != nil {
			s.releaseSnapshots(item.snapshots)
			return HashResult{Err: fmt.Errorf("hash worker set hash for %s: %w", dir, err)}
		}
	}

	s.releaseSnapshots(item.snapshots)

	checksum := s.workingLtHash.Checksum()
	return HashResult{Hash: checksum[:]}
}

// mergeAccountChanges applies field-level nonce/codehash operations to old account values.
func (s *CommitStore) mergeAccountChanges(p *changesetByDB) (map[string]mergedAccount, error) {
	result := make(map[string]mergedAccount, len(p.accountOps))

	for addrStr, ops := range p.accountOps {
		addr, ok := AddressFromBytes([]byte(addrStr))
		if !ok {
			return nil, fmt.Errorf("invalid address in account ops")
		}

		var av AccountValue
		readResult := p.accountReadKeys[addrStr]
		var oldRaw []byte
		if readResult.IsFound() && readResult.Value != nil {
			oldRaw = readResult.Value
			decoded, err := DecodeAccountValue(readResult.Value)
			if err != nil {
				return nil, fmt.Errorf("corrupted AccountValue for addr %x: %w", addr, err)
			}
			av = decoded
		}

		for _, op := range ops {
			if op.isDelete {
				if op.kind == evm.EVMKeyNonce {
					av.ClearNonce()
				} else {
					av.ClearCodeHash()
				}
			} else {
				if op.kind == evm.EVMKeyNonce {
					av.Nonce = binary.BigEndian.Uint64(op.value)
				} else {
					copy(av.CodeHash[:], op.value)
				}
			}
		}

		key := AccountKey(addr)
		result[addrStr] = mergedAccount{
			addr: addr,
			update: dbcache.CacheUpdate{
				Key:   key,
				Value: EncodeAccountValue(av),
			},
			oldRaw: oldRaw,
		}
	}

	return result, nil
}

// writeToCaches writes all parsed updates and merged accounts to their respective caches.
func (s *CommitStore) writeToCaches(p *changesetByDB, accounts map[string]mergedAccount) error {
	type writeJob struct {
		dir     string
		cache   dbcache.Cache
		updates []dbcache.CacheUpdate
	}

	storageUpdates := mapValues(p.storageUpdates)
	codeUpdates := mapValues(p.codeUpdates)
	legacyUpdates := mapValues(p.legacyUpdates)

	accountUpdates := make([]dbcache.CacheUpdate, 0, len(accounts))
	for _, ma := range accounts {
		accountUpdates = append(accountUpdates, ma.update)
	}

	jobs := [4]writeJob{
		{storageDBDir, s.storageDB, storageUpdates},
		{accountDBDir, s.accountDB, accountUpdates},
		{codeDBDir, s.codeDB, codeUpdates},
		{legacyDBDir, s.legacyDB, legacyUpdates},
	}

	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i, job := range jobs {
		if len(job.updates) == 0 {
			continue
		}
		wg.Add(1)
		err := s.miscPool.Submit(s.ctx, func() {
			defer wg.Done()
			errs[i] = job.cache.BatchSet(job.updates)
		})
		if err != nil {
			return fmt.Errorf("submit %s batch set: %w", job.dir, err)
		}
	}
	wg.Wait()

	for i, job := range jobs {
		if errs[i] != nil {
			return fmt.Errorf("%s batch set: %w", job.dir, errs[i])
		}
	}
	return nil
}

// computePerDBLtHash updates per-DB working LtHash and recomputes the global hash.
func (s *CommitStore) computePerDBLtHash(p *changesetByDB, accounts map[string]mergedAccount) {
	// Storage pairs
	storagePairs := make([]lthash.KVPairWithLastValue, 0, len(p.storageUpdates))
	for keyStr, update := range p.storageUpdates {
		storagePairs = append(storagePairs, lthash.KVPairWithLastValue{
			Key:       update.Key,
			Value:     update.Value,
			LastValue: p.storageReadKeys[keyStr].Value,
			Delete:    update.IsDelete(),
		})
	}

	// Code pairs
	codePairs := make([]lthash.KVPairWithLastValue, 0, len(p.codeUpdates))
	for keyStr, update := range p.codeUpdates {
		codePairs = append(codePairs, lthash.KVPairWithLastValue{
			Key:       update.Key,
			Value:     update.Value,
			LastValue: p.codeReadKeys[keyStr].Value,
			Delete:    update.IsDelete(),
		})
	}

	// Legacy pairs
	legacyPairs := make([]lthash.KVPairWithLastValue, 0, len(p.legacyUpdates))
	for keyStr, update := range p.legacyUpdates {
		legacyPairs = append(legacyPairs, lthash.KVPairWithLastValue{
			Key:       update.Key,
			Value:     update.Value,
			LastValue: p.legacyReadKeys[keyStr].Value,
			Delete:    update.IsDelete(),
		})
	}

	// Account pairs (never physically deleted)
	// TODO before merge: LLM refactor has reverted new account deletion semantics, do not merge before fixing!!!
	accountPairs := make([]lthash.KVPairWithLastValue, 0, len(accounts))
	for _, ma := range accounts {
		accountPairs = append(accountPairs, lthash.KVPairWithLastValue{
			Key:       ma.update.Key,
			Value:     ma.update.Value,
			LastValue: ma.oldRaw,
			Delete:    false,
		})
	}

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

	globalHash := lthash.New()
	for _, dir := range dataDBDirs {
		globalHash.MixIn(s.perDBWorkingLtHash[dir])
	}
	s.workingLtHash = globalHash
}

// mapValues extracts values from a map into a slice.
func mapValues(m map[string]dbcache.CacheUpdate) []dbcache.CacheUpdate {
	if len(m) == 0 {
		return nil
	}
	result := make([]dbcache.CacheUpdate, 0, len(m))
	for _, v := range m {
		result = append(result, v)
	}
	return result
}

// Commit persists buffered writes and advances the version.
// Protocol: WAL -> per-DB LocalMeta (all DBs including metadata) -> done.
// On crash, catchup replays WAL to recover incomplete commits.
func (s *CommitStore) Commit() (int64, error) {
	// TODO before merge: fix this!

	s.clearPendingWrites()
	return 0, nil
	// s.phaseTimer.SetPhase("commit_preamble")
	// if s.readOnly {
	// 	return 0, errReadOnly
	// }
	// version := s.committedVersion + 1

	// // Step 1: Write Changelog (WAL) - source of truth (always sync)
	// s.phaseTimer.SetPhase("commit_write_changelog")
	// changelogEntry := proto.ChangelogEntry{
	// 	Version:    version,
	// 	Changesets: s.pendingChangeSets,
	// }
	// if err := s.changelog.Write(changelogEntry); err != nil {
	// 	return 0, fmt.Errorf("changelog write: %w", err)
	// }

	// // Step 2: Commit LocalMeta to each DB (data DBs + metadata DB)
	// if err := s.commitBatches(version); err != nil {
	// 	return 0, fmt.Errorf("db commit: %w", err)
	// }

	// // Step 3: Update in-memory committed state
	// s.phaseTimer.SetPhase("commit_update_lt_hash")
	// s.committedVersion = version
	// s.committedLtHash = s.workingLtHash.Clone()

	// // Step 4: Clear pending buffers
	// s.phaseTimer.SetPhase("commit_clear_pending_writes")
	// s.clearPendingWrites()

	// // Periodic snapshot so WAL stays bounded and restarts are fast.
	// if s.config.SnapshotInterval > 0 && version%int64(s.config.SnapshotInterval) == 0 {
	// 	s.phaseTimer.SetPhase("commit_write_snapshot")
	// 	if err := s.WriteSnapshot(""); err != nil {
	// 		logger.Error("auto snapshot failed", "version", version, "err", err)
	// 	}
	// }

	// // Best-effort WAL truncation, throttled to amortize ReadDir cost.
	// if version%1000 == 0 {
	// 	s.tryTruncateWAL()
	// }

	// s.phaseTimer.SetPhase("commit_done")
	// logger.Info("Committed version", "version", version)
	// return version, nil
}

// commitBatches writes LocalMeta (version + LtHash) to every DB.
// Data DBs get their per-DB LtHash; the metadata DB gets the global LtHash.
// Data writes are handled by the cache; this only persists crash-recovery metadata.
func (s *CommitStore) commitBatches(version int64) error {
	type dbMeta struct {
		dir   string
		cache dbcache.Cache
	}

	dbs := [5]dbMeta{
		{accountDBDir, s.accountDB},
		{codeDBDir, s.codeDB},
		{storageDBDir, s.storageDB},
		{legacyDBDir, s.legacyDB},
		{metadataDir, s.metadataDB},
	}

	s.phaseTimer.SetPhase("commit_local_meta")

	for _, db := range dbs {
		db.cache.Set(metaVersionKey, versionToBytes(version))

		var h *lthash.LtHash
		if db.dir == metadataDir {
			h = s.workingLtHash
		} else {
			h = s.perDBWorkingLtHash[db.dir]
		}
		if h != nil {
			db.cache.Set(metaLtHashKey, h.Marshal())
		}
		s.localMeta[db.dir] = &LocalMeta{
			CommittedVersion: version,
			LtHash:           h.Clone(),
		}
	}

	return nil
}

// flushAllDBs ensures all DB caches have their pending data eligible for
// GC flush by taking and immediately releasing a snapshot on each.
func (s *CommitStore) flushAllDBs() error {
	for _, cache := range []dbcache.Cache{s.accountDB, s.codeDB, s.storageDB, s.legacyDB, s.metadataDB} {
		snap, err := cache.Snapshot()
		if err != nil {
			return fmt.Errorf("flush snapshot: %w", err)
		}
		// Set a nil hash (zero/identity) so the snapshot is GC-eligible
		// when hash tracking is enabled. The actual hash value is irrelevant
		// for flush-only snapshots.
		if err := snap.SetHash(nil); err != nil {
			return fmt.Errorf("flush set hash: %w", err)
		}
		if err := snap.Release(); err != nil {
			return fmt.Errorf("flush release: %w", err)
		}
	}
	return nil
}

// clearPendingWrites clears all pending write buffers.
func (s *CommitStore) clearPendingWrites() {
	s.pendingChangeSets = make([]*proto.NamedChangeSet, 0)
}

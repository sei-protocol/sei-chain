package flatkv

import (
	"fmt"
	"maps"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// ApplyChangeSets buffers EVM changesets and updates LtHash.
// Non-EVM modules are routed to legacyDB with a "<module>/" key prefix.
func (s *CommitStore) ApplyChangeSets(changeSets []*proto.NamedChangeSet) (err error) {
	obs := s.observeOp("ApplyChangeSets", otelMetrics.ApplyChangesetsLatency,
		"changesets", len(changeSets))
	defer obs.done(&err, nil)

	if s.readOnly {
		return errReadOnly
	}

	// Hold the write lock for the whole body: it both reads
	// (batchReadOldValues) and mutates (maps.Copy) the pending-writes maps,
	// which iterator construction and Get read under a read lock.
	s.mu.Lock()
	defer s.mu.Unlock()

	///////////
	// Setup //
	///////////
	s.phaseTimer.SetPhase("apply_change_sets_prepare")

	changesByType, err := classifyAndPrefix(changeSets)
	if err != nil {
		return err
	}
	storageChanges := len(changesByType[keys.EVMKeyStorage])
	accountChanges := len(changesByType[keys.EVMKeyNonce]) + len(changesByType[keys.EVMKeyCodeHash])
	codeChanges := len(changesByType[keys.EVMKeyCode])
	legacyChanges := len(changesByType[keys.EVMKeyLegacy])

	blockHeight := s.committedVersion + 1

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
	accountWrites, err := mergeAccountUpdates(
		changesByType[keys.EVMKeyNonce],
		changesByType[keys.EVMKeyCodeHash],
		nil, // TODO: update this when we add a balance key!
	)
	if err != nil {
		return fmt.Errorf("failed to gather account updates: %w", err)
	}
	newAccountValues := deriveNewAccountValues(accountWrites, accountOld, blockHeight)
	accountPairs := gatherLTHashPairs(newAccountValues, accountOld)
	maps.Copy(s.accountWrites, newAccountValues)

	storageWrites, err := processStorageChanges(changesByType[keys.EVMKeyStorage], blockHeight)
	if err != nil {
		return fmt.Errorf("failed to parse storage changes: %w", err)
	}
	storagePairs := gatherLTHashPairs(storageWrites, storageOld)
	maps.Copy(s.storageWrites, storageWrites)

	codeWrites, err := processCodeChanges(changesByType[keys.EVMKeyCode], blockHeight)
	if err != nil {
		return fmt.Errorf("failed to parse code changes: %w", err)
	}
	codePairs := gatherLTHashPairs(codeWrites, codeOld)
	maps.Copy(s.codeWrites, codeWrites)

	legacyWrites, err := processLegacyChanges(changesByType[keys.EVMKeyLegacy], blockHeight)
	if err != nil {
		return fmt.Errorf("failed to parse legacy changes: %w", err)
	}
	legacyPairs := gatherLTHashPairs(legacyWrites, legacyOld)
	maps.Copy(s.legacyWrites, legacyWrites)

	addKVPairs(s.ctx, accountDBDir, len(newAccountValues))
	addKVPairs(s.ctx, storageDBDir, len(storageWrites))
	addKVPairs(s.ctx, codeDBDir, len(codeWrites))
	addKVPairs(s.ctx, legacyDBDir, len(legacyWrites))
	recordPendingWrites(s.ctx, accountDBDir, len(s.accountWrites))
	recordPendingWrites(s.ctx, storageDBDir, len(s.storageWrites))
	recordPendingWrites(s.ctx, codeDBDir, len(s.codeWrites))
	recordPendingWrites(s.ctx, legacyDBDir, len(s.legacyWrites))

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

	//////////////
	// Finalize //
	//////////////

	// Now that we've made it through the batch without errors, we can add the change sets to the pending change sets.
	s.pendingChangeSets = append(s.pendingChangeSets, changeSets...)

	s.phaseTimer.SetPhase("apply_change_done")
	logger.Debug("FlatKV ApplyChangeSets complete",
		"changesets", len(changeSets),
		"accountChanges", accountChanges,
		"accountWrites", len(newAccountValues),
		"storageChanges", storageChanges,
		"storageWrites", len(storageWrites),
		"codeChanges", codeChanges,
		"codeWrites", len(codeWrites),
		"legacyChanges", legacyChanges,
		"legacyWrites", len(legacyWrites),
		"pendingAccount", len(s.accountWrites),
		"pendingStorage", len(s.storageWrites),
		"pendingCode", len(s.codeWrites),
		"pendingLegacy", len(s.legacyWrites),
		"elapsed", obs.elapsed())
	return nil
}

// classifyAndPrefix splits changeSets into per-EVMKeyKind maps whose keys are
// already in physical format ("module/" + prefix_encoded_key). Non-EVM modules are
// merged into the EVMKeyLegacy bucket with a "<module>/" prefix.
//
// This replaces the former sortChangeSets + prefixModuleKeys two-pass approach,
// avoiding an extra map allocation and repeated string concatenation per key.
func classifyAndPrefix(changeSets []*proto.NamedChangeSet) (map[keys.EVMKeyKind]map[string][]byte, error) {
	result := make(map[keys.EVMKeyKind]map[string][]byte, 5)

	getOrCreate := func(kind keys.EVMKeyKind, sizeHint int) map[string][]byte {
		m, ok := result[kind]
		if !ok {
			m = make(map[string][]byte, sizeHint)
			result[kind] = m
		}
		return m
	}

	for _, cs := range changeSets {
		if len(cs.Changeset.Pairs) == 0 {
			continue
		}

		if cs.Name == keys.EVMStoreKey {
			for _, pair := range cs.Changeset.Pairs {
				kind, keyBytes := keys.ParseEVMKey(pair.Key)
				if kind == keys.EVMKeyEmpty {
					return nil, fmt.Errorf("flatkv: empty key in changeset")
				}

				var physKey string
				if kind == keys.EVMKeyLegacy {
					physKey = string(ktype.ModulePhysicalKey(keys.EVMStoreKey, pair.Key))
				} else {
					physKey = string(ktype.EVMPhysicalKey(kind, keyBytes))
				}

				kindMap := getOrCreate(kind, len(cs.Changeset.Pairs))
				if pair.Delete {
					kindMap[physKey] = nil
				} else {
					kindMap[physKey] = nonNilValue(pair.Value)
				}
			}
		} else {
			legacyMap := getOrCreate(keys.EVMKeyLegacy, len(cs.Changeset.Pairs))
			for _, pair := range cs.Changeset.Pairs {
				physKey := string(ktype.ModulePhysicalKey(cs.Name, pair.Key))
				if pair.Delete {
					legacyMap[physKey] = nil
				} else {
					legacyMap[physKey] = nonNilValue(pair.Value)
				}
			}
		}
	}

	return result, nil
}

// nonNilValue normalizes a non-delete changeset value so the downstream
// "nil value == deletion" convention in process*Changes stays correct.
//
// A changeset pair is a deletion iff its Delete flag is set; an empty
// (zero-length) value with Delete=false is a legitimate "set this key to an
// empty value" write. Protobuf cannot distinguish an empty []byte{} from nil,
// so after a WAL round-trip (catchup, read-only clone, snapshot export,
// state-sync restore) such a write arrives as Value=nil. Without this
// normalization the process*Changes helpers would treat the nil value as a
// deletion and drop the key on replay, diverging the per-DB LtHash — and thus
// the evm_lattice store hash and the consensus AppHash — from the live chain
// that stored the key. True deletes carry Delete=true and are recorded as nil
// by the caller before reaching this helper.
func nonNilValue(v []byte) []byte {
	if v == nil {
		return []byte{}
	}
	return v
}

// Process incoming storage changes into a form appropriate for hashing and insertion into the DB.
func processStorageChanges(
	rawChanges map[string][]byte,
	blockHeight int64,
) (map[string]*vtype.StorageData, error) {
	result := make(map[string]*vtype.StorageData, len(rawChanges))

	for keyStr, rawChange := range rawChanges {
		if rawChange == nil {
			// Deletion is equivalent to setting the storage value to a zero value
			result[keyStr] = vtype.NewStorageData().SetBlockHeight(blockHeight).SetValue(&[32]byte{})
		} else {
			value, err := vtype.ParseStorageValue(rawChange)
			if err != nil {
				return nil, fmt.Errorf("failed to parse storage value: %w", err)
			}
			result[keyStr] = vtype.NewStorageData().SetBlockHeight(blockHeight).SetValue(value)
		}
	}

	return result, nil
}

// Process incoming code changes into a form appropriate for hashing and insertion into the DB.
func processCodeChanges(
	rawChanges map[string][]byte,
	blockHeight int64,
) (map[string]*vtype.CodeData, error) {
	result := make(map[string]*vtype.CodeData, len(rawChanges))

	for keyStr, rawChange := range rawChanges {
		if rawChange == nil {
			// Deletion is equivalent to setting the code to a zero value
			result[keyStr] = vtype.NewCodeData().SetBlockHeight(blockHeight).SetBytecode(nil)
		} else {
			result[keyStr] = vtype.NewCodeData().SetBlockHeight(blockHeight).SetBytecode(rawChange)
		}
	}
	return result, nil
}

// Process incoming legacy changes into a form appropriate for hashing and insertion into the DB.
func processLegacyChanges(
	rawChanges map[string][]byte,
	blockHeight int64,
) (map[string]*vtype.LegacyData, error) {
	result := make(map[string]*vtype.LegacyData, len(rawChanges))

	for keyStr, rawChange := range rawChanges {
		if rawChange == nil {
			result[keyStr] = vtype.NewLegacyData().SetBlockHeight(blockHeight).MarkDeleted()
		} else {
			result[keyStr] = vtype.NewLegacyData().SetBlockHeight(blockHeight).SetValue(rawChange)
		}
	}
	return result, nil
}

func gatherLTHashPairs[T vtype.VType](
	newValues map[string]T,
	oldValues map[string]T,
) []lthash.KVPairWithLastValue {

	pairs := make([]lthash.KVPairWithLastValue, 0, len(newValues))

	for keyStr, newValue := range newValues {
		oldValue := oldValues[keyStr]
		isDelete := newValue.IsDelete()

		var newBytes []byte
		if !isDelete {
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
			Delete:    isDelete,
		})
	}

	return pairs
}

// Merge account updates down into a single update per account.
func mergeAccountUpdates(
	nonceChanges map[string][]byte,
	codeHashChanges map[string][]byte,
	balanceChanges map[string][]byte,
) (map[string]*vtype.PendingAccountWrite, error) {

	updates := make(map[string]*vtype.PendingAccountWrite, len(nonceChanges)+len(codeHashChanges))

	for key, nonceChange := range nonceChanges {
		if nonceChange == nil {
			// Deletion is equivalent to setting the nonce to 0
			updates[key] = updates[key].SetNonce(0)
		} else {
			nonce, err := vtype.ParseNonce(nonceChange)
			if err != nil {
				return nil, fmt.Errorf("invalid nonce value: %w", err)
			}
			updates[key] = updates[key].SetNonce(nonce)
		}
	}

	for key, codeHashChange := range codeHashChanges {
		if codeHashChange == nil {
			// Deletion is equivalent to setting the code hash to a zero hash
			var zero vtype.CodeHash
			updates[key] = updates[key].SetCodeHash(&zero)
		} else {
			codeHash, err := vtype.ParseCodeHash(codeHashChange)
			if err != nil {
				return nil, fmt.Errorf("invalid codehash value: %w", err)
			}
			updates[key] = updates[key].SetCodeHash(codeHash)
		}
	}

	for key, balanceChange := range balanceChanges {
		if balanceChange == nil {
			// Deletion is equivalent to setting the balance to a zero balance
			var zero vtype.Balance
			updates[key] = updates[key].SetBalance(&zero)
		} else {
			balance, err := vtype.ParseBalance(balanceChange)
			if err != nil {
				return nil, fmt.Errorf("invalid balance value: %w", err)
			}
			updates[key] = updates[key].SetBalance(balance)
		}
	}
	return updates, nil
}

// Combine the pending account writes with prior values to determine the new account values.
//
// We need to take this step because accounts are split into multiple fields, and it's possible to overwrite just a
// single field (thus requiring us to copy the unmodified fields from the prior value).
func deriveNewAccountValues(
	pendingWrites map[string]*vtype.PendingAccountWrite,
	oldValues map[string]*vtype.AccountData,
	blockHeight int64,
) map[string]*vtype.AccountData {
	result := make(map[string]*vtype.AccountData, len(pendingWrites))

	for addrStr, pendingWrite := range pendingWrites {
		oldValue := oldValues[addrStr]

		newValue := pendingWrite.Merge(oldValue, blockHeight)
		result[addrStr] = newValue
	}
	return result
}

package flatkv

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// ApplyChangeSets buffers EVM changesets and updates LtHash.
// Non-EVM modules are routed to legacyDB with a "<module>/" key prefix.
func (s *CommitStore) ApplyChangeSets(changeSets []*proto.NamedChangeSet) error {
	if s.readOnly {
		return errReadOnly
	}

	///////////
	// Setup //
	///////////
	s.phaseTimer.SetPhase("apply_change_sets_prepare")

	evmChangeSets := make([]*proto.NamedChangeSet, 0, len(changeSets))
	nonEVMByModule := make(map[string]map[string][]byte)
	for _, cs := range changeSets {
		if cs.Name == evm.EVMStoreKey {
			evmChangeSets = append(evmChangeSets, cs)
		} else {
			if cs.Changeset.Pairs == nil {
				continue
			}
			modMap, ok := nonEVMByModule[cs.Name]
			if !ok {
				modMap = make(map[string][]byte, len(cs.Changeset.Pairs))
				nonEVMByModule[cs.Name] = modMap
			}
			for _, pair := range cs.Changeset.Pairs {
				if pair.Delete {
					modMap[string(pair.Key)] = nil
				} else {
					modMap[string(pair.Key)] = pair.Value
				}
			}
		}
	}

	changesByType, err := sortChangeSets(evmChangeSets)
	if err != nil {
		return err
	}

	prefixModuleKeys(changesByType, nonEVMByModule)

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
		changesByType[evm.EVMKeyNonce],
		changesByType[evm.EVMKeyCodeHash],
		nil, // TODO: update this when we add a balance key!
	)
	if err != nil {
		return fmt.Errorf("failed to gather account updates: %w", err)
	}
	newAccountValues := deriveNewAccountValues(accountWrites, accountOld, blockHeight)
	accountPairs := gatherLTHashPairs(newAccountValues, accountOld)
	storeWrites(s.accountWrites, newAccountValues)

	// Gather storage pairs
	storageChanges, err := processStorageChanges(changesByType[evm.EVMKeyStorage], blockHeight)
	if err != nil {
		return fmt.Errorf("failed to parse storage changes: %w", err)
	}
	storagePairs := gatherLTHashPairs(storageChanges, storageOld)
	storeWrites(s.storageWrites, storageChanges)

	// Gather code pairs
	codeChanges, err := processCodeChanges(changesByType[evm.EVMKeyCode], blockHeight)
	if err != nil {
		return fmt.Errorf("failed to parse code changes: %w", err)
	}
	codePairs := gatherLTHashPairs(codeChanges, codeOld)
	storeWrites(s.codeWrites, codeChanges)

	// Gather legacy pairs
	legacyChanges, err := processLegacyChanges(changesByType[evm.EVMKeyLegacy])
	if err != nil {
		return fmt.Errorf("failed to parse legacy changes: %w", err)
	}
	legacyPairs := gatherLTHashPairs(legacyChanges, legacyOld)
	storeWrites(s.legacyWrites, legacyChanges)

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
	return nil
}

func storeWrites[T vtype.VType](pendingWrites map[string]T, newValues map[string]T) {
	for keyStr, newValue := range newValues {
		pendingWrites[keyStr] = newValue
	}
}

// sortChangeSets classifies EVM changeset pairs by key kind.
// Map keys are memiavl keys (type prefix preserved); codehash keys are
// canonicalized to the nonce prefix (0x0a) for account merging.
// After this, prefixModuleKeys converts all map keys to physical format.
func sortChangeSets(changeSets []*proto.NamedChangeSet) (map[evm.EVMKeyKind]map[string][]byte, error) {
	result := make(map[evm.EVMKeyKind]map[string][]byte)

	for _, cs := range changeSets {
		if cs.Changeset.Pairs == nil {
			continue
		}
		for _, pair := range cs.Changeset.Pairs {
			kind, keyBytes := evm.ParseEVMKey(pair.Key)

			if kind == evm.EVMKeyEmpty {
				return nil, fmt.Errorf("flatkv: empty key in changeset")
			}

			keyStr := string(pair.Key)
			if kind == evm.EVMKeyCodeHash {
				keyStr = string(evm.BuildMemIAVLEVMKey(EVMKeyAccount, keyBytes))
			}

			kindMap, ok := result[kind]
			if !ok {
				kindMap = make(map[string][]byte, len(cs.Changeset.Pairs))
				result[kind] = kindMap
			}

			if pair.Delete {
				kindMap[keyStr] = nil
			} else {
				kindMap[keyStr] = pair.Value
			}
		}
	}

	return result, nil
}

// prefixModuleKeys prepends "module/" to every key for physical DB namespacing.
// EVM keys get "evm/" prepended (same layout as ModulePhysicalKey).
// Non-EVM cosmos keys are merged into the legacy map with their own module prefix.
func prefixModuleKeys(
	changesByType map[evm.EVMKeyKind]map[string][]byte,
	nonEVMByModule map[string]map[string][]byte,
) {
	evmPrefix := evm.EVMStoreKey + "/"
	for kind, m := range changesByType {
		prefixed := make(map[string][]byte, len(m))
		for key, val := range m {
			prefixed[evmPrefix+key] = val
		}
		changesByType[kind] = prefixed
	}

	legacyMap := changesByType[evm.EVMKeyLegacy]
	if len(nonEVMByModule) > 0 && legacyMap == nil {
		legacyMap = make(map[string][]byte)
		changesByType[evm.EVMKeyLegacy] = legacyMap
	}
	for module, pairs := range nonEVMByModule {
		modPrefix := module + "/"
		for rawKey, val := range pairs {
			legacyMap[modPrefix+rawKey] = val
		}
	}
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
func processLegacyChanges(rawChanges map[string][]byte) (map[string]*vtype.LegacyData, error) {
	result := make(map[string]*vtype.LegacyData, len(rawChanges))

	for keyStr, rawChange := range rawChanges {
		if rawChange == nil {
			result[keyStr] = vtype.NewLegacyData().MarkDeleted()
		} else {
			result[keyStr] = vtype.NewLegacyData().SetValue(rawChange)
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
// We need to take this step because accounts are split into multiple fields, and its possible to overwrite just a
// single field (thus requring us to copy the unmodified fields from the prior value).
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

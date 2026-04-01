package flatkv

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// Supported key types for FlatKV.
// TODO: add balance key when that is eventually supported
var supportedKeyTypes = map[evm.EVMKeyKind]struct{}{
	evm.EVMKeyStorage:  {},
	evm.EVMKeyNonce:    {},
	evm.EVMKeyCodeHash: {},
	evm.EVMKeyCode:     {},
	evm.EVMKeyLegacy:   {},
}

// ApplyChangeSets buffers EVM changesets and updates LtHash.
func (s *CommitStore) ApplyChangeSets(changeSets []*proto.NamedChangeSet) error {
	if s.readOnly {
		return errReadOnly
	}

	///////////
	// Setup //
	///////////
	s.phaseTimer.SetPhase("apply_change_sets_prepare")
	s.pendingChangeSets = append(s.pendingChangeSets, changeSets...) // TODO this is wrong!!

	changesByType, err := sortChangeSets(changeSets, s.config.StrictKeyTypeCheck)
	if err != nil {
		return fmt.Errorf("failed to sort change sets: %w", err)
	}

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

	// Gather storage pairs
	storageChanges, err := processStorageChanges(changesByType[evm.EVMKeyStorage], blockHeight)
	if err != nil {
		return fmt.Errorf("failed to parse storage changes: %w", err)
	}
	storagePairs := gatherLTHashPairs(storageChanges, storageOld)

	// Gather code pairs
	codeChanges, err := processCodeChanges(changesByType[evm.EVMKeyCode], blockHeight)
	if err != nil {
		return fmt.Errorf("failed to parse code changes: %w", err)
	}
	codePairs := gatherLTHashPairs(codeChanges, codeOld)

	// Gather legacy pairs
	legacyChanges, err := processLegacyChanges(changesByType[evm.EVMKeyLegacy], blockHeight)
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
func sortChangeSets(
	cs []*proto.NamedChangeSet,
	// If true, returns an error if an unsupported key type is encountered.
	strict bool,
) (map[evm.EVMKeyKind]map[string][]byte, error) {
	result := make(map[evm.EVMKeyKind]map[string][]byte)

	for _, cs := range cs {
		if cs.Changeset.Pairs == nil {
			continue
		}
		for _, pair := range cs.Changeset.Pairs {
			kind, keyBytes := evm.ParseEVMKey(pair.Key)

			if _, ok := supportedKeyTypes[kind]; !ok {
				if strict {
					return nil, fmt.Errorf("unsupported key type: %s", kind)
				} else {
					logger.Warn("unsupported key type", "key", kind)
				}
			}

			keyStr := string(keyBytes)

			kindMap, ok := result[kind]
			if !ok {
				kindMap = make(map[string][]byte)
				result[kind] = kindMap
			}

			kindMap[keyStr] = pair.Value
		}
	}

	return result, nil
}

// Process incoming storage changes into a form appropriate for hashing and insertion into the DB.
func processStorageChanges(
	rawChanges map[string][]byte,
	blockHeight int64,
) (map[string]*vtype.StorageData, error) {
	result := make(map[string]*vtype.StorageData)

	for keyStr, rawChange := range rawChanges {
		value, err := vtype.ParseStorageValue(rawChange)
		if err != nil {
			return nil, fmt.Errorf("failed to parse storage value: %w", err)
		}

		result[keyStr] = vtype.NewStorageData().SetBlockHeight(blockHeight).SetValue(value)
	}

	return result, nil
}

// Process incoming code changes into a form appropriate for hashing and insertion into the DB.
func processCodeChanges(
	rawChanges map[string][]byte,
	blockHeight int64,
) (map[string]*vtype.CodeData, error) {
	result := make(map[string]*vtype.CodeData)

	for keyStr, rawChange := range rawChanges {
		result[keyStr] = vtype.NewCodeData().SetBlockHeight(blockHeight).SetBytecode(rawChange)
	}
	return result, nil
}

// Process incoming legacy changes into a form appropriate for hashing and insertion into the DB.
func processLegacyChanges(
	rawChanges map[string][]byte,
	blockHeight int64,
) (map[string]*vtype.LegacyData, error) {
	result := make(map[string]*vtype.LegacyData)

	for keyStr, rawChange := range rawChanges {
		result[keyStr] = vtype.NewLegacyData().SetBlockHeight(blockHeight).SetValue(rawChange)
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
func mergeAccountUpdates(
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
func deriveNewAccountValues(
	pendingWrites map[string]*vtype.PendingAccountWrite,
	oldValues map[string]*vtype.AccountData,
	blockHeight int64,
) map[string]*vtype.AccountData {
	result := make(map[string]*vtype.AccountData)

	for addrStr, pendingWrite := range pendingWrites {
		oldValue := oldValues[addrStr]

		newValue := pendingWrite.Merge(oldValue, blockHeight)
		result[addrStr] = newValue
	}
	return result
}

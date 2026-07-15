package flatkv

import (
	"fmt"
	"maps"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	"go.opentelemetry.io/otel/metric"
)

// ApplyChangeSets classifies changesets, buffers pending writes, and folds
// them into the working LtHash. Non-EVM modules go to miscDB under "<module>/".
func (s *CommitStore) ApplyChangeSets(changeSets []*proto.NamedChangeSet) (err error) {
	// Hold the write lock for the whole body: it both reads (old values) and
	// mutates (maps.Copy) the pending-writes maps, which iterator construction
	// and Get read under a read lock.
	s.mu.Lock()
	defer s.mu.Unlock()

	obs := s.observeOp("ApplyChangeSets", otelMetrics.ApplyChangesetsLatency,
		"changesets", len(changeSets))
	defer obs.done(&err, nil)

	if s.readOnly {
		return errReadOnly
	}

	s.phaseTimer.SetPhase("apply_change_sets_prepare")
	changesByType, err := classifyAndPrefix(changeSets)
	if err != nil {
		return err
	}
	pairSets, counts, err := s.prepareWrites(changesByType, s.committedVersion+1)
	if err != nil {
		return err
	}

	s.phaseTimer.SetPhase("apply_change_compute_lt_hash")
	res, err := s.ltCalc.Compute(pairSets, s.perDBWorkingLtHash, s.perDBModuleWorkingLtHash, s.perDBModuleWorkingStats)
	if err != nil {
		return err
	}

	s.perDBWorkingLtHash = res.PerDB
	s.perDBModuleWorkingLtHash = res.PerModule
	s.perDBModuleWorkingStats = res.PerModuleStats
	s.workingLtHash = res.Global
	s.pendingChangeSets = append(s.pendingChangeSets, changeSets...)

	s.phaseTimer.SetPhase("apply_change_done")
	logger.Debug("FlatKV ApplyChangeSets complete",
		"changesets", len(changeSets),
		"writes", counts.accountWrites+counts.storageWrites+counts.codeWrites+counts.miscWrites,
		"elapsed", obs.elapsed())
	return nil
}

// applyCounts records per-DB write tallies for logging and metrics.
type applyCounts struct {
	accountWrites, storageWrites, codeWrites, miscWrites int
}

// prepareWrites reads prior values, applies EVM value semantics, buffers the
// resulting rows into the pending-write maps, and returns per-DB LtHash pairs
// for Compute. Only accounts need old values in structured form (to merge
// partial nonce/codehash updates); other DBs pass raw old bytes through.
func (s *CommitStore) prepareWrites(
	changesByType map[keys.EVMKeyKind]map[string][]byte,
	blockHeight int64,
) ([]lthash.DBPairs, applyCounts, error) {
	var counts applyCounts

	s.phaseTimer.SetPhase("apply_change_sets_batch_read")
	readStart := time.Now()
	oldByDB, err := s.ltCalc.ReadOldValues(s, keysByDBFromClassified(changesByType))
	otelMetrics.BatchReadOldValuesLatency.Record(s.ctx, secondsSince(readStart),
		metric.WithAttributes(successAttr(err)))
	if err != nil {
		return nil, counts, fmt.Errorf("failed to batch read old values: %w", err)
	}

	s.phaseTimer.SetPhase("apply_change_sets_gather_pairs")

	// Account: merge partial nonce/codehash updates onto the old account.
	accountOld, err := deserializeAccountOld(oldByDB[accountDBDir])
	if err != nil {
		return nil, counts, err
	}
	accountUpdates, err := mergeAccountUpdates(
		changesByType[keys.EVMKeyNonce],
		changesByType[keys.EVMKeyCodeHash],
		nil, // TODO: update this when we add a balance key!
	)
	if err != nil {
		return nil, counts, fmt.Errorf("failed to gather account updates: %w", err)
	}
	newAccounts := deriveNewAccountValues(accountUpdates, accountOld, blockHeight)
	accountPairs := gatherPairs(newAccounts, oldByDB[accountDBDir])
	maps.Copy(s.accountWrites, newAccounts)
	counts.accountWrites = len(newAccounts)

	storageWrites, err := processStorageChanges(changesByType[keys.EVMKeyStorage], blockHeight)
	if err != nil {
		return nil, counts, fmt.Errorf("failed to parse storage changes: %w", err)
	}
	storagePairs := gatherPairs(storageWrites, oldByDB[storageDBDir])
	maps.Copy(s.storageWrites, storageWrites)
	counts.storageWrites = len(storageWrites)

	codeWrites, err := processCodeChanges(changesByType[keys.EVMKeyCode], blockHeight)
	if err != nil {
		return nil, counts, fmt.Errorf("failed to parse code changes: %w", err)
	}
	codePairs := gatherPairs(codeWrites, oldByDB[codeDBDir])
	maps.Copy(s.codeWrites, codeWrites)
	counts.codeWrites = len(codeWrites)

	miscWrites, err := processMiscChanges(changesByType[keys.EVMKeyMisc], blockHeight)
	if err != nil {
		return nil, counts, fmt.Errorf("failed to parse misc changes: %w", err)
	}
	miscPairs := gatherPairs(miscWrites, oldByDB[miscDBDir])
	maps.Copy(s.miscWrites, miscWrites)
	counts.miscWrites = len(miscWrites)

	addKVPairs(s.ctx, accountDBDir, counts.accountWrites)
	addKVPairs(s.ctx, storageDBDir, counts.storageWrites)
	addKVPairs(s.ctx, codeDBDir, counts.codeWrites)
	addKVPairs(s.ctx, miscDBDir, counts.miscWrites)
	recordPendingWrites(s.ctx, accountDBDir, len(s.accountWrites))
	recordPendingWrites(s.ctx, storageDBDir, len(s.storageWrites))
	recordPendingWrites(s.ctx, codeDBDir, len(s.codeWrites))
	recordPendingWrites(s.ctx, miscDBDir, len(s.miscWrites))

	return []lthash.DBPairs{
		{Dir: storageDBDir, Pairs: storagePairs},
		{Dir: accountDBDir, Pairs: accountPairs},
		{Dir: codeDBDir, Pairs: codePairs},
		{Dir: miscDBDir, Pairs: miscPairs},
	}, counts, nil
}

// keysByDBFromClassified maps the per-kind classified changes to the set of
// physical keys per data DB dir, so the calculator can read old values grouped
// by DB. Account keys come from both the nonce and codehash kinds.
func keysByDBFromClassified(changesByType map[keys.EVMKeyKind]map[string][]byte) map[string]map[string]struct{} {
	out := make(map[string]map[string]struct{}, len(dataDBDirs))
	add := func(dir string, changes map[string][]byte) {
		if len(changes) == 0 {
			return
		}
		set := out[dir]
		if set == nil {
			set = make(map[string]struct{}, len(changes))
			out[dir] = set
		}
		for key := range changes {
			set[key] = struct{}{}
		}
	}
	add(storageDBDir, changesByType[keys.EVMKeyStorage])
	add(accountDBDir, changesByType[keys.EVMKeyNonce])
	add(accountDBDir, changesByType[keys.EVMKeyCodeHash])
	add(codeDBDir, changesByType[keys.EVMKeyCode])
	add(miscDBDir, changesByType[keys.EVMKeyMisc])
	return out
}

// deserializeAccountOld parses the raw old account bytes read by the calculator
// into structured AccountData, needed to merge partial account-field updates.
func deserializeAccountOld(raw map[string][]byte) (map[string]*vtype.AccountData, error) {
	old := make(map[string]*vtype.AccountData, len(raw))
	for key, b := range raw {
		if b == nil {
			continue
		}
		v, err := vtype.DeserializeAccountData(b)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize accountDB old value: %w", err)
		}
		old[key] = v
	}
	return old, nil
}

// moduleOfKey extracts the owning module from a physical key. Injected into the
// lthash HashCalculator so it can bucket pairs by module without importing ktype
// (ktype already imports lthash).
func moduleOfKey(physicalKey []byte) (string, error) {
	module, _, err := ktype.StripModulePrefix(physicalKey)
	return module, err
}

// classifyAndPrefix splits changeSets into per-EVMKeyKind maps whose keys are
// already in physical format ("module/" + prefix_encoded_key). Non-EVM modules are
// merged into the EVMKeyMisc bucket with a "<module>/" prefix.
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
				if kind == keys.EVMKeyMisc {
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
			miscMap := getOrCreate(keys.EVMKeyMisc, len(cs.Changeset.Pairs))
			for _, pair := range cs.Changeset.Pairs {
				physKey := string(ktype.ModulePhysicalKey(cs.Name, pair.Key))
				if pair.Delete {
					miscMap[physKey] = nil
				} else {
					miscMap[physKey] = nonNilValue(pair.Value)
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

// Process incoming misc changes into a form appropriate for hashing and insertion into the DB.
func processMiscChanges(
	rawChanges map[string][]byte,
	blockHeight int64,
) (map[string]*vtype.MiscData, error) {
	result := make(map[string]*vtype.MiscData, len(rawChanges))

	for keyStr, rawChange := range rawChanges {
		if rawChange == nil {
			result[keyStr] = vtype.NewMiscData().SetBlockHeight(blockHeight).MarkDeleted()
		} else {
			result[keyStr] = vtype.NewMiscData().SetBlockHeight(blockHeight).SetValue(rawChange)
		}
	}
	return result, nil
}

// gatherPairs builds the LtHash pairs for one DB from its new typed values and
// the raw old serialized bytes read by the calculator. The old bytes are used
// verbatim as LastValue: by the round-trip identity of the value serializers
// they equal the exact bytes previously folded into the hash, so unmixing them
// cancels that contribution precisely. A key with no prior value (or a pending
// deletion) has a nil entry in rawOld and thus a nil LastValue (nothing to
// unmix).
func gatherPairs[T vtype.VType](
	newValues map[string]T,
	rawOld map[string][]byte,
) []lthash.KVPairWithLastValue {
	pairs := make([]lthash.KVPairWithLastValue, 0, len(newValues))
	for keyStr, newValue := range newValues {
		isDelete := newValue.IsDelete()

		var newBytes []byte
		if !isDelete {
			newBytes = newValue.Serialize()
		}

		pairs = append(pairs, lthash.KVPairWithLastValue{
			Key:       []byte(keyStr),
			Value:     newBytes,
			LastValue: rawOld[keyStr],
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

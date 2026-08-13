package flatkv

import (
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/snapshot"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	"go.opentelemetry.io/otel/metric"
)

// ApplyChangeSets writes one block's changes into the four data stores and folds them into the
// working LtHash. Non-EVM modules go to miscDB under "<module>/". Each value records version as the
// height it was last modified at; the same version must be passed to the subsequent Commit.
func (s *CommitStore) ApplyChangeSets(version int64, changeSets []*proto.NamedChangeSet) error {
	return s.applyChangeSets(version, changeSets, nil)
}

// applyChangeSets is ApplyChangeSets with the replay skip list. alreadyHave is nil outside a startup
// replay, which means every store needs every block.
func (s *CommitStore) applyChangeSets(
	version int64,
	changeSets []*proto.NamedChangeSet,
	alreadyHave map[string]int64,
) (err error) {
	// Hold the write lock for the whole body: it both reads old values out of the stores and writes
	// this block's values into them, and Get and iterator construction read them under a read lock.
	s.mu.Lock()
	defer s.mu.Unlock()

	obs := s.observeOp("ApplyChangeSets", otelMetrics.ApplyChangesetsLatency,
		"changesets", len(changeSets))
	defer obs.done(&err, nil)

	if s.readOnly {
		return errReadOnly
	}
	// Blocks are contiguous and the first block is 1, so writes always land at committedVersion+1. See the
	// Commit contract: a store whose history starts higher is seeded by SetInitialVersion.
	// An empty batch for a block that is already committed is accepted and does nothing.
	if version > 0 && version == s.committedVersion {
		if len(changeSets) == 0 {
			// An empty batch would leave the sealed block exactly as it is, so a stale height is
			// harmless here. No caller produces one today: every writer stamps its batch at the height
			// after the one the store has committed. This stands as tolerance for a caller that has
			// lost track of the height, not as a path taken in normal operation.
			return nil
		}
		// Writes are a different matter: they would belong to a block that is already sealed, and there
		// is nowhere to put them.
		return fmt.Errorf("flatkv: apply version %d is already committed and this batch has %d changesets",
			version, len(changeSets))
	}
	if version != s.committedVersion+1 {
		return fmt.Errorf("flatkv: apply version %d must be committed version %d plus one",
			version, s.committedVersion)
	}
	// A single block's writes may arrive across several ApplyChangeSets calls at the same height (e.g. a
	// ModuleRouter fanning one block's changesets out to multiple routes that all target flatKV). The check
	// above already restricts those to committedVersion+1, which is the only height pending writes can be
	// stamped at, so same-height repeats are accepted and no other height can reach here.

	s.phaseTimer.SetPhase("apply_change_sets_prepare")
	changesByType, err := classifyAndPrefix(changeSets, s.classifyBucketSizes)
	if err != nil {
		return fmt.Errorf("classify changesets: %w", err)
	}
	s.classifyBucketSizes = changesByType.bucketSizes()
	// Parse, gather, and sort. Nothing is written until all of it has validated, so a parse failure
	// part way through cannot leave some of the block's values in a store.
	prepared, err := s.prepareWrites(changesByType, version)
	if err != nil {
		return fmt.Errorf("prepare writes: %w", err)
	}

	if err := s.writeToStores(prepared, changeSets, version, alreadyHave); err != nil {
		return fmt.Errorf("write to stores: %w", err)
	}

	s.phaseTimer.SetPhase("apply_change_done")
	logger.Debug("FlatKV ApplyChangeSets complete",
		"version", version,
		"changesets", len(changeSets),
		"writes", len(prepared.accounts)+len(prepared.storage)+len(prepared.code)+len(prepared.misc),
		"elapsed", obs.elapsed())
	return nil
}

// preparedWrites holds the fully-validated per-database values and LtHash pairs for one
// ApplyChangeSets call. Nothing here reaches a store until every kind has validated — see
// writeToStores.
type preparedWrites struct {
	accounts map[string]*vtype.AccountData
	storage  map[string]*vtype.StorageData
	code     map[string]*vtype.CodeData
	misc     map[string]*vtype.MiscData
}

// prepareWrites applies EVM value semantics and returns the values to write, per database.
func (s *CommitStore) prepareWrites(
	changesByType classifiedChanges,
	blockHeight int64,
) (preparedWrites, error) {
	var out preparedWrites

	// A nonce or codehash change carries only its own field, so it has to be merged onto the account as
	// it stands right now — a live read, since anything an earlier call at this height wrote counts.
	s.phaseTimer.SetPhase("apply_change_sets_read_accounts")
	readStart := time.Now()
	accountOld, err := s.readAccountsForMerge(changesByType)
	otelMetrics.BatchReadOldValuesLatency.Record(s.ctx, secondsSince(readStart),
		metric.WithAttributes(successAttr(err)))
	if err != nil {
		return out, err
	}

	s.phaseTimer.SetPhase("apply_change_sets_gather_values")

	accountUpdates, err := mergeAccountUpdates(
		changesByType[keys.EVMKeyNonce],
		changesByType[keys.EVMKeyCodeHash],
		nil, // TODO: update this when we add a balance key!
	)
	if err != nil {
		return out, fmt.Errorf("failed to gather account updates: %w", err)
	}
	newAccounts := deriveNewAccountValues(accountUpdates, accountOld, blockHeight)

	storageWrites, err := toStorageValues(changesByType[keys.EVMKeyStorage], blockHeight)
	if err != nil {
		return out, fmt.Errorf("failed to parse storage changes: %w", err)
	}

	codeWrites, err := toCodeValues(changesByType[keys.EVMKeyCode], blockHeight)
	if err != nil {
		return out, fmt.Errorf("failed to parse code changes: %w", err)
	}

	miscWrites, err := toMiscValues(changesByType[keys.EVMKeyMisc], blockHeight)
	if err != nil {
		return out, fmt.Errorf("failed to parse misc changes: %w", err)
	}

	out.accounts = newAccounts
	out.storage = storageWrites
	out.code = codeWrites
	out.misc = miscWrites
	return out, nil
}

// readAccountsForMerge reads the accounts that this batch's nonce and codehash changes touch, so those
// partial updates can be merged onto whole accounts. Keys come from both kinds, since either can name
// an account the other does not.
func (s *CommitStore) readAccountsForMerge(
	changesByType classifiedChanges,
) (map[string]*vtype.AccountData, error) {
	touched := make(map[string]struct{},
		len(changesByType[keys.EVMKeyNonce])+len(changesByType[keys.EVMKeyCodeHash]))
	for _, kind := range []keys.EVMKeyKind{keys.EVMKeyNonce, keys.EVMKeyCodeHash} {
		for _, change := range changesByType[kind] {
			touched[change.key] = struct{}{}
		}
	}
	if len(touched) == 0 {
		return nil, nil
	}

	physKeys := make([][]byte, 0, len(touched))
	for key := range touched {
		physKeys = append(physKeys, []byte(key))
	}
	raw, err := s.accountStore.BatchGet(physKeys)
	if err != nil {
		return nil, fmt.Errorf("read accounts to merge onto: %w", err)
	}
	return deserializeOldAccounts(raw)
}

// writeToStores writes one successful ApplyChangeSets batch into the four data stores and records the
// changesets and the block height they belong to.
//
// A store that already has this block is skipped. That happens only when a startup replay is catching
// the stores up to each other, where its hash already includes the block and writing it again would
// count it twice.
//
// The writes must come after the account reads in prepareWrites, because writing here is what makes
// this block's values visible to a read through the same store.
func (s *CommitStore) writeToStores(
	prepared preparedWrites,
	changeSets []*proto.NamedChangeSet,
	version int64,
	alreadyHave map[string]int64,
) error {
	s.phaseTimer.SetPhase("apply_change_write_to_stores")

	// TODO: currently, WAL replay may replay blocks already in some stores. In the future when WAL replay is external,
	// we may be able to simplify this code since we will be able to assume that all stores start at the same block.
	if alreadyHave[accountDBDir] < version {
		if err := serializeAndPut(s.accountStore, prepared.accounts); err != nil {
			return fmt.Errorf("write %s values: %w", accountDBDir, err)
		}
		addKVPairs(s.ctx, accountDBDir, len(prepared.accounts))
	}
	if alreadyHave[storageDBDir] < version {
		if err := serializeAndPut(s.storageStore, prepared.storage); err != nil {
			return fmt.Errorf("write %s values: %w", storageDBDir, err)
		}
		addKVPairs(s.ctx, storageDBDir, len(prepared.storage))
	}
	if alreadyHave[codeDBDir] < version {
		if err := serializeAndPut(s.codeStore, prepared.code); err != nil {
			return fmt.Errorf("write %s values: %w", codeDBDir, err)
		}
		addKVPairs(s.ctx, codeDBDir, len(prepared.code))
	}
	if alreadyHave[miscDBDir] < version {
		if err := serializeAndPut(s.miscStore, prepared.misc); err != nil {
			return fmt.Errorf("write %s values: %w", miscDBDir, err)
		}
		addKVPairs(s.ctx, miscDBDir, len(prepared.misc))
	}

	s.pendingChangeSets = append(s.pendingChangeSets, changeSets...)
	s.pendingBlockHeight = version
	return nil
}

// serializeAndPut writes values into the store's current version, to be sealed by the next Commit. A
// value reporting IsDelete becomes a deletion; every other value is stored as its serialized form.
//
// values is keyed by physical key.
func serializeAndPut[T vtype.VType](store snapshot.SnapshotEngine, values map[string]T) error {
	if len(values) == 0 {
		return nil
	}
	pairs := make([]*proto.KVPair, 0, len(values))
	for key, value := range values {
		if value.IsDelete() {
			pairs = append(pairs, &proto.KVPair{Key: []byte(key), Delete: true})
			continue
		}
		pairs = append(pairs, &proto.KVPair{Key: []byte(key), Value: value.Serialize()})
	}
	if err := store.BatchSet(pairs); err != nil {
		return fmt.Errorf("batch write: %w", err)
	}
	return nil
}

// deserializeOldAccounts parses the account database's old values into AccountData. A partial update —
// a nonce without a codehash, say — has to be merged onto the account that is already there, which
// needs the old value in structured form rather than as bytes.
//
// raw is keyed by physical key, and a key that had no prior value maps to nil; those are dropped
// rather than deserialized, so the result holds only accounts that already existed.
func deserializeOldAccounts(raw map[string][]byte) (map[string]*vtype.AccountData, error) {
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

// classifiedChange is one changeset pair with its physical key already built.
type classifiedChange struct {
	// key is the physical DB key: "module/" + the module's encoded key.
	key string

	// value is the key's new raw bytes. A nil value means the key was deleted.
	value []byte
}

// classifiedChanges holds one block's changeset pairs bucketed by EVM key kind.
//
// Pairs sit in the order they arrived and duplicate keys are kept, because the per-kind maps built
// in prepareWrites already apply last-write-wins; deduplicating here as well would hash every key a
// second time to reach the same answer.
type classifiedChanges [keys.EVMKeyKindCount][]classifiedChange

// bucketSizes returns the number of pairs in each kind's bucket, for sizing a later block's.
func (c classifiedChanges) bucketSizes() [keys.EVMKeyKindCount]int {
	var sizes [keys.EVMKeyKindCount]int
	for kind, bucket := range c {
		sizes[kind] = len(bucket)
	}
	return sizes
}

// classifyAndPrefix splits changeSets into per-EVMKeyKind buckets whose keys are already in
// physical format ("module/" + prefix_encoded_key). Non-EVM modules are merged into the
// EVMKeyMisc bucket with a "<module>/" prefix.
//
// sizeHints gives each bucket's length in the previous block. Buckets are allocated at twice that,
// since a block that grows a little then still lands in a single allocation rather than a resize
// and copy; a bucket with no hint grows on demand.
func classifyAndPrefix(
	changeSets []*proto.NamedChangeSet,
	sizeHints [keys.EVMKeyKindCount]int,
) (classifiedChanges, error) {
	var result classifiedChanges
	for kind, hint := range sizeHints {
		if hint > 0 {
			result[kind] = make([]classifiedChange, 0, 2*hint)
		}
	}

	// One buffer for the whole block. The string conversion copies each physical key out of it, so
	// it can be rewound and reused for every pair, leaving one allocation per key rather than one
	// for the key bytes and a second for the string.
	var scratchArray [ktype.MaxEVMPhysicalKeyLen]byte
	scratch := scratchArray[:0]

	for _, cs := range changeSets {
		if cs == nil || len(cs.Changeset.Pairs) == 0 {
			continue
		}

		if cs.Name == keys.EVMStoreKey {
			for _, pair := range cs.Changeset.Pairs {
				kind, keyBytes := keys.ParseEVMKey(pair.Key)
				if kind == keys.EVMKeyEmpty {
					return classifiedChanges{}, fmt.Errorf("flatkv: empty key in changeset")
				}

				if kind == keys.EVMKeyMisc {
					scratch = ktype.AppendModulePhysicalKey(scratch[:0], keys.EVMStoreKey, pair.Key)
				} else {
					scratch = ktype.AppendEVMPhysicalKey(scratch[:0], kind, keyBytes)
				}
				result[kind] = append(result[kind], newClassifiedChange(string(scratch), pair))
			}
			continue
		}

		// An empty module name would fold into "/"+key here and later
		// persist as the per-module meta key "_meta/x:/hash", which
		// ParseModuleLtHashKey rejects on reload — a store that ever
		// commits one becomes permanently unopenable (sum-to-root check
		// fails forever). Reject it up front instead; module names are
		// never empty in normal operation (Cosmos SDK's NewKVStoreKey
		// panics on an empty name), so this only guards malformed input.
		if cs.Name == "" {
			return classifiedChanges{}, fmt.Errorf("flatkv: empty module name in changeset")
		}
		miscBucket := &result[keys.EVMKeyMisc]
		for _, pair := range cs.Changeset.Pairs {
			scratch = ktype.AppendModulePhysicalKey(scratch[:0], cs.Name, pair.Key)
			*miscBucket = append(*miscBucket, newClassifiedChange(string(scratch), pair))
		}
	}

	return result, nil
}

// newClassifiedChange pairs a physical key with a changeset pair's new value, recording a deleted
// pair as a nil value.
func newClassifiedChange(physicalKey string, pair *proto.KVPair) classifiedChange {
	if pair.Delete {
		return classifiedChange{key: physicalKey}
	}
	return classifiedChange{key: physicalKey, value: nonNilValue(pair.Value)}
}

// nonNilValue normalizes a non-delete changeset value so the downstream
// "nil value == deletion" convention in the to*Values helpers stays correct.
//
// A changeset pair is a deletion iff its Delete flag is set; an empty
// (zero-length) value with Delete=false is a legitimate "set this key to an
// empty value" write. Protobuf cannot distinguish an empty []byte{} from nil,
// so after a WAL round-trip (catchup, read-only clone, snapshot export,
// state-sync restore) such a write arrives as Value=nil. Without this
// normalization the to*Values helpers would treat the nil value as a
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

// toStorageValues turns raw storage changes into StorageData stamped with blockHeight. A nil change is
// a deletion, which for storage means the zero value. Both maps are keyed by physical key.
func toStorageValues(
	rawChanges []classifiedChange,
	blockHeight int64,
) (map[string]*vtype.StorageData, error) {
	result := make(map[string]*vtype.StorageData, len(rawChanges))

	for _, change := range rawChanges {
		if change.value == nil {
			// Deletion is equivalent to setting the storage value to a zero value
			result[change.key] = vtype.NewStorageData().SetBlockHeight(blockHeight).SetValue(&[32]byte{})
		} else {
			value, err := vtype.ParseStorageValue(change.value)
			if err != nil {
				return nil, fmt.Errorf("failed to parse storage value: %w", err)
			}
			result[change.key] = vtype.NewStorageData().SetBlockHeight(blockHeight).SetValue(value)
		}
	}

	return result, nil
}

// toCodeValues turns raw code changes into CodeData stamped with blockHeight. A nil change is a
// deletion, which for code means empty bytecode. Both maps are keyed by physical key.
func toCodeValues(
	rawChanges []classifiedChange,
	blockHeight int64,
) (map[string]*vtype.CodeData, error) {
	result := make(map[string]*vtype.CodeData, len(rawChanges))

	for _, change := range rawChanges {
		if change.value == nil {
			// Deletion is equivalent to setting the code to a zero value
			result[change.key] = vtype.NewCodeData().SetBlockHeight(blockHeight).SetBytecode(nil)
		} else {
			result[change.key] = vtype.NewCodeData().SetBlockHeight(blockHeight).SetBytecode(change.value)
		}
	}
	return result, nil
}

// toMiscValues turns raw misc changes into MiscData stamped with blockHeight. A nil change is a
// deletion, which for misc means an empty value. Both maps are keyed by physical key.
func toMiscValues(
	rawChanges []classifiedChange,
	blockHeight int64,
) (map[string]*vtype.MiscData, error) {
	result := make(map[string]*vtype.MiscData, len(rawChanges))

	for _, change := range rawChanges {
		if change.value == nil {
			result[change.key] = vtype.NewMiscData().SetBlockHeight(blockHeight).MarkDeleted()
		} else {
			result[change.key] = vtype.NewMiscData().SetBlockHeight(blockHeight).SetValue(change.value)
		}
	}
	return result, nil
}

// Merge account updates down into a single update per account.
func mergeAccountUpdates(
	nonceChanges []classifiedChange,
	codeHashChanges []classifiedChange,
	balanceChanges []classifiedChange,
) (map[string]*vtype.PendingAccountWrite, error) {

	updates := make(map[string]*vtype.PendingAccountWrite, len(nonceChanges)+len(codeHashChanges))

	for _, change := range nonceChanges {
		if change.value == nil {
			// Deletion is equivalent to setting the nonce to 0
			updates[change.key] = updates[change.key].SetNonce(0)
		} else {
			nonce, err := vtype.ParseNonce(change.value)
			if err != nil {
				return nil, fmt.Errorf("invalid nonce value: %w", err)
			}
			updates[change.key] = updates[change.key].SetNonce(nonce)
		}
	}

	for _, change := range codeHashChanges {
		if change.value == nil {
			// Deletion is equivalent to setting the code hash to a zero hash
			var zero vtype.CodeHash
			updates[change.key] = updates[change.key].SetCodeHash(&zero)
		} else {
			codeHash, err := vtype.ParseCodeHash(change.value)
			if err != nil {
				return nil, fmt.Errorf("invalid codehash value: %w", err)
			}
			updates[change.key] = updates[change.key].SetCodeHash(codeHash)
		}
	}

	for _, change := range balanceChanges {
		if change.value == nil {
			// Deletion is equivalent to setting the balance to a zero balance
			var zero vtype.Balance
			updates[change.key] = updates[change.key].SetBalance(&zero)
		} else {
			balance, err := vtype.ParseBalance(change.value)
			if err != nil {
				return nil, fmt.Errorf("invalid balance value: %w", err)
			}
			updates[change.key] = updates[change.key].SetBalance(balance)
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

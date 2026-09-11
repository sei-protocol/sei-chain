package flatkv

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	"go.opentelemetry.io/otel/metric"
)

// ApplyChangeSets writes one block's changes into the four data stores. Non-EVM modules go to miscDB
// under "<module>/". Each value records version as the height it was last modified at; the same version
// must be passed to the subsequent Commit, which is what folds the block into the LtHash.
func (s *CommitStore) ApplyChangeSets(version int64, changeSets []*proto.NamedChangeSet) error {
	// The read-only refusal belongs here rather than in applyChangeSets, which a read-only store reaches
	// legitimately: building a view at a past height replays the primary's WAL through the same apply path.
	// Commit and outOfBandSnapshot place their refusals at the same boundary, and readOnly is fixed for a store's
	// lifetime, so reading it outside the lock is safe.
	if s.readOnly {
		return errReadOnly
	}
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
	changesByType, err := classifyAndPrefix(changeSets)
	if err != nil {
		return fmt.Errorf("classify changesets: %w", err)
	}
	// Every value the changeset carries is parsed here, before anything is written, so a malformed
	// block fails with no part of it in a store. What is left for the write is folding the account
	// changes onto the rows those accounts already hold; see accountUpdater.
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
		"writes", prepared.accountCount()+len(prepared.storage)+len(prepared.code)+len(prepared.misc),
		"elapsed", obs.elapsed())
	return nil
}

// preparedWrites holds one ApplyChangeSets call's values, per database. Storage, code and misc
// arrive as the values to write; accounts arrive as the changes to fold onto the rows they modify,
// which the account store resolves as it writes.
type preparedWrites struct {
	accounts *accountUpdater
	storage  map[string]*vtype.StorageData
	code     map[string]*vtype.CodeData
	misc     map[string]*vtype.MiscData
}

// accountCount reports how many accounts the block writes, treating a block that touches none as zero
// rather than requiring the caller to nil-check.
func (p preparedWrites) accountCount() int {
	if p.accounts == nil {
		return 0
	}
	return len(p.accounts.keys)
}

// prepareWrites applies EVM value semantics to one block's changes, parsing every value the
// changeset carries.
func (s *CommitStore) prepareWrites(
	changesByType map[keys.EVMKeyKind]map[string][]byte,
	blockHeight int64,
) (preparedWrites, error) {
	// Accounts are the one kind that has to be read back out of its store before it can be written,
	// and the other three databases' values do not depend on that read, so they are gathered while
	// it is in flight.
	var out preparedWrites
	var gatherErr error
	var gathered sync.WaitGroup
	gathered.Add(1)
	s.miscPool.Submit(func() {
		defer gathered.Done()
		out, gatherErr = gatherNonAccountValues(changesByType, blockHeight)
	})

	// Every account field value is parsed here, before anything is written, which is what keeps a
	// malformed changeset from leaving the account store half-updated: the rows themselves are folded
	// later, inside the write, where a failure would come after some of them had landed.
	s.phaseTimer.SetPhase("apply_change_sets_merge_accounts")
	updater, mergeErr := newAccountUpdater(
		changesByType[keys.EVMKeyNonce],
		changesByType[keys.EVMKeyCodeHash],
		changesByType[keys.EVMKeyBalance],
		blockHeight,
	)

	gathered.Wait()
	if mergeErr != nil {
		return preparedWrites{}, mergeErr
	}
	if gatherErr != nil {
		return preparedWrites{}, gatherErr
	}
	out.accounts = updater
	return out, nil
}

// gatherNonAccountValues turns one block's storage, code and misc changes into the values to write,
// per database. The accounts field of the result is left empty; see newAccountUpdater.
func gatherNonAccountValues(
	changesByType map[keys.EVMKeyKind]map[string][]byte,
	blockHeight int64,
) (preparedWrites, error) {
	var out preparedWrites

	storageWrites, err := toStorageValues(changesByType[keys.EVMKeyStorage], blockHeight)
	if err != nil {
		return preparedWrites{}, fmt.Errorf("failed to parse storage changes: %w", err)
	}

	codeWrites, err := toCodeValues(changesByType[keys.EVMKeyCode], blockHeight)
	if err != nil {
		return preparedWrites{}, fmt.Errorf("failed to parse code changes: %w", err)
	}

	miscWrites, err := toMiscValues(changesByType[keys.EVMKeyMisc], blockHeight)
	if err != nil {
		return preparedWrites{}, fmt.Errorf("failed to parse misc changes: %w", err)
	}

	out.storage = storageWrites
	out.code = codeWrites
	out.misc = miscWrites
	return out, nil
}

var _ view.BatchUpdater = (*accountUpdater)(nil)

// accountUpdater folds one block's per-field account changes onto the rows those accounts already
// hold.
//
// An account is stored as one row but written a field at a time, so a change carrying only a nonce or
// only a code hash has to be applied on top of the row as it stands. The row is read by the account
// store while it writes, rather than by this store beforehand, because the write already looks the
// key up: see view.ViewManager.BatchUpdate.
type accountUpdater struct {
	// pending is the fields this block set, keyed by physical key. Values are parsed when this is
	// built, so folding a row cannot fail on a malformed change.
	pending map[string]vtype.PendingAccountWrite

	// keys names every account the block touched. Held alongside pending because BatchUpdate needs
	// the keys as a slice, and building it here means walking the map once rather than once per
	// write.
	keys []string

	// blockHeight is stamped on every row written, whether or not any field value actually changed,
	// because GetBlockHeightModified reports it.
	blockHeight int64
}

// newAccountUpdater parses one batch's per-field account changes into the fields to set on each
// account. Reports nil when the batch touches no account.
func newAccountUpdater(
	nonceChanges map[string][]byte,
	codeHashChanges map[string][]byte,
	balanceChanges map[string][]byte,
	blockHeight int64,
) (*accountUpdater, error) {
	pending, err := mergeAccountUpdates(nonceChanges, codeHashChanges, balanceChanges)
	if err != nil {
		return nil, fmt.Errorf("failed to gather account updates: %w", err)
	}
	if len(pending) == 0 {
		return nil, nil
	}

	physKeys := make([]string, 0, len(pending))
	for key := range pending {
		physKeys = append(physKeys, key)
	}
	return &accountUpdater{pending: pending, keys: physKeys, blockHeight: blockHeight}, nil
}

// NewValueFor folds this block's changes to one account onto the row it already holds. An account the
// store does not hold starts from zero, and a row left with no balance, nonce or code hash is deleted.
func (u *accountUpdater) NewValueFor(key string, priorValue []byte) ([]byte, error) {
	var stored *vtype.AccountData
	if priorValue != nil {
		parsed, err := vtype.DeserializeAccountData(priorValue)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize accountDB old value: %w", err)
		}
		stored = parsed
	}

	// Copied out of the map so the pointer-receiver methods have something addressable to work on.
	pending := u.pending[key]
	// Merge copies rather than writing through, so the value handed back does not alias the row the
	// store still holds for earlier versions.
	merged := pending.Merge(stored, u.blockHeight)
	if merged.IsDelete() {
		return nil, nil
	}
	return merged.Serialize(), nil
}

// writeToStores writes one successful ApplyChangeSets batch into the four data stores and records the
// changesets and the block height they belong to.
//
// A store that already has this block is skipped. That happens only when a startup replay is catching
// the stores up to each other, where its hash already includes the block and writing it again would
// count it twice.
func (s *CommitStore) writeToStores(
	prepared preparedWrites,
	changeSets []*proto.NamedChangeSet,
	version int64,
	alreadyHave map[string]int64,
) error {
	s.phaseTimer.SetPhase("apply_change_write_to_stores")

	// The four databases are independent view managers with independent locks, so their writes run
	// concurrently rather than one store's fan-out waiting on the last. Account and storage carry
	// most of a block between them, so overlapping the two is most of the win.
	writes := []func() error{
		// TODO: currently, WAL replay may replay blocks already in some stores. In the future when WAL replay
		// is external, we may be able to simplify this code since we will be able to assume that all stores
		// start at the same block.
		// Accounts alone are written by folding onto what the store already holds, so they take a
		// different path: see accountUpdater.
		writeAccountStore(s, prepared.accounts, version, alreadyHave),
		writeStore(s, s.storageStore, storageDBDir, prepared.storage, version, alreadyHave),
		writeStore(s, s.codeStore, codeDBDir, prepared.code, version, alreadyHave),
		writeStore(s, s.miscStore, miscDBDir, prepared.misc, version, alreadyHave),
	}
	errs := make([]error, len(writes))
	var wg sync.WaitGroup
	for i, write := range writes {
		wg.Add(1)
		s.miscPool.Submit(func() {
			defer wg.Done()
			errs[i] = write()
		})
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return err
	}

	s.pendingChangeSets = append(s.pendingChangeSets, changeSets...)
	s.pendingBlockHeight = version
	return nil
}

// writeAccountStore returns the write of the block's accounts, each folded onto the row its key
// already holds, or a no-op for a store that already holds this block.
func writeAccountStore(
	s *CommitStore,
	updater *accountUpdater,
	version int64,
	alreadyHave map[string]int64,
) func() error {
	return func() error {
		if alreadyHave[accountDBDir] >= version || updater == nil {
			return nil
		}
		start := time.Now()
		err := s.accountStore.BatchUpdate(updater.keys, updater)
		otelMetrics.AccountUpdateLatency.Record(s.ctx, secondsSince(start),
			metric.WithAttributes(successAttr(err)))
		if err != nil {
			return fmt.Errorf("write %s values: %w", accountDBDir, err)
		}
		addKVPairs(s.ctx, accountDBDir, len(updater.keys))
		return nil
	}
}

// writeStore returns the write of one database's values, or a no-op for a store that already holds
// this block.
func writeStore[T vtype.VType](
	s *CommitStore,
	store view.ViewManager,
	dbDir string,
	values map[string]T,
	version int64,
	alreadyHave map[string]int64,
) func() error {
	return func() error {
		if alreadyHave[dbDir] >= version {
			return nil
		}
		if err := serializeAndPut(store, values); err != nil {
			return fmt.Errorf("write %s values: %w", dbDir, err)
		}
		addKVPairs(s.ctx, dbDir, len(values))
		return nil
	}
}

// serializeAndPut writes values into the store's current version, to be sealed by the next Commit. A
// value reporting IsDelete becomes a deletion; every other value is stored as its serialized form.
//
// values is keyed by physical key.
func serializeAndPut[T vtype.VType](store view.ViewManager, values map[string]T) error {
	if len(values) == 0 {
		return nil
	}
	// One slice of values rather than a slice of pointers, and the physical keys handed over as the
	// strings they already are: the store keys its own structures by string, so converting them to
	// []byte here only to have them converted back is the whole cost of this loop.
	pairs := make([]view.BatchKVPair, 0, len(values))
	for key, value := range values {
		if value.IsDelete() {
			pairs = append(pairs, view.BatchKVPair{Key: key, Delete: true})
			continue
		}
		pairs = append(pairs, view.BatchKVPair{Key: key, Value: value.Serialize()})
	}
	if err := store.BatchSet(pairs); err != nil {
		return fmt.Errorf("batch write: %w", err)
	}
	return nil
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
// In the result the inner string is a physical key and its value is that key's new raw bytes, with nil
// meaning the key was deleted.
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
		if cs == nil || len(cs.Changeset.Pairs) == 0 {
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
			// An empty module name would fold into "/"+key here and later
			// persist as the per-module meta key "_meta/x:/hash", which
			// ParseModuleLtHashKey rejects on reload — a store that ever
			// commits one becomes permanently unopenable (sum-to-root check
			// fails forever). Reject it up front instead; module names are
			// never empty in normal operation (Cosmos SDK's NewKVStoreKey
			// panics on an empty name), so this only guards malformed input.
			if cs.Name == "" {
				return nil, fmt.Errorf("flatkv: empty module name in changeset")
			}
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

// toCodeValues turns raw code changes into CodeData stamped with blockHeight. A nil change is a
// deletion, which for code means empty bytecode. Both maps are keyed by physical key.
func toCodeValues(
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

// toMiscValues turns raw misc changes into MiscData stamped with blockHeight. A nil change is a
// deletion, which for misc means an empty value. Both maps are keyed by physical key.
func toMiscValues(
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

// mergeAccountUpdates folds a block's per-field account changes into one pending write per account,
// parsing every value as it goes so a malformed change fails here rather than mid-write.
//
// The map holds pending writes by value: a block touches thousands of accounts, and a pointer per
// account was measured as most of this function's cost.
func mergeAccountUpdates(
	nonceChanges map[string][]byte,
	codeHashChanges map[string][]byte,
	balanceChanges map[string][]byte,
) (map[string]vtype.PendingAccountWrite, error) {

	updates := make(map[string]vtype.PendingAccountWrite,
		len(nonceChanges)+len(codeHashChanges)+len(balanceChanges))

	for key, nonceChange := range nonceChanges {
		// Deletion is equivalent to setting the nonce to 0.
		var nonce uint64
		if nonceChange != nil {
			parsed, err := vtype.ParseNonce(nonceChange)
			if err != nil {
				return nil, fmt.Errorf("invalid nonce value: %w", err)
			}
			nonce = parsed
		}
		pending := updates[key]
		pending.SetNonce(nonce)
		updates[key] = pending
	}

	for key, codeHashChange := range codeHashChanges {
		// Deletion is equivalent to setting the code hash to a zero hash.
		pending := updates[key]
		if codeHashChange == nil {
			pending.SetCodeHash(nil)
		} else if _, err := pending.SetCodeHashBytes(codeHashChange); err != nil {
			return nil, fmt.Errorf("invalid codehash value: %w", err)
		}
		updates[key] = pending
	}

	for key, balanceChange := range balanceChanges {
		// Deletion is equivalent to setting the balance to a zero balance.
		pending := updates[key]
		if balanceChange == nil {
			pending.SetBalance(nil)
		} else {
			balance, err := vtype.ParseBalance(balanceChange)
			if err != nil {
				return nil, fmt.Errorf("invalid balance value: %w", err)
			}
			pending.SetBalance(balance)
		}
		updates[key] = pending
	}
	return updates, nil
}

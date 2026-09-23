package flatkv

import (
	"context"
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
		"writes", prepared.accountCount()+len(prepared.storage)+len(prepared.code)+len(prepared.misc),
		"elapsed", obs.elapsed())
	return nil
}

// preparedWrites holds the fully-validated per-database values and LtHash pairs for one
// ApplyChangeSets call. Nothing here reaches a store until every kind has validated — see
// writeToStores.
type preparedWrites struct {
	accounts *accountUpdater
	storage  []view.Write
	code     []view.Write
	misc     []view.Write
}

// prepareWrites applies EVM value semantics and returns the values to write, per database.
func (s *CommitStore) prepareWrites(
	changesByType map[keys.EVMKeyKind]map[string][]byte,
	blockHeight int64,
) (preparedWrites, error) {
	s.phaseTimer.SetPhase("apply_change_sets_gather_values")

	var wg sync.WaitGroup
	wg.Add(4)

	var accountWrites *accountUpdater
	var accountErr error
	s.miscPool.Submit(func() {
		defer wg.Done()
		writes, err := newAccountUpdater(
			changesByType[keys.EVMKeyNonce],
			changesByType[keys.EVMKeyCodeHash],
			changesByType[keys.EVMKeyBalance],
			blockHeight,
		)
		if err != nil {
			accountErr = fmt.Errorf("prepare account writes for block %d: %w", blockHeight, err)
			return
		}
		accountWrites = writes
	})

	var storageWrites []view.Write
	var storageErr error
	s.miscPool.Submit(func() {
		defer wg.Done()
		writes, err := toStorageValues(changesByType[keys.EVMKeyStorage], blockHeight)
		if err != nil {
			storageErr = fmt.Errorf("failed to parse storage changes: %w", err)
			return
		}
		storageWrites = writes
	})

	var codeWrites []view.Write
	var codeErr error
	s.miscPool.Submit(func() {
		defer wg.Done()
		writes, err := toCodeValues(changesByType[keys.EVMKeyCode], blockHeight)
		if err != nil {
			codeErr = fmt.Errorf("failed to parse code changes: %w", err)
			return
		}
		codeWrites = writes
	})

	var miscWrites []view.Write
	var miscErr error
	s.miscPool.Submit(func() {
		defer wg.Done()
		writes, err := toMiscValues(changesByType[keys.EVMKeyMisc], blockHeight)
		if err != nil {
			miscErr = fmt.Errorf("failed to parse misc changes: %w", err)
			return
		}
		miscWrites = writes
	})

	// Every kind has to validate before any of them is returned: writeToStores stages rows, so a parse
	// failure discovered after it ran would leave part of a block behind.
	wg.Wait()
	if err := errors.Join(accountErr, storageErr, codeErr, miscErr); err != nil {
		return preparedWrites{}, err
	}

	return preparedWrites{
		accounts: accountWrites,
		storage:  storageWrites,
		code:     codeWrites,
		misc:     miscWrites,
	}, nil
}

var _ view.BatchUpdater = (*accountUpdater)(nil)

// accountUpdater folds one block's per-field account changes onto the rows those accounts already
// hold.
//
// An account is stored as one row but written a field at a time, so a change carrying only a nonce or
// only a code hash has to be applied on top of the row as it stands. The account store does that fold
// on its own threads, after the write has been staged, so no part of it runs on the thread applying
// the block.
type accountUpdater struct {
	// pending is the fields this block set, keyed by physical key. Parsed up front, so a fold can
	// never fail on a malformed change.
	pending map[string]vtype.PendingAccountWrite

	// keys names every account the block touched, in the form BatchUpdate takes them.
	keys []string

	// blockHeight is stamped on every row written, whether or not any field value changed, because
	// GetBlockHeightModified reports it.
	blockHeight int64
}

// newAccountUpdater parses one batch's per-field account changes into the fields to set on each
// account. Reports nil when the batch touches no account.
//
// Parsing here rather than during the fold is what keeps a malformed changeset from being discovered
// halfway through writing the block: by the time the folds run, the block has already been accepted.
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

// accountCount reports how many accounts the block writes, treating a block that touches none as zero
// rather than requiring the caller to nil-check.
func (p preparedWrites) accountCount() int {
	if p.accounts == nil {
		return 0
	}
	return len(p.accounts.keys)
}

// writeToStores writes one block's prepared values into the four data stores.
func (s *CommitStore) writeToStores(
	prepared preparedWrites,
	changeSets []*proto.NamedChangeSet,
	version int64,
	alreadyHave map[string]int64,
) error {
	s.phaseTimer.SetPhase("apply_change_write_to_stores")

	// The four databases are independent view managers with independent locks, so their writes run
	// concurrently rather than one store's fan-out waiting on the last.
	var wg sync.WaitGroup
	wg.Add(4)

	var accountErr error
	s.miscPool.Submit(func() {
		defer wg.Done()
		accountErr = s.writeAccountStore(prepared.accounts, version, alreadyHave)
	})
	var storageErr error
	s.miscPool.Submit(func() {
		defer wg.Done()
		storageErr = writeStore(s.ctx, s.storageStore, storageDBDir, prepared.storage, version, alreadyHave)
	})
	var codeErr error
	s.miscPool.Submit(func() {
		defer wg.Done()
		codeErr = writeStore(s.ctx, s.codeStore, codeDBDir, prepared.code, version, alreadyHave)
	})
	var miscErr error
	s.miscPool.Submit(func() {
		defer wg.Done()
		miscErr = writeStore(s.ctx, s.miscStore, miscDBDir, prepared.misc, version, alreadyHave)
	})

	wg.Wait()
	if err := errors.Join(accountErr, storageErr, codeErr, miscErr); err != nil {
		return err
	}

	s.pendingChangeSets = append(s.pendingChangeSets, changeSets...)
	s.pendingBlockHeight = version
	return nil
}

// writeStore writes one database's values, and is a no-op for a store that already holds this block.
func writeStore(
	ctx context.Context,
	store view.ViewManager,
	dbDir string,
	writes []view.Write,
	version int64,
	alreadyHave map[string]int64,
) error {
	if alreadyHave[dbDir] >= version {
		// A store already holds the block only when a startup replay is catching the stores up to each
		// other, where its hash already includes the block and writing it again would count it twice.
		//
		// TODO: currently, WAL replay may replay blocks already in some stores. In the future when WAL
		// replay is external, we may be able to simplify this code since we will be able to assume that
		// all stores start at the same block.
		return nil
	}
	if len(writes) == 0 {
		return nil
	}
	if err := store.BatchSet(writes); err != nil {
		return fmt.Errorf("write %s values: %w", dbDir, err)
	}
	addKVPairs(ctx, dbDir, len(writes))
	return nil
}

// writeAccountStore writes the block's accounts, each folded onto the row its key already holds. A batch
// touching no account, and a store that already holds this block, are both no-ops.
func (s *CommitStore) writeAccountStore(
	updater *accountUpdater,
	version int64,
	alreadyHave map[string]int64,
) error {
	if alreadyHave[accountDBDir] >= version || updater == nil {
		// The store already holding the block is the replay case described in writeStore().
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

// moduleOfKey extracts the owning module from a physical key. Injected into the
// lthash HashCalculator so it can bucket pairs by module without importing ktype
// (ktype already imports lthash).
func moduleOfKey(physicalKey []byte) (string, error) {
	module, _, err := ktype.StripModulePrefix(physicalKey)
	if err != nil {
		return "", fmt.Errorf("strip the module prefix from key %x: %w", physicalKey, err)
	}
	return module, nil
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

	keyBuf := make([]byte, 0, physKeyBufLen)
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
					keyBuf = ktype.AppendModulePhysicalKey(keyBuf[:0], keys.EVMStoreKey, pair.Key)
				} else {
					keyBuf = ktype.AppendEVMPhysicalKey(keyBuf[:0], kind, keyBytes)
				}
				physKey = string(keyBuf)

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
				keyBuf = ktype.AppendModulePhysicalKey(keyBuf[:0], cs.Name, pair.Key)
				physKey := string(keyBuf)
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

// toStorageValues turns raw storage changes into the writes the storage store takes, stamped with
// blockHeight. rawChanges is keyed by physical key, and a nil change is a deletion — as is a value
// of all zeros, which is the same thing for storage.
func toStorageValues(
	rawChanges map[string][]byte,
	blockHeight int64,
) ([]view.Write, error) {
	writes := make([]view.Write, 0, len(rawChanges))

	for keyStr, rawChange := range rawChanges {
		if rawChange == nil {
			writes = append(writes, view.Write{Key: keyStr})
			continue
		}
		value, err := vtype.SerializeStorage(blockHeight, rawChange)
		if err != nil {
			return nil, fmt.Errorf("failed to parse storage value: %w", err)
		}
		writes = append(writes, view.Write{Key: keyStr, Value: value})
	}

	return writes, nil
}

// toCodeValues turns raw code changes into the writes the code store takes, stamped with
// blockHeight. rawChanges is keyed by physical key, and a nil change is a deletion — as is empty
// bytecode, which is the same thing for code.
func toCodeValues(
	rawChanges map[string][]byte,
	blockHeight int64,
) ([]view.Write, error) {
	writes := make([]view.Write, 0, len(rawChanges))

	for keyStr, rawChange := range rawChanges {
		if len(rawChange) == 0 {
			writes = append(writes, view.Write{Key: keyStr})
			continue
		}
		writes = append(writes, view.Write{Key: keyStr, Value: vtype.SerializeCode(blockHeight, rawChange)})
	}
	return writes, nil
}

// toMiscValues turns raw misc changes into the writes the misc store takes, stamped with
// blockHeight. rawChanges is keyed by physical key, and only a nil change is a deletion: an empty
// value is a write a Cosmos module may legitimately make. See nonNilValue.
func toMiscValues(
	rawChanges map[string][]byte,
	blockHeight int64,
) ([]view.Write, error) {
	writes := make([]view.Write, 0, len(rawChanges))

	for keyStr, rawChange := range rawChanges {
		if rawChange == nil {
			writes = append(writes, view.Write{Key: keyStr})
			continue
		}
		writes = append(writes, view.Write{Key: keyStr, Value: vtype.SerializeMisc(blockHeight, rawChange)})
	}
	return writes, nil
}

// Merge account updates down into a single update per account.
func mergeAccountUpdates(
	nonceChanges map[string][]byte,
	codeHashChanges map[string][]byte,
	balanceChanges map[string][]byte,
) (map[string]vtype.PendingAccountWrite, error) {

	updates := make(map[string]vtype.PendingAccountWrite,
		len(nonceChanges)+len(codeHashChanges)+len(balanceChanges))

	for key, nonceChange := range nonceChanges {
		// Deletion is equivalent to setting the nonce to 0
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
		pending := updates[key]
		if codeHashChange == nil {
			// Deletion is equivalent to setting the code hash to a zero hash
			pending.SetCodeHash(nil)
		} else if _, err := pending.SetCodeHashBytes(codeHashChange); err != nil {
			return nil, fmt.Errorf("invalid codehash value: %w", err)
		}
		updates[key] = pending
	}

	for key, balanceChange := range balanceChanges {
		// Deletion is equivalent to setting the balance to a zero balance
		var balance *vtype.Balance
		if balanceChange != nil {
			parsed, err := vtype.ParseBalance(balanceChange)
			if err != nil {
				return nil, fmt.Errorf("invalid balance value: %w", err)
			}
			balance = parsed
		}
		pending := updates[key]
		pending.SetBalance(balance)
		updates[key] = pending
	}
	return updates, nil
}

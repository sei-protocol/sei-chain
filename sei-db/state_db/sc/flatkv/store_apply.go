package flatkv

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	"go.opentelemetry.io/otel/metric"
)

// classifyBucketHeadroom is the factor applied to a kind's bucket length in the previous ApplyChangeSets call to
// size that bucket in the next.
const classifyBucketHeadroom = 2

// classifyUnitSize is the most changeset pairs one worker classifies.
const classifyUnitSize = 1024

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
	changesByType, err := classifyAndPrefix(changeSets, s.classifyBucketSizes, s.miscPool)
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
	changesByType classifiedChanges,
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
	nonceChanges []classifiedChange,
	codeHashChanges []classifiedChange,
	balanceChanges []classifiedChange,
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

// classifiedChange is one changeset pair with its physical key already built.
type classifiedChange struct {
	// key is the physical key: "module/" + the module's encoded key. It is carved from a keyArena, so anything that
	// keeps it past the version that writes it must copy it.
	key string

	// value is the key's new raw bytes. A nil value means the key was deleted.
	value []byte
}

// classifiedChanges holds one ApplyChangeSets call's pairs bucketed by EVM key kind, each bucket in the order the
// pairs arrived. A key written more than once appears once per write, and the last of them is its new value.
type classifiedChanges [keys.EVMKeyKindCount][]classifiedChange

// bucketSizes returns the number of pairs in each kind's bucket.
func (c *classifiedChanges) bucketSizes() [keys.EVMKeyKindCount]int {
	var sizes [keys.EVMKeyKindCount]int
	for kind, bucket := range c {
		sizes[kind] = len(bucket)
	}
	return sizes
}

// classifyAndPrefix splits changeSets into per-EVMKeyKind buckets whose keys are already in physical format
// ("module/" + prefix_encoded_key). Non-EVM modules go to the EVMKeyMisc bucket with a "<module>/" prefix.
//
// sizeHints gives each kind's bucket length in an earlier call, and a kind with no hint grows on demand. The pairs
// are classified on pool when there is more than one unit of them, and on the calling goroutine when pool is nil.
func classifyAndPrefix(
	changeSets []*proto.NamedChangeSet,
	sizeHints [keys.EVMKeyKindCount]int,
	pool threading.Pool,
) (classifiedChanges, error) {
	// Repeated keys are kept rather than resolved here. Every consumer already resolves them in arrival order:
	// the view manager keeps a key's last write in a version, and mergeAccountUpdates folds each account into
	// one entry. Import input has unique keys.
	units := planClassifyUnits(changeSets)
	if pool == nil || len(units) < 2 {
		return classifyUnitsSerially(units, sizeHints)
	}
	return classifyUnitsInParallel(units, sizeHints, pool)
}

// classifyUnit is a contiguous run of one changeset's pairs.
type classifyUnit struct {
	// moduleName is the name of the changeset the pairs belong to.
	moduleName string

	// pairs is the run itself, at most classifyUnitSize long.
	pairs []*proto.KVPair
}

// planClassifyUnits divides changeSets into units of at most classifyUnitSize pairs, in block order. A unit never
// spans two changesets.
func planClassifyUnits(changeSets []*proto.NamedChangeSet) []classifyUnit {
	var units []classifyUnit
	for _, cs := range changeSets {
		if cs == nil {
			continue
		}
		pairs := cs.Changeset.Pairs
		for len(pairs) > 0 {
			count := min(len(pairs), classifyUnitSize)
			units = append(units, classifyUnit{moduleName: cs.Name, pairs: pairs[:count]})
			pairs = pairs[count:]
		}
	}
	return units
}

// classifyUnitsSerially classifies every unit on the calling goroutine, into one set of buckets.
func classifyUnitsSerially(
	units []classifyUnit,
	sizeHints [keys.EVMKeyKindCount]int,
) (classifiedChanges, error) {
	result := newClassifiedChanges(sizeHints)
	for _, unit := range units {
		if err := classifyUnitPairs(unit, &result); err != nil {
			return classifiedChanges{}, err
		}
	}
	return result, nil
}

// classifyUnitsInParallel classifies each unit on pool into its own set of buckets, then concatenates them in unit
// order.
func classifyUnitsInParallel(
	units []classifyUnit,
	sizeHints [keys.EVMKeyKindCount]int,
	pool threading.Pool,
) (classifiedChanges, error) {
	// Each unit is sized for its share of the call rather than all of it. A unit that receives an uneven share of
	// some kind grows that bucket on demand.
	var unitHints [keys.EVMKeyKindCount]int
	for kind, hint := range sizeHints {
		unitHints[kind] = hint / len(units)
	}

	parts := make([]classifiedChanges, len(units))
	errs := make([]error, len(units))

	var wg sync.WaitGroup
	wg.Add(len(units))
	for i := range units {
		pool.Submit(func() {
			defer wg.Done()
			parts[i] = newClassifiedChanges(unitHints)
			errs[i] = classifyUnitPairs(units[i], &parts[i])
		})
	}
	// The calling goroutine blocks rather than classifying a unit of its own. The runtime queues the last worker
	// woken to run next on this goroutine's own CPU, so a unit classified here would delay that worker until it
	// finished, running the two units back to back.
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return classifiedChanges{}, err
		}
	}
	return mergeClassified(parts), nil
}

// newClassifiedChanges returns empty buckets, each with room for classifyBucketHeadroom times its hint.
func newClassifiedChanges(sizeHints [keys.EVMKeyKindCount]int) classifiedChanges {
	var result classifiedChanges
	for kind, hint := range sizeHints {
		if hint > 0 {
			result[kind] = make([]classifiedChange, 0, classifyBucketHeadroom*hint)
		}
	}
	return result
}

// classifyUnitPairs appends each of unit's pairs, with its physical key built, to the bucket for its kind.
func classifyUnitPairs(unit classifyUnit, into *classifiedChanges) error {
	keyBuf := make([]byte, 0, physKeyBufLen)
	var arena keyArena

	if unit.moduleName == keys.EVMStoreKey {
		for _, pair := range unit.pairs {
			kind, keyBytes := keys.ParseEVMKey(pair.Key)
			if kind == keys.EVMKeyEmpty {
				return fmt.Errorf("flatkv: empty key in changeset")
			}

			if kind == keys.EVMKeyMisc {
				keyBuf = ktype.AppendModulePhysicalKey(keyBuf[:0], keys.EVMStoreKey, pair.Key)
			} else {
				keyBuf = ktype.AppendEVMPhysicalKey(keyBuf[:0], kind, keyBytes)
			}
			into[kind] = append(into[kind], newClassifiedChange(arena.intern(keyBuf), pair))
		}
		return nil
	}

	// An empty module name would fold into "/"+key here and later
	// persist as the per-module meta key "_meta/x:/hash", which
	// ParseModuleLtHashKey rejects on reload — a store that ever
	// commits one becomes permanently unopenable (sum-to-root check
	// fails forever). Reject it up front instead; module names are
	// never empty in normal operation (Cosmos SDK's NewKVStoreKey
	// panics on an empty name), so this only guards malformed input.
	if unit.moduleName == "" {
		return fmt.Errorf("flatkv: empty module name in changeset")
	}
	miscBucket := &into[keys.EVMKeyMisc]
	for _, pair := range unit.pairs {
		keyBuf = ktype.AppendModulePhysicalKey(keyBuf[:0], unit.moduleName, pair.Key)
		*miscBucket = append(*miscBucket, newClassifiedChange(arena.intern(keyBuf), pair))
	}
	return nil
}

// mergeClassified concatenates each part's buckets in part order.
func mergeClassified(parts []classifiedChanges) classifiedChanges {
	var totals [keys.EVMKeyKindCount]int
	for i := range parts {
		for kind, bucket := range parts[i] {
			totals[kind] += len(bucket)
		}
	}

	var result classifiedChanges
	for kind, total := range totals {
		if total > 0 {
			result[kind] = make([]classifiedChange, 0, total)
		}
	}
	for i := range parts {
		for kind, bucket := range parts[i] {
			result[kind] = append(result[kind], bucket...)
		}
	}
	return result
}

// newClassifiedChange pairs a physical key with a changeset pair's new value, recording a deleted pair as a nil
// value.
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

// toStorageValues turns raw storage changes into the writes the storage store takes, stamped with
// blockHeight, one write per change and in the same order. A nil change is a deletion — as is a value
// of all zeros, which is the same thing for storage.
func toStorageValues(
	rawChanges []classifiedChange,
	blockHeight int64,
) ([]view.Write, error) {
	writes := make([]view.Write, 0, len(rawChanges))

	for _, change := range rawChanges {
		if change.value == nil {
			writes = append(writes, view.Write{Key: change.key})
			continue
		}
		value, err := vtype.SerializeStorage(blockHeight, change.value)
		if err != nil {
			return nil, fmt.Errorf("failed to parse storage value: %w", err)
		}
		writes = append(writes, view.Write{Key: change.key, Value: value})
	}

	return writes, nil
}

// toCodeValues turns raw code changes into the writes the code store takes, stamped with
// blockHeight, one write per change and in the same order. A nil change is a deletion — as is empty
// bytecode, which is the same thing for code.
func toCodeValues(
	rawChanges []classifiedChange,
	blockHeight int64,
) ([]view.Write, error) {
	writes := make([]view.Write, 0, len(rawChanges))

	for _, change := range rawChanges {
		if len(change.value) == 0 {
			writes = append(writes, view.Write{Key: change.key})
			continue
		}
		value := vtype.SerializeCode(blockHeight, change.value)
		writes = append(writes, view.Write{Key: change.key, Value: value})
	}
	return writes, nil
}

// toMiscValues turns raw misc changes into the writes the misc store takes, stamped with
// blockHeight, one write per change and in the same order. Only a nil change is a deletion: an empty
// value is a write a Cosmos module may legitimately make. See nonNilValue.
func toMiscValues(
	rawChanges []classifiedChange,
	blockHeight int64,
) ([]view.Write, error) {
	writes := make([]view.Write, 0, len(rawChanges))

	for _, change := range rawChanges {
		if change.value == nil {
			writes = append(writes, view.Write{Key: change.key})
			continue
		}
		value := vtype.SerializeMisc(blockHeight, change.value)
		writes = append(writes, view.Write{Key: change.key, Value: value})
	}
	return writes, nil
}

// mergeAccountUpdates folds per-field account changes into a single update per account. Where a field of one
// account changes more than once, the last change wins.
func mergeAccountUpdates(
	nonceChanges []classifiedChange,
	codeHashChanges []classifiedChange,
	balanceChanges []classifiedChange,
) (map[string]vtype.PendingAccountWrite, error) {

	updates := make(map[string]vtype.PendingAccountWrite,
		len(nonceChanges)+len(codeHashChanges)+len(balanceChanges))

	for _, change := range nonceChanges {
		// Deletion is equivalent to setting the nonce to 0
		var nonce uint64
		if change.value != nil {
			parsed, err := vtype.ParseNonce(change.value)
			if err != nil {
				return nil, fmt.Errorf("invalid nonce value: %w", err)
			}
			nonce = parsed
		}
		pending := updates[change.key]
		pending.SetNonce(nonce)
		updates[change.key] = pending
	}

	for _, change := range codeHashChanges {
		pending := updates[change.key]
		if change.value == nil {
			// Deletion is equivalent to setting the code hash to a zero hash
			pending.SetCodeHash(nil)
		} else if err := pending.SetCodeHashBytes(change.value); err != nil {
			return nil, fmt.Errorf("invalid codehash value: %w", err)
		}
		updates[change.key] = pending
	}

	for _, change := range balanceChanges {
		// Deletion is equivalent to setting the balance to a zero balance
		var balance *vtype.Balance
		if change.value != nil {
			parsed, err := vtype.ParseBalance(change.value)
			if err != nil {
				return nil, fmt.Errorf("invalid balance value: %w", err)
			}
			balance = parsed
		}
		pending := updates[change.key]
		pending.SetBalance(balance)
		updates[change.key] = pending
	}
	return updates, nil
}

package flatkv

import (
	"errors"
	"fmt"
	"iter"
	"sync"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
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

	s.phaseTimer.SetPhase("apply_change_sets_read_accounts")
	readStart := time.Now()
	accounts, readErr := s.readAccountsToMerge(changesByType, blockHeight)
	otelMetrics.BatchReadOldValuesLatency.Record(s.ctx, secondsSince(readStart),
		metric.WithAttributes(successAttr(readErr)))

	// The other three databases are gathered off this thread, so what is left here is waiting for
	// that to land and folding the changes onto the accounts just read.
	s.phaseTimer.SetPhase("apply_change_sets_merge_accounts")
	gathered.Wait()
	if readErr != nil {
		return preparedWrites{}, readErr
	}
	if gatherErr != nil {
		return preparedWrites{}, gatherErr
	}

	if err := mergeAccountValues(accounts, &changesByType); err != nil {
		return preparedWrites{}, fmt.Errorf("failed to gather account updates: %w", err)
	}
	out.accounts = accounts
	return out, nil
}

// gatherNonAccountValues turns one block's storage, code and misc changes into the values to write,
// per database. The accounts field of the result is left empty; see mergeAccountValues.
func gatherNonAccountValues(
	changesByType classifiedChanges,
	blockHeight int64,
) (preparedWrites, error) {
	var out preparedWrites

	storageWrites, err := toStorageValues(changesByType.changes(keys.EVMKeyStorage),
		changesByType.count(keys.EVMKeyStorage), blockHeight)
	if err != nil {
		return preparedWrites{}, fmt.Errorf("failed to parse storage changes: %w", err)
	}

	codeWrites, err := toCodeValues(changesByType.changes(keys.EVMKeyCode),
		changesByType.count(keys.EVMKeyCode), blockHeight)
	if err != nil {
		return preparedWrites{}, fmt.Errorf("failed to parse code changes: %w", err)
	}

	miscWrites, err := toMiscValues(changesByType.changes(keys.EVMKeyMisc),
		changesByType.count(keys.EVMKeyMisc), blockHeight)
	if err != nil {
		return preparedWrites{}, fmt.Errorf("failed to parse misc changes: %w", err)
	}

	out.storage = storageWrites
	out.code = codeWrites
	out.misc = miscWrites
	return out, nil
}

// readAccountsToMerge returns the account that each of this batch's nonce and codehash changes will
// be merged onto, keyed by physical key and stamped with blockHeight. An account the store does not
// hold starts from zero.
//
// An account is stored as one row but written a field at a time, so a change carrying only a nonce or
// only a code hash has to be applied on top of the account as it stands right now — a live read,
// since anything an earlier call at this height wrote counts.
func (s *CommitStore) readAccountsToMerge(
	changesByType classifiedChanges,
	blockHeight int64,
) (map[string]*vtype.AccountData, error) {
	accounts := touchedAccounts(changesByType)
	if len(accounts) == 0 {
		return nil, nil
	}

	physKeys := make([]string, 0, len(accounts))
	for key := range accounts {
		physKeys = append(physKeys, key)
	}
	stored := make([][]byte, len(physKeys))
	if err := s.accountStore.BatchGetStringInto(physKeys, stored); err != nil {
		return nil, fmt.Errorf("read accounts to merge onto: %w", err)
	}

	if err := populateAccounts(accounts, physKeys, stored, blockHeight); err != nil {
		return nil, err
	}
	return accounts, nil
}

// touchedAccounts returns one entry per account this batch's nonce and codehash changes name, with
// no value yet. Keys come from both kinds, since either can name an account the other does not.
//
// The map the accounts will be read into doubles as the set of keys to read, so a block's accounts
// are hashed once rather than once per structure they pass through.
func touchedAccounts(changesByType classifiedChanges) map[string]*vtype.AccountData {
	accounts := make(map[string]*vtype.AccountData,
		changesByType.count(keys.EVMKeyNonce)+changesByType.count(keys.EVMKeyCodeHash))
	for _, kind := range []keys.EVMKeyKind{keys.EVMKeyNonce, keys.EVMKeyCodeHash} {
		for change := range changesByType.changes(kind) {
			accounts[change.key] = nil
		}
	}
	return accounts
}

// populateAccounts gives every account in accounts its value: the account database's stored value
// where there is one, and a zero account everywhere else, each stamped with blockHeight.
//
// keys and stored are parallel, as the batch read leaves them: stored[i] is the account database's
// value for keys[i], or nil where it held none. keys must name every account in accounts, which is
// what leaves none of them without a value.
func populateAccounts(
	accounts map[string]*vtype.AccountData,
	keys []string,
	stored [][]byte,
	blockHeight int64,
) error {
	for i, value := range stored {
		if value == nil {
			accounts[keys[i]] = vtype.NewAccountData().SetBlockHeight(blockHeight)
			continue
		}
		account, err := vtype.DeserializeAccountData(value)
		if err != nil {
			return fmt.Errorf("failed to deserialize accountDB old value: %w", err)
		}
		accounts[keys[i]] = account.SetBlockHeight(blockHeight)
	}
	return nil
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

	// The four databases are independent engines with independent locks, so their writes run
	// concurrently rather than one store's fan-out waiting on the last. Account and storage carry
	// most of a block between them, so overlapping the two is most of the win.
	writes := []func() error{
		// TODO: currently, WAL replay may replay blocks already in some stores. In the future when WAL replay
		// is external, we may be able to simplify this code since we will be able to assume that all stores
		// start at the same block.
		writeStore(s, s.accountStore, accountDBDir, prepared.accounts, version, alreadyHave),
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

// writeStore returns the write of one database's values, or a no-op for a store that already holds
// this block. A store is skipped only during a startup replay catching the stores up to each other,
// where its hash already includes the block and writing it again would count it twice.
func writeStore[T vtype.VType](
	s *CommitStore,
	store snapshot.SnapshotEngine,
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
func serializeAndPut[T vtype.VType](store snapshot.SnapshotEngine, values map[string]T) error {
	if len(values) == 0 {
		return nil
	}
	// One slice of values rather than a slice of pointers, and the physical keys handed over as the
	// strings they already are: the store keys its own structures by string, so converting them to
	// []byte here only to have them converted back is the whole cost of this loop.
	pairs := make([]snapshot.StringKVPair, 0, len(values))
	for key, value := range values {
		if value.IsDelete() {
			pairs = append(pairs, snapshot.StringKVPair{Key: key, Delete: true})
			continue
		}
		pairs = append(pairs, snapshot.StringKVPair{Key: key, Value: value.Serialize()})
	}
	if err := store.BatchSetString(pairs); err != nil {
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
//
// A kind's pairs are spread across one bucket set per partition rather than gathered into one slice.
// Classification runs in parallel over contiguous partitions of the block, and each fills only its
// own set, so gathering them would mean allocating a second copy of every bucket. Walking the
// partitions in index order recovers the block's order, which is what the last-write-wins above
// depends on.
type classifiedChanges struct {
	// buckets[partition][kind] holds the changes of that kind the partition saw, in the order it saw
	// them. Partitions are indexed as the block is ordered.
	buckets [][keys.EVMKeyKindCount][]classifiedChange
}

// changes returns every change of the given kind, in the order the block presented them.
func (c *classifiedChanges) changes(kind keys.EVMKeyKind) iter.Seq[classifiedChange] {
	return func(yield func(classifiedChange) bool) {
		for i := range c.buckets {
			for _, change := range c.buckets[i][kind] {
				if !yield(change) {
					return
				}
			}
		}
	}
}

// count returns how many changes of the given kind there are, for sizing a map that will hold them.
func (c *classifiedChanges) count(kind keys.EVMKeyKind) int {
	total := 0
	for i := range c.buckets {
		total += len(c.buckets[i][kind])
	}
	return total
}

// bucketSizes returns the number of pairs in each kind's bucket, for sizing a later block's.
func (c *classifiedChanges) bucketSizes() [keys.EVMKeyKindCount]int {
	var sizes [keys.EVMKeyKindCount]int
	for kind := range sizes {
		sizes[kind] = c.count(keys.EVMKeyKind(kind))
	}
	return sizes
}

// classifyPartitions is the number of contiguous runs a block is split into for classification.
//
// Classification is dominated by walking pairs the producing thread has just written, so the work is
// waiting on memory rather than computing; splitting it lets several cores carry misses at once. The
// useful width is therefore bounded by how many outstanding misses a core sustains rather than by
// core count, which is why this is a modest number and not the size of the pool.
const classifyPartitions = 8

// classifyPartition names a contiguous run of a block's pairs: where the run starts, and how many
// pairs it covers.
//
// A run may cross changesets. Keeping runs contiguous rather than striding is what lets the buckets
// stay unmerged: partition order is block order, so a duplicated key still resolves to its last
// write. Allowing them to cross means a block splits into the same number of partitions however its
// pairs are distributed, which the ModuleRouter case above makes worth having.
type classifyPartition struct {
	changeSet int
	pair      int
	count     int
}

// planClassifyPartitions divides the block's pairs into at most parts contiguous runs.
//
// Empty and nil changesets are skipped here and skipped identically while walking, so a run's count
// always names the same pairs the planner counted.
func planClassifyPartitions(changeSets []*proto.NamedChangeSet, parts int) ([]classifyPartition, int) {
	total := 0
	for _, cs := range changeSets {
		if cs == nil {
			continue
		}
		total += len(cs.Changeset.Pairs)
	}
	if total == 0 {
		return nil, 0
	}

	per := (total + parts - 1) / parts
	plan := make([]classifyPartition, 0, parts)

	changeSet, pair, assigned := 0, 0, 0
	for assigned < total {
		count := min(per, total-assigned)

		// Advance past changesets already consumed, so the run starts at a real pair.
		for changeSet < len(changeSets) {
			cs := changeSets[changeSet]
			if cs != nil && pair < len(cs.Changeset.Pairs) {
				break
			}
			changeSet++
			pair = 0
		}

		plan = append(plan, classifyPartition{changeSet: changeSet, pair: pair, count: count})
		assigned += count

		// Walk the cursor forward by count pairs, which may cross changesets.
		remaining := count
		for remaining > 0 {
			cs := changeSets[changeSet]
			if cs == nil || pair >= len(cs.Changeset.Pairs) {
				changeSet++
				pair = 0
				continue
			}
			step := min(remaining, len(cs.Changeset.Pairs)-pair)
			pair += step
			remaining -= step
		}
	}
	return plan, total
}

// classifyAndPrefix splits changeSets into per-EVMKeyKind buckets whose keys are already in
// physical format ("module/" + prefix_encoded_key). Non-EVM modules are merged into the
// EVMKeyMisc bucket with a "<module>/" prefix.
//
// sizeHints gives each bucket's length in the previous block. Buckets are allocated at twice that
// share of a partition, since a block that grows a little then still lands in a single allocation
// rather than a resize and copy; a bucket with no hint grows on demand.
//
// The block is split into contiguous partitions classified in parallel on pool, because this walks
// every pair of a changeset the producing thread has just written and so spends most of its time
// waiting on memory rather than computing. A nil pool, or a block small enough that dispatch would
// cost more than the work, is classified on the calling goroutine.
func classifyAndPrefix(
	changeSets []*proto.NamedChangeSet,
	sizeHints [keys.EVMKeyKindCount]int,
	pool threading.Pool,
) (classifiedChanges, error) {
	plan, total := planClassifyPartitions(changeSets, classifyPartitions)
	if total == 0 {
		return classifiedChanges{}, nil
	}
	if pool == nil {
		// Callers without a pool classify on their own goroutine, as one partition.
		plan = []classifyPartition{{changeSet: 0, pair: 0, count: total}}
	}

	result := classifiedChanges{buckets: make([][keys.EVMKeyKindCount][]classifiedChange, len(plan))}
	errs := make([]error, len(plan))

	if len(plan) == 1 {
		result.buckets[0], errs[0] = classifyPartitionBuckets(changeSets, plan[0], sizeHints, 1)
		return result, errs[0]
	}

	var wg sync.WaitGroup
	for i, part := range plan {
		wg.Add(1)
		pool.Submit(func() {
			defer wg.Done()
			result.buckets[i], errs[i] = classifyPartitionBuckets(changeSets, part, sizeHints, len(plan))
		})
	}
	wg.Wait()

	if err := errors.Join(errs...); err != nil {
		return classifiedChanges{}, err
	}
	return result, nil
}

// classifyPartitionBuckets classifies one partition's pairs into its own bucket set.
//
// parts is how many partitions the block was split into, used only to size the buckets: each holds
// roughly its share of what the same bucket held last block.
func classifyPartitionBuckets(
	changeSets []*proto.NamedChangeSet,
	part classifyPartition,
	sizeHints [keys.EVMKeyKindCount]int,
	parts int,
) ([keys.EVMKeyKindCount][]classifiedChange, error) {
	var buckets [keys.EVMKeyKindCount][]classifiedChange
	for kind, hint := range sizeHints {
		if hint > 0 {
			buckets[kind] = make([]classifiedChange, 0, 2*hint/parts+1)
		}
	}

	// One buffer for the whole partition, held on this goroutine's stack so partitions share nothing.
	// The string conversion copies each physical key out of it, so it can be rewound and reused for
	// every pair, leaving one allocation per key rather than one for the key bytes and a second for
	// the string.
	var scratchArray [ktype.MaxEVMPhysicalKeyLen]byte
	scratch := scratchArray[:0]

	changeSet, pair, remaining := part.changeSet, part.pair, part.count
	for remaining > 0 {
		cs := changeSets[changeSet]
		if cs == nil || pair >= len(cs.Changeset.Pairs) {
			// Skipped exactly as the planner skipped it, so remaining still names real pairs.
			changeSet++
			pair = 0
			continue
		}

		// The run may end inside this changeset, or carry on into the next one.
		end := min(len(cs.Changeset.Pairs), pair+remaining)
		pairs := cs.Changeset.Pairs[pair:end]

		if cs.Name == keys.EVMStoreKey {
			for _, p := range pairs {
				kind, keyBytes := keys.ParseEVMKey(p.Key)
				if kind == keys.EVMKeyEmpty {
					return buckets, fmt.Errorf("flatkv: empty key in changeset")
				}

				if kind == keys.EVMKeyMisc {
					scratch = ktype.AppendModulePhysicalKey(scratch[:0], keys.EVMStoreKey, p.Key)
				} else {
					scratch = ktype.AppendEVMPhysicalKey(scratch[:0], kind, keyBytes)
				}
				buckets[kind] = append(buckets[kind], newClassifiedChange(string(scratch), p))
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
				return buckets, fmt.Errorf("flatkv: empty module name in changeset")
			}
			miscBucket := &buckets[keys.EVMKeyMisc]
			for _, p := range pairs {
				scratch = ktype.AppendModulePhysicalKey(scratch[:0], cs.Name, p.Key)
				*miscBucket = append(*miscBucket, newClassifiedChange(string(scratch), p))
			}
		}

		remaining -= len(pairs)
		changeSet++
		pair = 0
	}

	return buckets, nil
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
	rawChanges iter.Seq[classifiedChange],
	count int,
	blockHeight int64,
) (map[string]*vtype.StorageData, error) {
	result := make(map[string]*vtype.StorageData, count)

	for change := range rawChanges {
		if change.value == nil {
			// Deletion is equivalent to setting the storage value to a zero value
			result[change.key] = vtype.NewStorageData().SetBlockHeight(blockHeight)
			continue
		}
		storageData, err := vtype.NewStorageDataFrom(blockHeight, change.value)
		if err != nil {
			return nil, fmt.Errorf("failed to parse storage value: %w", err)
		}
		result[change.key] = storageData
	}

	return result, nil
}

// toCodeValues turns raw code changes into CodeData stamped with blockHeight. A nil change is a
// deletion, which for code means empty bytecode. Both maps are keyed by physical key.
func toCodeValues(
	rawChanges iter.Seq[classifiedChange],
	count int,
	blockHeight int64,
) (map[string]*vtype.CodeData, error) {
	result := make(map[string]*vtype.CodeData, count)

	for change := range rawChanges {
		// A nil change is a deletion, which for code means empty bytecode.
		result[change.key] = vtype.NewCodeDataFrom(blockHeight, change.value)
	}
	return result, nil
}

// toMiscValues turns raw misc changes into MiscData stamped with blockHeight. A nil change is a
// deletion, which for misc means an empty value. Both maps are keyed by physical key.
func toMiscValues(
	rawChanges iter.Seq[classifiedChange],
	count int,
	blockHeight int64,
) (map[string]*vtype.MiscData, error) {
	result := make(map[string]*vtype.MiscData, count)

	for change := range rawChanges {
		if change.value == nil {
			result[change.key] = vtype.NewDeletedMiscData(blockHeight)
			continue
		}
		result[change.key] = vtype.NewMiscDataFrom(blockHeight, change.value)
	}
	return result, nil
}

// Merge account updates down into a single update per account.
func mergeAccountUpdates(
	changes *classifiedChanges,
) (map[string]*vtype.PendingAccountWrite, error) {

	updates := make(map[string]*vtype.PendingAccountWrite,
		changes.count(keys.EVMKeyNonce)+changes.count(keys.EVMKeyCodeHash))

	for change := range changes.changes(keys.EVMKeyNonce) {
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

	for change := range changes.changes(keys.EVMKeyCodeHash) {
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

	// Balances are not folded in: there is no balance key kind yet, so no change can carry one. When
	// one is introduced, mirror the codehash loop above.
	return updates, nil
}

// mergeAccountValues folds a block's per-field account changes onto the accounts they modify,
// leaving accounts holding the new value of every account the block touches.
//
// accounts must hold an entry for every key the changes name, which is what readAccountsToMerge
// produces from the same classified changes. The accounts are modified in place, so a change is
// applied on top of the value read from the store and an account named by several changes carries
// all of them. Every account keeps the block height it was stamped with, even when no field value
// actually changed.
func mergeAccountValues(
	accounts map[string]*vtype.AccountData,
	changes *classifiedChanges,
) error {
	for change := range changes.changes(keys.EVMKeyNonce) {
		account, err := accountFor(accounts, change.key)
		if err != nil {
			return err
		}
		if change.value == nil {
			// Deletion is equivalent to setting the nonce to 0
			account.SetNonce(0)
			continue
		}
		nonce, err := vtype.ParseNonce(change.value)
		if err != nil {
			return fmt.Errorf("invalid nonce value: %w", err)
		}
		account.SetNonce(nonce)
	}

	for change := range changes.changes(keys.EVMKeyCodeHash) {
		account, err := accountFor(accounts, change.key)
		if err != nil {
			return err
		}
		if change.value == nil {
			// Deletion is equivalent to setting the code hash to a zero hash
			var zero vtype.CodeHash
			account.SetCodeHash(&zero)
			continue
		}
		if _, err := account.SetCodeHashBytes(change.value); err != nil {
			return fmt.Errorf("invalid codehash value: %w", err)
		}
	}

	// Balances are not folded in: there is no balance key kind yet, so no change can carry one. When
	// one is introduced, mirror the codehash loop above.
	return nil
}

// accountFor returns the account a change applies to. The account setters build a fresh account when
// called on a nil one, so a key with no entry would take its change to a value nobody holds; this
// reports that as the error it is.
func accountFor(accounts map[string]*vtype.AccountData, key string) (*vtype.AccountData, error) {
	account := accounts[key]
	if account == nil {
		return nil, fmt.Errorf("no account was read for key %x", key)
	}
	return account, nil
}

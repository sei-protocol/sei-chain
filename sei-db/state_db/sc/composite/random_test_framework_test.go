package composite

// This file provides a self-contained, oracle-based randomized test harness
// for CompositeCommitStore. It mirrors the approach used by the migration
// package's migration_test_framework_test.go (an in-memory reference model
// plus a deterministic random workload), but drives the full composite store
// API (ApplyChangeSets / Commit / Get / Has / Iterator / GetProof /
// LoadVersion / Rollback / Exporter / Importer) instead of the bare Router
// interface, and deep-inspects the unexported memIAVL / flatKV backends.
//
// The primitives here are intentionally duplicated rather than shared with the
// migration package: those live in _test.go files in package migration and are
// not importable. Keeping a local copy avoids any test-only cross-package
// coupling.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/testutil"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/migration"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/stretchr/testify/require"
)

// randomTestStores is the set of module stores the randomized workload writes
// to. It deliberately spans the three routing classes the production routers
// distinguish: bank/ (stays on memiavl the longest), a generic non-bank /
// non-evm module (staking/, migrates in the MigrateAllButBank step), and evm/
// (migrates first, uses structurally-valid EVM keys).
var randomTestStores = []string{keys.BankStoreKey, keys.StakingStoreKey, keys.EVMStoreKey}

// =============================================================================
// Oracle: a deep-copyable in-memory reference model.
// =============================================================================

// storeOracle is the reference implementation the composite store is checked
// against. It is a thin wrapper around map[store]map[key]value. Snapshot
// returns a deep copy, which the rollback / crash-reconcile scenarios capture
// at the height they later expect the store to return to.
type storeOracle struct {
	stores map[string]map[string][]byte
}

func newStoreOracle() *storeOracle {
	return &storeOracle{stores: map[string]map[string][]byte{}}
}

// apply mutates the oracle exactly as the composite store's ApplyChangeSets
// would mutate committed state: sets become writes, deletes remove the key.
func (o *storeOracle) apply(changesets []*proto.NamedChangeSet) {
	for _, ncs := range changesets {
		if ncs == nil {
			continue
		}
		m, ok := o.stores[ncs.Name]
		if !ok {
			m = map[string][]byte{}
			o.stores[ncs.Name] = m
		}
		for _, pair := range ncs.Changeset.Pairs {
			if pair == nil {
				continue
			}
			if pair.Delete {
				delete(m, string(pair.Key))
				continue
			}
			m[string(pair.Key)] = append([]byte(nil), pair.Value...)
		}
	}
}

// get returns the oracle's expected value for (store, key).
func (o *storeOracle) get(store string, key []byte) ([]byte, bool) {
	m, ok := o.stores[store]
	if !ok {
		return nil, false
	}
	v, ok := m[string(key)]
	return v, ok
}

// snapshot returns a deep copy of the oracle. Tests call this immediately
// after committing the height they intend to roll back / reconcile to.
func (o *storeOracle) snapshot() *storeOracle {
	out := newStoreOracle()
	for store, m := range o.stores {
		cp := make(map[string][]byte, len(m))
		for k, v := range m {
			cp[k] = append([]byte(nil), v...)
		}
		out.stores[store] = cp
	}
	return out
}

// =============================================================================
// liveKeySet: O(1) add/remove with deterministic random sampling.
// =============================================================================

type keyPair struct {
	store string
	key   string
}

// liveKeySet tracks currently-live (store, key) pairs and supports O(1) Add,
// O(1) Remove, and O(n) deterministic random sampling via Floyd's algorithm
// (no reliance on Go's randomized map-iteration order). Ported from the
// migration package's liveKeySet.
type liveKeySet struct {
	keys []keyPair
	idx  map[keyPair]int
}

func newLiveKeySet() *liveKeySet {
	return &liveKeySet{idx: make(map[keyPair]int)}
}

func (s *liveKeySet) Len() int { return len(s.keys) }

func (s *liveKeySet) Contains(kp keyPair) bool {
	_, ok := s.idx[kp]
	return ok
}

func (s *liveKeySet) Add(kp keyPair) {
	if _, ok := s.idx[kp]; ok {
		return
	}
	s.idx[kp] = len(s.keys)
	s.keys = append(s.keys, kp)
}

func (s *liveKeySet) Remove(kp keyPair) {
	i, ok := s.idx[kp]
	if !ok {
		return
	}
	last := len(s.keys) - 1
	if i != last {
		s.keys[i] = s.keys[last]
		s.idx[s.keys[i]] = i
	}
	s.keys = s.keys[:last]
	delete(s.idx, kp)
}

// Sample returns up to n distinct keyPairs uniformly at random using Floyd's
// algorithm. Output depends only on s.keys and the calls made to r, so it is
// fully reproducible from r's seed.
func (s *liveKeySet) Sample(r *testutil.TestRandom, n int) []keyPair {
	population := len(s.keys)
	if n > population {
		n = population
	}
	if n == 0 {
		return nil
	}
	chosen := make(map[int]struct{}, n)
	out := make([]keyPair, 0, n)
	for i := population - n; i < population; i++ {
		j := r.Intn(i + 1)
		if _, exists := chosen[j]; exists {
			chosen[i] = struct{}{}
			out = append(out, s.keys[i])
		} else {
			chosen[j] = struct{}{}
			out = append(out, s.keys[j])
		}
	}
	return out
}

// liveKeySetFromOracle rebuilds a liveKeySet that exactly covers the keys in
// the given oracle. Used after a rollback / reconcile to resume the workload
// from a restored state, since the in-flight liveKeySet no longer matches the
// rolled-back keyspace.
func liveKeySetFromOracle(o *storeOracle) *liveKeySet {
	var pairs []keyPair
	for store, m := range o.stores {
		for k := range m {
			pairs = append(pairs, keyPair{store: store, key: k})
		}
	}
	// Sort so s.keys ordering is independent of Go's randomized map-iteration
	// order; otherwise Sample results would not reproduce from the seed.
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].store != pairs[j].store {
			return pairs[i].store < pairs[j].store
		}
		return pairs[i].key < pairs[j].key
	})
	s := newLiveKeySet()
	for _, kp := range pairs {
		s.Add(kp)
	}
	return s
}

// =============================================================================
// Random key/value generators.
// =============================================================================

func randomTestBytes(rng *testutil.TestRandom, n int) []byte {
	return rng.Bytes(n)
}

// ensureNonZero guarantees b is not all-zero, setting its final byte when every
// byte is zero. FlatKV treats an all-zero storage value, a zero nonce, and a
// zero code hash as "absent" (its value-encoded deletion convention), so a
// workload that intends a live write must avoid generating those by accident;
// otherwise the simple map oracle (which keeps the key) would diverge from
// flatkv (which drops it).
func ensureNonZero(b []byte) []byte {
	for _, x := range b {
		if x != 0 {
			return b
		}
	}
	if len(b) > 0 {
		b[len(b)-1] = 1
	}
	return b
}

// Live (non-deletion) EVM value generators, each producing the exact length
// flatkv's vtype parsers require so a write never collapses into a delete.
func randomNonceValue(rng *testutil.TestRandom) []byte {
	return ensureNonZero(randomTestBytes(rng, vtype.NonceLen))
}

func randomCodeHashValue(rng *testutil.TestRandom) []byte {
	return ensureNonZero(randomTestBytes(rng, vtype.CodeHashLen))
}

func randomStorageValue(rng *testutil.TestRandom) []byte {
	return ensureNonZero(randomTestBytes(rng, vtype.SlotLen))
}

// randomCodeValue returns a non-empty bytecode blob (empty bytecode is flatkv's
// code-deletion sentinel).
func randomCodeValue(rng *testutil.TestRandom) []byte {
	return randomTestBytes(rng, 1+rng.Intn(48))
}

// randomLegacyValue returns a variable-length value for cosmos / EVM-legacy
// keys. Roughly 10% of the time it returns an explicit empty value to exercise
// flatkv's nonNilValue empty-vs-nil WAL round-trip (and memiavl's empty-value
// leaf): such keys are present but hold a zero-length value, and the restart /
// state-sync / migration phases verify they survive replay. Empty values are
// only ever generated for legacy-style keys -- a fixed-length nonce / code
// hash / storage slot would parse-fail or read back as a delete, and empty
// bytecode is the code-deletion sentinel.
func randomLegacyValue(rng *testutil.TestRandom) []byte {
	if rng.Intn(10) == 0 {
		return []byte{}
	}
	return randomTestBytes(rng, 1+rng.Intn(16))
}

// randomLegacyEVMKey builds an EVM key that ParseEVMKey routes to the legacy
// lane (address mappings, codesize, etc.). 0x01 is never an optimized prefix
// (those are 0x03/0x07/0x08/0x0a), so the key is always classified legacy
// regardless of length.
func randomLegacyEVMKey(rng *testutil.TestRandom) []byte {
	return append([]byte{0x01}, randomTestBytes(rng, 1+rng.Intn(24))...)
}

// newRandomEVMEntry returns the changeset pairs for a single fresh EVM entry,
// chosen uniformly among the physical maps flatkv maintains so that, over a
// run, every map receives data:
//
//   - storage: one storageDB row  (0x03 || addr || slot)
//   - code:    one codeDB row      (0x07 || addr)
//   - account: one accountDB row   (0x0a || addr) — a nonce is always written
//     (a zero nonce reads back as absent), and with ~50% probability a code
//     hash is written for the SAME address. That second case is the account-map
//     "collision": the nonce and code hash merge into one physical account row,
//     exercising the merged-account read / iterate / migrate paths.
//   - legacy:  one legacyDB row     (0x01 || suffix) — a non-optimized EVM key
//     (address mappings, codesize, etc.) with a variable-length, occasionally
//     empty value, populating flatkv's EVM legacy lane.
//
// Returning a slice (rather than one pair) is what lets a single logical
// account own both a nonce and a code hash within one block.
func newRandomEVMEntry(rng *testutil.TestRandom) []*proto.KVPair {
	switch rng.Intn(4) {
	case 0:
		addr := randomTestBytes(rng, keys.AddressLen)
		pairs := []*proto.KVPair{
			{Key: keys.BuildEVMKey(keys.EVMKeyNonce, addr), Value: randomNonceValue(rng)},
		}
		if rng.Intn(2) == 0 {
			pairs = append(pairs, &proto.KVPair{
				Key:   keys.BuildEVMKey(keys.EVMKeyCodeHash, addr),
				Value: randomCodeHashValue(rng),
			})
		}
		return pairs
	case 1:
		addr := randomTestBytes(rng, keys.AddressLen)
		return []*proto.KVPair{{Key: keys.BuildEVMKey(keys.EVMKeyCode, addr), Value: randomCodeValue(rng)}}
	case 2:
		addr := randomTestBytes(rng, keys.AddressLen)
		stripped := append(addr, randomTestBytes(rng, vtype.SlotLen)...) // addr || slot
		return []*proto.KVPair{{Key: keys.BuildEVMKey(keys.EVMKeyStorage, stripped), Value: randomStorageValue(rng)}}
	default:
		return []*proto.KVPair{{Key: randomLegacyEVMKey(rng), Value: randomLegacyValue(rng)}}
	}
}

// freshEVMValue returns a new live value of the correct kind/length for an
// existing EVM key, used when overwriting it.
func freshEVMValue(rng *testutil.TestRandom, key []byte) []byte {
	kind, _ := keys.ParseEVMKey(key)
	switch kind {
	case keys.EVMKeyNonce:
		return randomNonceValue(rng)
	case keys.EVMKeyCodeHash:
		return randomCodeHashValue(rng)
	case keys.EVMKeyCode:
		return randomCodeValue(rng)
	case keys.EVMKeyStorage:
		return randomStorageValue(rng)
	default: // EVMKeyMisc
		return randomLegacyValue(rng)
	}
}

// valuesEqual compares two stored values treating a nil and a zero-length
// slice as equal. testify's require.Equal does NOT: it special-cases []byte so
// that []byte(nil) and []byte{} compare unequal. Backends (and the map oracle)
// freely return either representation for an empty value, so all value
// comparisons in this harness go through valuesEqual instead.
func valuesEqual(a, b []byte) bool {
	return bytes.Equal(a, b)
}

// =============================================================================
// Block simulator.
// =============================================================================

// simParams bundles the per-block operation counts for simulateBlocks.
type simParams struct {
	readsPerBlock   int
	updatesPerBlock int
	deletesPerBlock int
	newKeysPerBlock int
	// conflictsPerBlock is the number of intra-block conflict sequences to
	// emit per block (see Phase D in simulateBlocks). Zero disables them.
	conflictsPerBlock int
	blocks            int
}

// conflictOps returns an ordered op sequence for a single key, all within one
// block / changeset, plus whether the key is finally live. It exercises the
// intra-block last-write-wins, delete-after-set, and set-after-delete paths
// that a single Get/iteration at block end must resolve correctly. A leading
// delete on a not-yet-present key is a no-op for both the oracle and the store.
func conflictOps(rng *testutil.TestRandom, key []byte, valueFn func(*testutil.TestRandom) []byte) (pairs []*proto.KVPair, finallyLive bool) {
	switch rng.Intn(4) {
	case 0: // set, set -> live (second value wins)
		return []*proto.KVPair{
			{Key: key, Value: valueFn(rng)},
			{Key: key, Value: valueFn(rng)},
		}, true
	case 1: // set, delete -> absent
		return []*proto.KVPair{
			{Key: key, Value: valueFn(rng)},
			{Key: key, Delete: true},
		}, false
	case 2: // delete, set -> live
		return []*proto.KVPair{
			{Key: key, Delete: true},
			{Key: key, Value: valueFn(rng)},
		}, true
	default: // set, delete, set -> live
		return []*proto.KVPair{
			{Key: key, Value: valueFn(rng)},
			{Key: key, Delete: true},
			{Key: key, Value: valueFn(rng)},
		}, true
	}
}

// conflictValueFn returns the value generator matching a store/key kind for
// intra-block conflict sequences (EVM storage slots vs cosmos/legacy values).
func conflictValueFn(store string) func(*testutil.TestRandom) []byte {
	if store == keys.EVMStoreKey {
		return randomStorageValue
	}
	return randomLegacyValue
}

// sampleUntouchedSafeKey returns a live key safe for an intra-block conflict
// sequence -- a cosmos key or an EVM storage key (standalone physical rows, no
// account-merge nonce/codehash sibling rule) that has not already been touched
// this block. Sampling is deterministic from rng.
func sampleUntouchedSafeKey(rng *testutil.TestRandom, keysInUse *liveKeySet, touched map[keyPair]struct{}) (keyPair, bool) {
	for _, kp := range keysInUse.Sample(rng, 8) {
		if _, ok := touched[kp]; ok {
			continue
		}
		if kp.store == keys.EVMStoreKey {
			if kind, _ := keys.ParseEVMKey([]byte(kp.key)); kind != keys.EVMKeyStorage {
				continue
			}
		}
		return kp, true
	}
	return keyPair{}, false
}

// pickConflictTarget chooses a key for an intra-block conflict sequence: ~50% an
// existing safe, untouched live key (exercising delete-then-set / double-update
// on committed state), otherwise a brand-new cosmos or EVM-storage key
// (exercising create-then-delete / create-then-overwrite within one block).
func pickConflictTarget(rng *testutil.TestRandom, keysInUse *liveKeySet, touched map[keyPair]struct{}, stores []string) (string, []byte, func(*testutil.TestRandom) []byte) {
	if rng.Intn(2) == 0 {
		if kp, ok := sampleUntouchedSafeKey(rng, keysInUse, touched); ok {
			return kp.store, []byte(kp.key), conflictValueFn(kp.store)
		}
	}
	store := stores[rng.Intn(len(stores))]
	if store == keys.EVMStoreKey {
		key := keys.BuildEVMKey(keys.EVMKeyStorage,
			append(randomTestBytes(rng, keys.AddressLen), randomTestBytes(rng, vtype.SlotLen)...))
		return store, key, randomStorageValue
	}
	return store, randomTestBytes(rng, 8), randomLegacyValue
}

// simulateBlocks drives a deterministic mixed insert / update / delete / read
// workload against the composite store, mutating oracle in lockstep. After
// each block it commits, asserts the version advanced by one, checks that
// just-deleted keys read back as absent, and samples live keys through both
// the direct Get/Has path and the GetChildStoreByName (RouterCommitKVStore)
// adapter, comparing every read against the oracle.
//
// All randomness comes from rng, so a given seed yields a byte-identical
// apply/commit sequence.
func simulateBlocks(
	t *testing.T,
	cs *CompositeCommitStore,
	oracle *storeOracle,
	rng *testutil.TestRandom,
	keysInUse *liveKeySet,
	stores []string,
	p simParams,
) {
	t.Helper()

	for range p.blocks {
		startVersion := cs.Version()
		allPairs := make(map[string][]*proto.KVPair)

		// Insert brand-new keys distributed across the allowed stores. EVM
		// entries may expand to two pairs (a nonce + a code hash for one
		// address) so the account map gets merge "collisions".
		for range p.newKeysPerBlock {
			store := stores[rng.Intn(len(stores))]
			if store == keys.EVMStoreKey {
				for _, pair := range newRandomEVMEntry(rng) {
					allPairs[store] = append(allPairs[store], pair)
					keysInUse.Add(keyPair{store: store, key: string(pair.Key)})
				}
				continue
			}
			pair := &proto.KVPair{Key: randomTestBytes(rng, 8), Value: randomLegacyValue(rng)}
			allPairs[store] = append(allPairs[store], pair)
			keysInUse.Add(keyPair{store: store, key: string(pair.Key)})
		}

		// Overwrite existing keys with fresh values.
		for _, kp := range keysInUse.Sample(rng, p.updatesPerBlock) {
			var value []byte
			if kp.store == keys.EVMStoreKey {
				value = freshEVMValue(rng, []byte(kp.key))
			} else {
				value = randomLegacyValue(rng)
			}
			allPairs[kp.store] = append(allPairs[kp.store],
				&proto.KVPair{Key: []byte(kp.key), Value: value})
		}

		// Delete existing keys. Deleting an account's nonce must also delete
		// that address's code hash in the same block: flatkv merges both into a
		// single physical account row, so dropping only the nonce would leave a
		// live (now nonce-zero) row whose nonce reads back via the phantom-nonce
		// path — present in flatkv but absent from the oracle. Expanding the
		// delete set deterministically (in sample order, deduped) keeps the
		// generated changeset byte-identical for a given seed.
		toDelete := make([]keyPair, 0, p.deletesPerBlock)
		inDelete := make(map[keyPair]struct{}, p.deletesPerBlock)
		addDelete := func(kp keyPair) {
			if _, ok := inDelete[kp]; ok {
				return
			}
			inDelete[kp] = struct{}{}
			toDelete = append(toDelete, kp)
		}
		for _, kp := range keysInUse.Sample(rng, p.deletesPerBlock) {
			addDelete(kp)
			if kp.store != keys.EVMStoreKey {
				continue
			}
			if kind, stripped := keys.ParseEVMKey([]byte(kp.key)); kind == keys.EVMKeyNonce {
				sibling := keyPair{store: kp.store, key: string(keys.BuildEVMKey(keys.EVMKeyCodeHash, stripped))}
				if keysInUse.Contains(sibling) {
					addDelete(sibling)
				}
			}
		}
		for _, kp := range toDelete {
			allPairs[kp.store] = append(allPairs[kp.store], &proto.KVPair{Key: []byte(kp.key), Delete: true})
		}

		// Phase D: deliberate intra-block conflicts. Each conflict targets a key
		// disjoint from the inserts/updates/deletes above, then appends a 2-3 op
		// sequence to the SAME changeset (set/set, set/delete, delete/set,
		// set/delete/set). Because the key is untouched by the other phases, its
		// committed state is fully determined by its own sequence, so the oracle
		// (which applies the changeset in order) stays the source of truth and
		// keysInUse bookkeeping needs only the final-state flag. This exercises
		// last-write-wins and create/delete collapse within one block, which
		// full-range and bounded reads at block end must resolve correctly.
		touchedThisBlock := make(map[keyPair]struct{})
		for store, pairs := range allPairs {
			for _, pr := range pairs {
				touchedThisBlock[keyPair{store: store, key: string(pr.Key)}] = struct{}{}
			}
		}
		var conflictLive []keyPair
		for range p.conflictsPerBlock {
			store, key, valueFn := pickConflictTarget(rng, keysInUse, touchedThisBlock, stores)
			kp := keyPair{store: store, key: string(key)}
			if _, dup := touchedThisBlock[kp]; dup {
				continue // already chosen / rare fresh-key collision: skip to stay disjoint
			}
			touchedThisBlock[kp] = struct{}{}
			pairs, finallyLive := conflictOps(rng, key, valueFn)
			allPairs[store] = append(allPairs[store], pairs...)
			if finallyLive {
				conflictLive = append(conflictLive, kp)
			} else {
				addDelete(kp) // bookkeeping only; the delete pair is already in the sequence
			}
		}

		// Build the changeset slice in deterministic store-name order.
		storeNames := make([]string, 0, len(allPairs))
		for store := range allPairs {
			storeNames = append(storeNames, store)
		}
		sort.Strings(storeNames)
		cset := make([]*proto.NamedChangeSet, 0, len(allPairs))
		for _, store := range storeNames {
			cset = append(cset,
				&proto.NamedChangeSet{Name: store, Changeset: proto.ChangeSet{Pairs: allPairs[store]}})
		}

		require.NoError(t, cs.ApplyChangeSets(cset), "ApplyChangeSets")
		oracle.apply(cset)
		version, err := cs.Commit()
		require.NoError(t, err, "Commit")
		require.Equal(t, startVersion+1, version, "Commit must advance the version by exactly one")

		for _, kp := range toDelete {
			keysInUse.Remove(kp)
		}
		// Conflict keys whose sequence ends live become (or stay) sampleable.
		// Disjoint from toDelete, so Remove-then-Add ordering is unambiguous.
		for _, kp := range conflictLive {
			keysInUse.Add(kp)
		}

		// Negative check: a key deleted this block (and not re-added) must
		// read back as absent.
		for _, kp := range toDelete {
			if _, stillLive := oracle.get(kp.store, []byte(kp.key)); stillLive {
				continue
			}
			_, ok, err := cs.Get(kp.store, []byte(kp.key))
			require.NoError(t, err)
			require.False(t, ok, "deleted key still present: store=%q key=%x", kp.store, kp.key)
		}

		// Positive read sampling: exercise both read paths against the oracle.
		for _, kp := range keysInUse.Sample(rng, p.readsPerBlock) {
			key := []byte(kp.key)
			want, _ := oracle.get(kp.store, key)

			got, ok, err := cs.Get(kp.store, key)
			require.NoError(t, err, "Get store=%q key=%x", kp.store, key)
			require.True(t, ok, "expected present store=%q key=%x", kp.store, key)
			require.True(t,
				valuesEqual(want, got), "Get value mismatch store=%q key=%x: want %x got %x", kp.store, key, want, got)

			has, err := cs.Has(kp.store, key)
			require.NoError(t, err)
			require.True(t, has, "Has must agree with Get store=%q key=%x", kp.store, key)

			child := cs.GetChildStoreByName(kp.store)
			require.True(t, valuesEqual(want, child.Get(key)),
				"GetChildStoreByName read mismatch store=%q key=%x", kp.store, key)
		}
	}
}

// =============================================================================
// Backend inspection helpers.
// =============================================================================

// memiavlGetForTest reads (store, key) directly from the memiavl backend, or
// reports not-found when memiavl is absent (e.g. FlatKVOnly mode).
func memiavlGetForTest(cs *CompositeCommitStore, store string, key []byte) ([]byte, bool) {
	if cs.memIAVL == nil {
		return nil, false
	}
	child := cs.memIAVL.GetChildStoreByName(store)
	if child == nil {
		return nil, false
	}
	v := child.Get(key)
	return v, v != nil
}

// flatKVGetForTest reads (store, key) directly from the flatkv backend, or
// reports not-found when flatkv is absent (e.g. MemiavlOnly mode).
func flatKVGetForTest(cs *CompositeCommitStore, store string, key []byte) ([]byte, bool) {
	if cs.flatKV == nil {
		return nil, false
	}
	return cs.flatKV.Get(store, key)
}

// getMemIAVLKeyCount returns the total number of keys across every tree in the
// memiavl backend. Ported from the migration framework's GetMemIAVLKeyCount.
func getMemIAVLKeyCount(t *testing.T, cs *CompositeCommitStore) int64 {
	t.Helper()
	require.NotNil(t, cs.memIAVL)
	var total int64
	for _, namedTree := range cs.memIAVL.GetDB().Trees() {
		iter := namedTree.Iterator(nil, nil, true)
		for ; iter.Valid(); iter.Next() {
			total++
		}
		require.NoError(t, iter.Error(), "iterator error on tree %q", namedTree.Name)
		_ = iter.Close()
	}
	return total
}

// getFlatKVKeyCount returns the raw physical key count across all flatkv data
// DBs. Ported from the migration framework's GetFlatKVKeyCount; the same
// physical-vs-logical caveat applies (random 20-byte EVM addresses make
// account-row merge collisions astronomically unlikely, so the physical count
// equals the logical key count).
func getFlatKVKeyCount(t *testing.T, cs *CompositeCommitStore) int64 {
	t.Helper()
	require.NotNil(t, cs.flatKV)
	iter, err := cs.flatKV.RawGlobalIterator()
	require.NoError(t, err)
	defer func() { _ = iter.Close() }()
	var count int64
	for ; iter.Valid(); iter.Next() {
		count++
	}
	require.NoError(t, iter.Error())
	return count
}

// =============================================================================
// Placement model: where each store's keys are expected to live.
// =============================================================================

type backendPlacement int

const (
	inMemiavlOnly backendPlacement = iota
	inFlatKVOnly
	inBoth // dual-write: every key in memiavl, evm additionally mirrored to flatkv
)

// steadyStatePlacement returns the per-store backend expectation for a
// steady-state (non-migrating) write mode. Panics for migration modes, whose
// placement is in-flight and must be verified differently.
func steadyStatePlacement(mode types.WriteMode) func(store string) backendPlacement {
	switch mode {
	case types.MemiavlOnly:
		return func(string) backendPlacement { return inMemiavlOnly }
	case types.FlatKVOnly:
		return func(string) backendPlacement { return inFlatKVOnly }
	case types.EVMMigrated:
		return func(store string) backendPlacement {
			if store == keys.EVMStoreKey {
				return inFlatKVOnly
			}
			return inMemiavlOnly
		}
	case types.AllMigratedButBank:
		return func(store string) backendPlacement {
			if store == keys.BankStoreKey {
				return inMemiavlOnly
			}
			return inFlatKVOnly
		}
	case types.TestOnlyDualWrite:
		return func(store string) backendPlacement {
			if store == keys.EVMStoreKey {
				return inBoth
			}
			return inMemiavlOnly
		}
	default:
		panic("steadyStatePlacement: not a steady-state mode: " + string(mode))
	}
}

// =============================================================================
// Verification helpers (all driven from the oracle).
// =============================================================================

// verifyOracle asserts every (store, key) the oracle knows about reads back
// through composite.Get / Has with the expected value.
func verifyOracle(t *testing.T, cs *CompositeCommitStore, oracle *storeOracle) {
	t.Helper()
	for store, m := range oracle.stores {
		for k, want := range m {
			key := []byte(k)
			got, ok, err := cs.Get(store, key)
			require.NoError(t, err, "Get store=%q key=%x", store, key)
			require.True(t, ok, "missing store=%q key=%x", store, key)
			require.True(t,
				valuesEqual(want, got), "value mismatch store=%q key=%x: want %x got %x", store, key, want, got)
			has, err := cs.Has(store, key)
			require.NoError(t, err)
			require.True(t, has, "Has must agree with Get store=%q key=%x", store, key)
		}
	}
}

// verifyIteration asserts that, for each store, a full-range composite.Iterator
// (ascending and descending) yields exactly the oracle's key/value set in the
// correct order. Composite iteration stitches the memiavl and flatkv backends
// directly (bypassing the router), so this exercises cross-backend merging
// that the router-level tests cannot reach.
func verifyIteration(t *testing.T, cs *CompositeCommitStore, oracle *storeOracle, stores []string) {
	t.Helper()
	for _, store := range stores {
		m := oracle.stores[store]
		want := make([]iterKV, 0, len(m))
		for k, v := range m {
			want = append(want, iterKV{k, string(v)})
		}
		sort.Slice(want, func(i, j int) bool { return want[i].k < want[j].k })

		// Ascending must equal the sorted oracle entries.
		assertIterationEquals(t, want, collectIterator(t, cs, store, true), store, "ascending")

		// Descending must equal the reverse of the ascending expectation.
		rev := make([]iterKV, len(want))
		for i := range want {
			rev[len(want)-1-i] = want[i]
		}
		assertIterationEquals(t, rev, collectIterator(t, cs, store, false), store, "descending")
	}
}

type iterKV struct{ k, v string }

// assertIterationEquals compares element-by-element so a nil and an empty
// slice (both representing "no rows") are treated as equal.
func assertIterationEquals(t *testing.T, want, got []iterKV, store, direction string) {
	t.Helper()
	require.Equal(t, len(want), len(got), "%s iteration count mismatch for store %q", direction, store)
	for i := range want {
		require.Equal(t, want[i], got[i], "%s iteration element %d mismatch for store %q", direction, i, store)
	}
}

func collectIterator(t *testing.T, cs *CompositeCommitStore, store string, ascending bool) []iterKV {
	return collectBoundedIterator(t, cs, store, nil, nil, ascending)
}

// byteRange is a single [start, end) iteration window. A nil bound is open.
type byteRange struct{ start, end []byte }

// verifyBoundedIteration exercises bounded [start, end) iteration (ascending
// and descending) for each store, comparing against the oracle filtered to the
// range. Full-range scans alone leave flatkv's per-lane bound clamping
// untested; this samples several deterministic window shapes from rng (full,
// half-open below, half-open above, a narrow window between two live keys, and
// a single-key window) plus, for the EVM store, ranges that straddle the
// physical type-prefix boundaries (storage 0x03 / code 0x07 / codehash 0x08 /
// nonce 0x0a / legacy 0x01) so the lane skipping and logical->physical bound
// translation in evmLaneBounds are exercised.
func verifyBoundedIteration(
	t *testing.T, cs *CompositeCommitStore, oracle *storeOracle, rng *testutil.TestRandom, stores []string) {
	t.Helper()
	for _, store := range stores {
		ranges := sampleRanges(rng, sortedOracleKeys(oracle, store))
		if store == keys.EVMStoreKey {
			ranges = append(ranges, evmCrossLaneRanges()...)
		}
		for _, rg := range ranges {
			assertRangeMatches(t, cs, oracle, store, rg.start, rg.end)
		}
	}
}

// sortedOracleKeys returns the oracle's keys for a store in ascending order.
func sortedOracleKeys(oracle *storeOracle, store string) [][]byte {
	m := oracle.stores[store]
	out := make([][]byte, 0, len(m))
	for k := range m {
		out = append(out, []byte(k))
	}
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i], out[j]) < 0 })
	return out
}

// sampleRanges derives a deterministic set of [start, end) windows from rng,
// anchored on existing keys so the windows are meaningful (boundary-hitting)
// rather than almost-always-empty random byte ranges.
func sampleRanges(rng *testutil.TestRandom, sorted [][]byte) []byteRange {
	ranges := []byteRange{{nil, nil}} // full scan
	n := len(sorted)
	if n == 0 {
		return ranges
	}
	ranges = append(ranges, byteRange{nil, sorted[rng.Intn(n)]}) // [nil, end)
	ranges = append(ranges, byteRange{sorted[rng.Intn(n)], nil}) // [start, nil)
	if n >= 2 {
		i, j := rng.Intn(n), rng.Intn(n)
		if i > j {
			i, j = j, i
		}
		ranges = append(ranges, byteRange{sorted[i], sorted[j]}) // narrow window
		k := rng.Intn(n - 1)
		ranges = append(ranges, byteRange{sorted[k], sorted[k+1]}) // single-key window
	}
	return ranges
}

// evmCrossLaneRanges returns ranges whose endpoints fall in different EVM
// physical lanes, forcing flatkv to clamp and translate per-lane bounds.
func evmCrossLaneRanges() []byteRange {
	return []byteRange{
		{[]byte{0x03}, []byte{0x08}}, // storage start .. codehash end
		{[]byte{0x07}, nil},          // code start .. open
		{[]byte{0x01}, []byte{0x0a}}, // legacy start .. nonce end
		{keys.BuildEVMKey(keys.EVMKeyStorage, make([]byte, keys.AddressLen+vtype.SlotLen)), []byte{0x0b}},
	}
}

// assertRangeMatches compares ascending and descending bounded iteration for
// one [start, end) window against the oracle filtered to that window.
func assertRangeMatches(t *testing.T, cs *CompositeCommitStore, oracle *storeOracle, store string, start, end []byte) {
	t.Helper()
	assertIterationEquals(t, oracleRange(oracle, store, start, end, true),
		collectBoundedIterator(t, cs, store, start, end, true), store, fmt.Sprintf("ascending[%x,%x)", start, end))
	assertIterationEquals(t, oracleRange(oracle, store, start, end, false),
		collectBoundedIterator(t, cs, store, start, end, false), store, fmt.Sprintf("descending[%x,%x)", start, end))
}

// oracleRange returns the oracle's (k, v) entries within [start, end), sorted
// ascending or descending.
func oracleRange(oracle *storeOracle, store string, start, end []byte, ascending bool) []iterKV {
	m := oracle.stores[store]
	out := make([]iterKV, 0, len(m))
	for k, v := range m {
		kb := []byte(k)
		if start != nil && bytes.Compare(kb, start) < 0 {
			continue
		}
		if end != nil && bytes.Compare(kb, end) >= 0 {
			continue
		}
		out = append(out, iterKV{k, string(v)})
	}
	sort.Slice(out, func(i, j int) bool {
		if ascending {
			return out[i].k < out[j].k
		}
		return out[i].k > out[j].k
	})
	return out
}

func collectBoundedIterator(
	t *testing.T, cs *CompositeCommitStore, store string, start, end []byte, ascending bool) []iterKV {
	t.Helper()
	iter, err := cs.Iterator(store, start, end, ascending)
	require.NoError(t, err)
	require.NotNil(t, iter)
	defer func() { _ = iter.Close() }()
	var out []iterKV
	for ; iter.Valid(); iter.Next() {
		out = append(out, iterKV{string(iter.Key()), string(iter.Value())})
	}
	require.NoError(t, iter.Error())
	return out
}

// verifyKeyPlacement deep-inspects both backends: every oracle key must be
// present in exactly the backend(s) the placement function says it should be,
// and absent from the other.
func verifyKeyPlacement(
	t *testing.T,
	cs *CompositeCommitStore,
	oracle *storeOracle,
	placement func(store string) backendPlacement,
) {
	t.Helper()
	for store, m := range oracle.stores {
		p := placement(store)
		for k, want := range m {
			key := []byte(k)
			memVal, memFound := memiavlGetForTest(cs, store, key)
			flatVal, flatFound := flatKVGetForTest(cs, store, key)
			switch p {
			case inMemiavlOnly:
				require.True(t, memFound, "store %q key %x should be in memiavl", store, key)
				require.True(t, valuesEqual(want, memVal), "store %q key %x memiavl value mismatch", store, key)
				require.False(t, flatFound, "store %q key %x should not be in flatkv", store, key)
			case inFlatKVOnly:
				require.True(t, flatFound, "store %q key %x should be in flatkv", store, key)
				require.True(t, valuesEqual(want, flatVal), "store %q key %x flatkv value mismatch", store, key)
				require.False(t, memFound, "store %q key %x should not be in memiavl", store, key)
			case inBoth:
				require.True(t, memFound, "store %q key %x should be in memiavl (dual-write)", store, key)
				require.True(t, valuesEqual(want, memVal), "store %q key %x memiavl value mismatch", store, key)
				require.True(t, flatFound, "store %q key %x should be mirrored to flatkv (dual-write)", store, key)
				require.True(t, valuesEqual(want, flatVal), "store %q key %x flatkv value mismatch", store, key)
			}
		}
	}
}

// verifyKeyCounts asserts each backend holds exactly the number of physical
// keys implied by the oracle and the placement model (phantom-row detection).
// Only valid in steady states; migration modes write extra metadata rows and
// split keys across backends in-flight.
//
// memiavl stores every logical key 1:1, so its expected count is just the
// number of oracle keys routed to it. flatkv merges an address's nonce and
// code hash into one physical account row, so its expected count comes from the
// physical-row projection (oracleToFlatKVRows) rather than the raw key tally.
func verifyKeyCounts(
	t *testing.T,
	cs *CompositeCommitStore,
	oracle *storeOracle,
	placement func(store string) backendPlacement,
) {
	t.Helper()
	if cs.memIAVL != nil {
		var memExpected int64
		for store, m := range oracle.stores {
			switch placement(store) {
			case inMemiavlOnly, inBoth:
				memExpected += int64(len(m))
			}
		}
		require.Equal(t, memExpected, getMemIAVLKeyCount(t, cs), "memiavl physical key count")
	}
	if cs.flatKV != nil {
		require.Equal(t, int64(len(oracleToFlatKVRows(oracle, placement))), getFlatKVKeyCount(t, cs),
			"flatkv physical key count")
	}
}

// =============================================================================
// FlatKV physical-row model: project the oracle into the exact set of physical
// rows flatkv should hold, then compare row-by-row against the live DBs.
// =============================================================================

type flatKVRowKind int

const (
	rowStorage flatKVRowKind = iota
	rowAccount
	rowCode
	rowLegacy
)

// flatKVExpectedRow is the decoded, logical content expected for one physical
// flatkv row. Only the field(s) relevant to kind are populated.
type flatKVExpectedRow struct {
	kind         flatKVRowKind
	storageValue [32]byte // rowStorage
	nonce        uint64   // rowAccount
	codeHash     [32]byte // rowAccount
	code         []byte   // rowCode
	legacyValue  []byte   // rowLegacy
}

// oracleToFlatKVRows projects the oracle into the physical row layout flatkv
// uses internally, keyed by the physical (module-prefixed) key. Only stores the
// placement model routes to flatkv are included. The EVM nonce and code hash
// for a single address are merged into one account row, exactly as flatkv's
// accountDB stores them — this is what makes the row-by-row check sensitive to
// the account-merge logic. Valid only for steady-state placement.
func oracleToFlatKVRows(
	oracle *storeOracle, placement func(store string) backendPlacement) map[string]flatKVExpectedRow {
	type acct struct {
		nonce    uint64
		codeHash [32]byte
	}
	accounts := map[string]*acct{}
	getAcct := func(addr string) *acct {
		a, ok := accounts[addr]
		if !ok {
			a = &acct{}
			accounts[addr] = a
		}
		return a
	}

	rows := map[string]flatKVExpectedRow{}
	for store, m := range oracle.stores {
		switch placement(store) {
		case inFlatKVOnly, inBoth:
		default:
			continue
		}
		if store != keys.EVMStoreKey {
			for k, v := range m {
				rows[string(ktype.ModulePhysicalKey(store, []byte(k)))] =
					flatKVExpectedRow{kind: rowLegacy, legacyValue: append([]byte(nil), v...)}
			}
			continue
		}
		for k, v := range m {
			kind, stripped := keys.ParseEVMKey([]byte(k))
			switch kind {
			case keys.EVMKeyStorage:
				var sv [32]byte
				copy(sv[:], v)
				rows[string(ktype.EVMPhysicalKey(keys.EVMKeyStorage, stripped))] =
					flatKVExpectedRow{kind: rowStorage, storageValue: sv}
			case keys.EVMKeyCode:
				rows[string(ktype.EVMPhysicalKey(keys.EVMKeyCode, stripped))] =
					flatKVExpectedRow{kind: rowCode, code: append([]byte(nil), v...)}
			case keys.EVMKeyNonce:
				getAcct(string(stripped)).nonce = binary.BigEndian.Uint64(v)
			case keys.EVMKeyCodeHash:
				copy(getAcct(string(stripped)).codeHash[:], v)
			default: // EVMKeyMisc: identity-mapped under the "evm/" prefix
				rows[string(ktype.ModulePhysicalKey(keys.EVMStoreKey, []byte(k)))] =
					flatKVExpectedRow{kind: rowLegacy, legacyValue: append([]byte(nil), v...)}
			}
		}
	}

	for addr, a := range accounts {
		rows[string(ktype.EVMPhysicalKey(ktype.EVMKeyAccount, []byte(addr)))] =
			flatKVExpectedRow{kind: rowAccount, nonce: a.nonce, codeHash: a.codeHash}
	}
	return rows
}

// verifyFlatKVRows performs the row-by-row comparison: it walks every physical
// row flatkv actually holds (RawGlobalIterator exposes the raw on-disk
// account / code / storage / legacy rows) and asserts the set of physical keys
// matches the oracle's projection exactly (no missing rows, no phantom rows)
// and that each row decodes to the expected value. Migration-metadata rows are
// skipped (checked separately by verifyMigrationMetadata). Decoded fields are
// compared rather than raw bytes because the stored block height is not part of
// the logical state and legitimately differs after migration / import.
func verifyFlatKVRows(
	t *testing.T,
	cs *CompositeCommitStore,
	oracle *storeOracle,
	placement func(store string) backendPlacement,
) {
	t.Helper()
	if cs.flatKV == nil {
		return
	}
	expected := oracleToFlatKVRows(oracle, placement)

	iter, err := cs.flatKV.RawGlobalIterator()
	require.NoError(t, err)
	defer func() { _ = iter.Close() }()

	seen := make(map[string]struct{}, len(expected))
	for ; iter.Valid(); iter.Next() {
		physKey := append([]byte(nil), iter.Key()...)
		module, _, err := ktype.StripModulePrefix(physKey)
		require.NoError(t, err, "physical key missing module prefix: %x", physKey)
		if module == migration.MigrationStore {
			continue // bookkeeping rows; see verifyMigrationMetadata
		}
		exp, ok := expected[string(physKey)]
		require.True(t, ok, "phantom flatkv physical row: module=%q key=%x", module, physKey)
		assertFlatKVRowMatches(t, physKey, append([]byte(nil), iter.Value()...), exp)
		seen[string(physKey)] = struct{}{}
	}
	require.NoError(t, iter.Error())

	for k := range expected {
		if _, ok := seen[k]; !ok {
			require.Failf(t, "missing flatkv physical row", "key=%x", []byte(k))
		}
	}
}

func assertFlatKVRowMatches(t *testing.T, physKey, rawVal []byte, exp flatKVExpectedRow) {
	t.Helper()
	switch exp.kind {
	case rowStorage:
		sd, err := vtype.DeserializeStorageData(rawVal)
		require.NoError(t, err, "decode storage row %x", physKey)
		require.False(t, sd.IsDelete(), "storage row %x must not be a committed tombstone", physKey)
		require.Equal(t, exp.storageValue[:], sd.GetValue()[:], "storage value mismatch for %x", physKey)
	case rowCode:
		cd, err := vtype.DeserializeCodeData(rawVal)
		require.NoError(t, err, "decode code row %x", physKey)
		require.Equal(t, exp.code, cd.GetBytecode(), "code bytecode mismatch for %x", physKey)
	case rowAccount:
		ad, err := vtype.DeserializeAccountData(rawVal)
		require.NoError(t, err, "decode account row %x", physKey)
		require.Equal(t, exp.nonce, ad.GetNonce(), "account nonce mismatch for %x", physKey)
		require.Equal(t, exp.codeHash[:], ad.GetCodeHash()[:], "account code hash mismatch for %x", physKey)
		var zeroBalance vtype.Balance
		require.Equal(t, zeroBalance[:], ad.GetBalance()[:],
			"account balance must be zero (balances are not stored in flatkv yet) for %x", physKey)
	case rowLegacy:
		ld, err := vtype.DeserializeMiscData(rawVal)
		require.NoError(t, err, "decode legacy row %x", physKey)
		require.True(t, valuesEqual(exp.legacyValue, ld.GetValue()), "legacy value mismatch for %x", physKey)
	}
}

// assertFlatKVMapsExercised checks the workload populated every flatkv physical
// map the current placement routes data into, so the suite genuinely exercises
// each on-disk format (and, for the account map, the nonce+codehash merge).
// Counts are derived from the oracle; verifyFlatKVRows independently proves
// flatkv matches the oracle, so a non-empty oracle category implies a non-empty
// flatkv map.
func assertFlatKVMapsExercised(t *testing.T, oracle *storeOracle, placement func(store string) backendPlacement) {
	t.Helper()
	var storageRows, codeRows, legacyRows int
	type acctFlags struct{ nonce, codeHash bool }
	accounts := map[string]*acctFlags{}
	flag := func(addr string) *acctFlags {
		a, ok := accounts[addr]
		if !ok {
			a = &acctFlags{}
			accounts[addr] = a
		}
		return a
	}

	for store, m := range oracle.stores {
		switch placement(store) {
		case inFlatKVOnly, inBoth:
		default:
			continue
		}
		if store != keys.EVMStoreKey {
			legacyRows += len(m)
			continue
		}
		for k := range m {
			kind, stripped := keys.ParseEVMKey([]byte(k))
			switch kind {
			case keys.EVMKeyStorage:
				storageRows++
			case keys.EVMKeyCode:
				codeRows++
			case keys.EVMKeyNonce:
				flag(string(stripped)).nonce = true
			case keys.EVMKeyCodeHash:
				flag(string(stripped)).codeHash = true
			default:
				legacyRows++
			}
		}
	}

	var accountRows, collisions int
	for _, af := range accounts {
		accountRows++
		if af.nonce && af.codeHash {
			collisions++
		}
	}

	evmPlacement := placement(keys.EVMStoreKey)
	if evmPlacement == inFlatKVOnly || evmPlacement == inBoth {
		require.Positive(t, storageRows, "expected storage-map rows in flatkv")
		require.Positive(t, codeRows, "expected code-map rows in flatkv")
		require.Positive(t, accountRows, "expected account-map rows in flatkv")
		require.Positive(t, collisions,
			"expected at least one account with both a nonce and a code hash (account-map collision)")
	}

	legacyExpected := false
	for _, store := range randomTestStores {
		if store == keys.EVMStoreKey {
			continue
		}
		if p := placement(store); p == inFlatKVOnly || p == inBoth {
			legacyExpected = true
		}
	}
	if legacyExpected {
		require.Positive(t, legacyRows, "expected legacy-map rows in flatkv")
	}
}

// verifyMigrationMetadata asserts the presence/absence of the migration
// version and boundary keys in flatkv's reserved MigrationStore.
func verifyMigrationMetadata(t *testing.T, cs *CompositeCommitStore, wantVersion, wantBoundary bool) {
	t.Helper()
	if cs.flatKV == nil {
		require.False(t, wantVersion, "no flatkv backend: version key cannot be present")
		require.False(t, wantBoundary, "no flatkv backend: boundary key cannot be present")
		return
	}
	_, versionPresent := cs.flatKV.Get(migration.MigrationStore, []byte(migration.MigrationVersionKey))
	_, boundaryPresent := cs.flatKV.Get(migration.MigrationStore, []byte(migration.MigrationBoundaryKey))
	require.Equal(t, wantVersion, versionPresent, "migration version key presence")
	require.Equal(t, wantBoundary, boundaryPresent, "migration boundary key presence")
}

// verifyCommitInfo asserts the evm_lattice StoreInfo is present (or absent) in
// both LastCommitInfo and WorkingCommitInfo, matching whether flatkv
// participates in the AppHash for this mode.
func verifyCommitInfo(t *testing.T, cs *CompositeCommitStore, expectLattice bool) {
	t.Helper()
	last := cs.LastCommitInfo()
	require.NotNil(t, last)
	require.Equal(t, expectLattice, containsLatticeStoreInfo(last.StoreInfos),
		"evm_lattice presence in LastCommitInfo")
	working := cs.WorkingCommitInfo()
	require.NotNil(t, working)
	require.Equal(t, expectLattice, containsLatticeStoreInfo(working.StoreInfos),
		"evm_lattice presence in WorkingCommitInfo")
}

// assertCommitInfoEqual asserts two CommitInfos describe the same committed
// state: identical version and an identical set of per-store {name, version,
// hash} entries (order-independent). Callers should pass deep copies
// (cloneStoreInfos) when either side is a live store whose mmap-backed buffers
// may be reused after a close/reopen.
func assertCommitInfoEqual(t *testing.T, label string, want, got *proto.CommitInfo) {
	t.Helper()
	require.NotNil(t, want, "%s: want CommitInfo is nil", label)
	require.NotNil(t, got, "%s: got CommitInfo is nil", label)
	require.Equal(t, want.Version, got.Version, "%s: commit version", label)
	require.Equal(t, storeInfoMap(want.StoreInfos), storeInfoMap(got.StoreInfos), "%s: per-store commit info", label)
}

// storeInfoMap reduces a StoreInfo slice to a name->CommitID map so two commit
// infos can be compared independent of store ordering.
func storeInfoMap(infos []proto.StoreInfo) map[string]proto.CommitID {
	out := make(map[string]proto.CommitID, len(infos))
	for _, si := range infos {
		out[si.Name] = proto.CommitID{Version: si.CommitId.Version, Hash: append([]byte(nil), si.CommitId.Hash...)}
	}
	return out
}

// snapshotCommitInfo returns a deep copy of cs.LastCommitInfo safe to retain
// across a close/reopen (memiavl hashes are mmap-backed).
func snapshotCommitInfo(cs *CompositeCommitStore) *proto.CommitInfo {
	last := cs.LastCommitInfo()
	if last == nil {
		return nil
	}
	return &proto.CommitInfo{Version: last.Version, StoreInfos: cloneStoreInfos(last.StoreInfos)}
}

// verifyProofRouting samples one key per store and asserts GetProof succeeds
// for memiavl-backed stores (which support ICS-23 proofs) and fails for
// flatkv-backed stores (which do not). For memiavl-backed stores it goes
// further and verifies the returned proof CRYPTOGRAPHICALLY against the live
// tree's root via Tree.VerifyMembership, so a structurally-valid-but-wrong
// proof would be caught -- not just that GetProof returned without error.
func verifyProofRouting(
	t *testing.T,
	cs *CompositeCommitStore,
	oracle *storeOracle,
	placement func(store string) backendPlacement,
) {
	t.Helper()
	for store, m := range oracle.stores {
		// Pick a key whose value is non-empty: ics23 existence proofs cannot
		// represent an empty value (LeafOp.Apply requires a non-empty value),
		// so empty-value keys -- valid state, but not ics23-provable -- are
		// unusable for cryptographic membership verification. Routing (success
		// vs error) does not depend on the value, so any non-empty-value key is
		// representative.
		var key []byte
		for k, v := range m {
			if len(v) > 0 {
				key = []byte(k)
				break
			}
		}
		if key == nil {
			continue
		}
		proof, err := cs.GetProof(store, key)
		switch placement(store) {
		case inMemiavlOnly, inBoth:
			require.NoError(t, err, "proof should succeed for memiavl-backed store %q", store)
			require.NotNil(t, proof, "proof must be non-nil for memiavl-backed store %q", store)
			tree := cs.memIAVL.GetDB().TreeByName(store)
			require.NotNil(t, tree, "memiavl tree %q must exist for proof verification", store)
			require.True(t, tree.VerifyMembership(proof, key),
				"membership proof must verify against the tree root for store %q key %x", store, key)
		case inFlatKVOnly:
			require.Error(t, err, "proof should fail for flatkv-backed store %q", store)
		}
	}
}

// migrationHighSentinelCount is the number of high sentinels
// seedMigrationSentinels writes per migrating store. It must exceed the
// maximum number of keys the migration can drain before the last
// assertMigrationInFlight call of a scenario: currently 11 effective
// migration-mode blocks (5 + 3 in runMigrationScenario, plus 2 + 1 in
// runMidMigrationInterleavings; rolled-back blocks rewind the boundary and do
// not count) times the maximum KeysToMigratePerBlock of 5, i.e. 55. If a
// scenario gains migration-mode blocks beyond this budget, raise the count.
const migrationHighSentinelCount = 64

// migrationSentinelPairs returns the sentinel writes seedMigrationSentinels
// applies to one store: one "low" key that sorts before every key the random
// workload can generate, plus migrationHighSentinelCount "high" keys that sort
// after every generatable key. The shapes are chosen so a workload key can
// never collide with a sentinel (not merely with negligible probability):
//
//   - low:  []byte{0x00} — cosmos workload keys are exactly 8 bytes, EVM
//     workload keys always start with 0x01/0x03/0x07/0x08/0x0a, so no
//     generated key is the 1-byte 0x00. Sorts first in either keyspace.
//   - high: 0xFF^8 || i (9 bytes) — longer than any cosmos workload key and
//     greater than any 8-byte key; no EVM prefix is 0xFF. For the EVM store,
//     ParseEVMKey classifies these (and the low key) as legacy-lane keys, a
//     shape the workload already writes.
//
// Values are fixed and non-empty (memiavlGetForTest treats nil as absent).
func migrationSentinelPairs() []*proto.KVPair {
	pairs := make([]*proto.KVPair, 0, 1+migrationHighSentinelCount)
	pairs = append(pairs, &proto.KVPair{Key: []byte{0x00}, Value: []byte{0x01}})
	for i := range migrationHighSentinelCount {
		key := append(bytes.Repeat([]byte{0xFF}, 8), byte(i))
		pairs = append(pairs, &proto.KVPair{Key: key, Value: []byte{0x01}})
	}
	return pairs
}

// seedMigrationSentinels commits sentinel keys into each given store (and the
// oracle) so that assertMigrationInFlight holds deterministically rather than
// resting on seed-dependent workload-volume margins. The sentinels are never
// added to the live-key set, so the workload can never update or delete them:
//
//   - The low sentinel sorts first, so it is drained by the migration's first
//     batch; every assertMigrationInFlight call site runs after at least one
//     migration-mode commit, so a migrated, still-live key is guaranteed to
//     exist in flatkv.
//   - The high sentinels are migrationHighSentinelCount never-deleted keys, so
//     while the migration has drained fewer keys than that (see the constant's
//     budget), at least one of them is still un-migrated in memiavl.
func seedMigrationSentinels(t *testing.T, cs *CompositeCommitStore, oracle *storeOracle, stores []string) {
	t.Helper()
	startVersion := cs.Version()
	sorted := append([]string(nil), stores...)
	sort.Strings(sorted)
	cset := make([]*proto.NamedChangeSet, 0, len(sorted))
	for _, store := range sorted {
		cset = append(cset, &proto.NamedChangeSet{
			Name:      store,
			Changeset: proto.ChangeSet{Pairs: migrationSentinelPairs()},
		})
	}
	require.NoError(t, cs.ApplyChangeSets(cset), "ApplyChangeSets(sentinels)")
	oracle.apply(cset)
	version, err := cs.Commit()
	require.NoError(t, err, "Commit(sentinels)")
	require.Equal(t, startVersion+1, version)
}

// assertMigrationInFlight verifies that, across migratingStores, at least one
// tracked key is still in memiavl (un-migrated) AND at least one is already in
// flatkv (migrated). Use immediately before a mid-migration restart to confirm
// the test actually exercises the in-flight resume path. Ported from the
// migration framework's AssertMigrationInFlight.
//
// Callers must have seeded the migrating stores with seedMigrationSentinels;
// those keys make this assertion deterministic instead of dependent on how
// many keys the seeded workload happened to produce for a given seed.
func assertMigrationInFlight(t *testing.T, cs *CompositeCommitStore, oracle *storeOracle, migratingStores ...string) {
	t.Helper()
	targets := make(map[string]bool, len(migratingStores))
	for _, s := range migratingStores {
		targets[s] = true
	}
	var foundInMemiavl, foundInFlatKV bool
outer:
	for store, m := range oracle.stores {
		if !targets[store] {
			continue
		}
		for k := range m {
			key := []byte(k)
			if !foundInMemiavl {
				if _, ok := memiavlGetForTest(cs, store, key); ok {
					foundInMemiavl = true
				}
			}
			if !foundInFlatKV {
				if _, ok := flatKVGetForTest(cs, store, key); ok {
					foundInFlatKV = true
				}
			}
			if foundInMemiavl && foundInFlatKV {
				break outer
			}
		}
	}
	require.True(t, foundInMemiavl,
		"expected at least one un-migrated key in memiavl across %v; migration completed before the checkpoint "+
			"(reduce phase length or raise source-key volume)", migratingStores)
	require.True(t, foundInFlatKV,
		"expected at least one migrated key in flatkv across %v; migration had not started by the checkpoint "+
			"(increase phase length or lower the batch size)", migratingStores)
}

// =============================================================================
// Lifecycle helpers.
// =============================================================================

// randomTestConfig returns a StateCommitConfig for the given mode with
// deterministic snapshot settings and RANDOMIZED snapshot intervals (drawn from
// rng and logged for reproducibility). AsyncCommitBuffer=0 keeps the on-disk
// WAL tail in step with each Commit so restarts replay deterministically.
//
// The interval is varied across {1,3,5} to exercise the snapshot+WAL-replay
// reconstruction paths (a SnapshotInterval of 1 trivially has a snapshot at
// every height and never replays). To keep that safe for the rollback-,
// historical-read-, and state-sync-heavy scenarios -- all of which reconstruct
// arbitrary, non-snapshot-aligned past versions via seekSnapshot + WAL replay
// -- every snapshot is retained for the (short) duration of a test by setting
// SnapshotKeepRecent very high. Without that, pruning could remove the base
// snapshot/WAL a past version needs and a non-1 interval would be unsafe.
func randomTestConfig(t *testing.T, rng *testutil.TestRandom, mode types.WriteMode) config.StateCommitConfig {
	t.Helper()
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = mode

	intervals := []uint32{1, 3, 5}
	memInterval := intervals[rng.Intn(len(intervals))]
	flatInterval := intervals[rng.Intn(len(intervals))]

	cfg.MemIAVLConfig.SnapshotInterval = memInterval
	cfg.MemIAVLConfig.SnapshotMinTimeInterval = 0
	cfg.MemIAVLConfig.AsyncCommitBuffer = 0
	cfg.MemIAVLConfig.SnapshotKeepRecent = 10000

	cfg.FlatKVConfig.SnapshotInterval = flatInterval
	cfg.FlatKVConfig.SnapshotKeepRecent = 10000

	t.Logf("randomTestConfig mode=%s memSnapshotInterval=%d flatSnapshotInterval=%d",
		mode, memInterval, flatInterval)
	return cfg
}

// openComposite constructs, initializes (with the full canonical store set),
// and loads a composite store at the latest version, registering cleanup.
// testMigrationBatchSize is the per-block migration rate the framework
// re-applies on every store open. The rate is no longer a persisted config;
// production re-reads the governance-controlled migration.NumKeysToMigratePerBlock
// param in BeginBlock and re-applies it after every restart, so the framework
// mirrors that by re-applying it whenever a store is (re)opened (open / restart
// / state-sync clone). 0 leaves the migration paused, which is correct for the
// steady-state scenarios; migration scenarios set it for their duration.
var testMigrationBatchSize int

// applyTestMigrationBatchSize re-applies testMigrationBatchSize to a freshly
// opened store, mimicking the app's BeginBlock push of the gov param.
func applyTestMigrationBatchSize(t *testing.T, cs *CompositeCommitStore) {
	t.Helper()
	if testMigrationBatchSize > 0 {
		require.NoError(t, cs.SetMigrationBatchSize(testMigrationBatchSize))
	}
}

func openComposite(t *testing.T, dir string, cfg config.StateCommitConfig) *CompositeCommitStore {
	t.Helper()
	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, cs.Initialize(keys.MemIAVLStoreKeys))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	applyTestMigrationBatchSize(t, cs)
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// restartComposite closes cs and reopens a fresh handle on the same directory,
// simulating a process restart (snapshot load + WAL-tail replay). cfg may
// differ from the original to exercise a mode flip across the restart.
//
// When the WriteMode is unchanged, the reopened store must report byte-
// identical commit info (same version and per-store hashes): a snapshot+WAL
// reload must not perturb the AppHash. The check is skipped on a mode flip,
// where the participating stores (e.g. evm_lattice) legitimately change.
func restartComposite(
	t *testing.T, cs *CompositeCommitStore, dir string, cfg config.StateCommitConfig) *CompositeCommitStore {
	t.Helper()
	sameMode := cs.config.WriteMode == cfg.WriteMode
	var before *proto.CommitInfo
	if sameMode {
		before = snapshotCommitInfo(cs)
	}
	require.NoError(t, cs.Close())
	reopened := openComposite(t, dir, cfg)
	if sameMode {
		assertCommitInfoEqual(t, "restart", before, snapshotCommitInfo(reopened))
	}
	return reopened
}

// stateSyncClone exports src at the given version, replays the stream into a
// brand-new directory's importer, and returns the reopened clone loaded at
// that version. Mirrors the export/import dance in TestExportImportEVMMigrated.
func stateSyncClone(
	t *testing.T, src *CompositeCommitStore, version int64, cfg config.StateCommitConfig) *CompositeCommitStore {
	t.Helper()

	exporter, err := src.Exporter(version)
	require.NoError(t, err)
	items := drainCompositeExporter(t, exporter)
	require.NoError(t, exporter.Close())

	dstDir := t.TempDir()
	dst, err := NewCompositeCommitStore(t.Context(), dstDir, cfg)
	require.NoError(t, err)
	require.NoError(t, dst.Initialize(keys.MemIAVLStoreKeys))
	// Open then close the writable handle so the importer takes over a
	// freshly-initialized, unlocked store (matches the production state-sync
	// import path and TestExportImportEVMMigrated).
	_, err = dst.LoadVersion(0, false)
	require.NoError(t, err)
	require.NoError(t, dst.Close())

	importer, err := dst.Importer(version)
	require.NoError(t, err)
	replayImport(t, importer, items)
	require.NoError(t, importer.Close())

	_, err = dst.LoadVersion(version, false)
	require.NoError(t, err)
	applyTestMigrationBatchSize(t, dst)
	t.Cleanup(func() { _ = dst.Close() })

	// State sync must reproduce identical committed state. When the source is
	// at the export version, the clone's commit info there must match it
	// exactly -- same version and per-store hashes, including evm_lattice. This
	// is a strong end-to-end fidelity check on export/import that goes beyond
	// the row-by-row value comparison the caller also runs.
	srcInfo := snapshotCommitInfo(src)
	if srcInfo != nil && srcInfo.Version == version {
		assertCommitInfoEqual(t, "state-sync clone", srcInfo, snapshotCommitInfo(dst))
	}
	return dst
}

// readHistoricalSnapshot opens a read-only handle at a past version and asserts
// it returns exactly the checkpoint oracle snapshot (value reads + iteration),
// then closes it. The writable store is a separate handle and is left
// untouched. Backends reconstruct an arbitrary past version via
// seekSnapshot + WAL replay (snapshots are retained for the whole test; see
// randomTestConfig), so version need not be snapshot-aligned.
func readHistoricalSnapshot(t *testing.T, cs *CompositeCommitStore, version int64, snap *storeOracle, stores []string) {
	t.Helper()
	committer, err := cs.LoadVersion(version, true)
	require.NoError(t, err, "read-only LoadVersion at historical version %d", version)
	ro, ok := committer.(*CompositeCommitStore)
	require.True(t, ok, "read-only LoadVersion must return *CompositeCommitStore")
	defer func() { require.NoError(t, ro.Close()) }()
	require.Equal(t, version, ro.Version(), "read-only handle must report the historical version")
	verifyOracle(t, ro, snap)
	verifyIteration(t, ro, snap, stores)
}

// rollbackFlatKVIndependently opens the flatkv backend on dir in isolation and
// rolls it back to target, simulating a crash that left flatkv one or more
// versions behind memiavl. Mirrors the crash simulation in
// TestReconcileVersionsAfterCrash.
func rollbackFlatKVIndependently(t *testing.T, dir string, cfg config.StateCommitConfig, target int64) {
	t.Helper()
	flatkvCfg := cfg.FlatKVConfig
	flatkvCfg.DataDir = utils.GetFlatKVPath(dir)
	evmStore, err := flatkv.NewCommitStore(t.Context(), &flatkvCfg)
	require.NoError(t, err)
	_, err = evmStore.LoadVersion(0, false)
	require.NoError(t, err)
	require.NoError(t, evmStore.Rollback(target))
	require.NoError(t, evmStore.Close())
}

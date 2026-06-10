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
	"encoding/binary"
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
	s := newLiveKeySet()
	for store, m := range o.stores {
		for k := range m {
			s.Add(keyPair{store: store, key: k})
		}
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
//
// Returning a slice (rather than one pair) is what lets a single logical
// account own both a nonce and a code hash within one block.
func newRandomEVMEntry(rng *testutil.TestRandom) []*proto.KVPair {
	switch rng.Intn(3) {
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
	default:
		addr := randomTestBytes(rng, keys.AddressLen)
		stripped := append(addr, randomTestBytes(rng, vtype.SlotLen)...) // addr || slot
		return []*proto.KVPair{{Key: keys.BuildEVMKey(keys.EVMKeyStorage, stripped), Value: randomStorageValue(rng)}}
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
	default: // storage
		return randomStorageValue(rng)
	}
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
	blocks          int
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
			pair := &proto.KVPair{Key: randomTestBytes(rng, 8), Value: randomTestBytes(rng, 8)}
			allPairs[store] = append(allPairs[store], pair)
			keysInUse.Add(keyPair{store: store, key: string(pair.Key)})
		}

		// Overwrite existing keys with fresh values.
		for _, kp := range keysInUse.Sample(rng, p.updatesPerBlock) {
			var value []byte
			if kp.store == keys.EVMStoreKey {
				value = freshEVMValue(rng, []byte(kp.key))
			} else {
				value = randomTestBytes(rng, 8)
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

		// Build the changeset slice in deterministic store-name order.
		storeNames := make([]string, 0, len(allPairs))
		for store := range allPairs {
			storeNames = append(storeNames, store)
		}
		sort.Strings(storeNames)
		cset := make([]*proto.NamedChangeSet, 0, len(allPairs))
		for _, store := range storeNames {
			cset = append(cset, &proto.NamedChangeSet{Name: store, Changeset: proto.ChangeSet{Pairs: allPairs[store]}})
		}

		require.NoError(t, cs.ApplyChangeSets(cset), "ApplyChangeSets")
		oracle.apply(cset)
		version, err := cs.Commit()
		require.NoError(t, err, "Commit")
		require.Equal(t, startVersion+1, version, "Commit must advance the version by exactly one")

		for _, kp := range toDelete {
			keysInUse.Remove(kp)
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
			require.Equal(t, want, got, "Get value mismatch store=%q key=%x", kp.store, key)

			has, err := cs.Has(kp.store, key)
			require.NoError(t, err)
			require.True(t, has, "Has must agree with Get store=%q key=%x", kp.store, key)

			child := cs.GetChildStoreByName(kp.store)
			require.Equal(t, want, child.Get(key),
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
func steadyStatePlacement(mode config.WriteMode) func(store string) backendPlacement {
	switch mode {
	case config.MemiavlOnly:
		return func(string) backendPlacement { return inMemiavlOnly }
	case config.FlatKVOnly:
		return func(string) backendPlacement { return inFlatKVOnly }
	case config.EVMMigrated:
		return func(store string) backendPlacement {
			if store == keys.EVMStoreKey {
				return inFlatKVOnly
			}
			return inMemiavlOnly
		}
	case config.AllMigratedButBank:
		return func(store string) backendPlacement {
			if store == keys.BankStoreKey {
				return inMemiavlOnly
			}
			return inFlatKVOnly
		}
	case config.TestOnlyDualWrite:
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
			require.Equal(t, want, got, "value mismatch store=%q key=%x", store, key)
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
	t.Helper()
	iter, err := cs.Iterator(store, nil, nil, ascending)
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
				require.Equal(t, want, memVal, "store %q key %x memiavl value mismatch", store, key)
				require.False(t, flatFound, "store %q key %x should not be in flatkv", store, key)
			case inFlatKVOnly:
				require.True(t, flatFound, "store %q key %x should be in flatkv", store, key)
				require.Equal(t, want, flatVal, "store %q key %x flatkv value mismatch", store, key)
				require.False(t, memFound, "store %q key %x should not be in memiavl", store, key)
			case inBoth:
				require.True(t, memFound, "store %q key %x should be in memiavl (dual-write)", store, key)
				require.Equal(t, want, memVal, "store %q key %x memiavl value mismatch", store, key)
				require.True(t, flatFound, "store %q key %x should be mirrored to flatkv (dual-write)", store, key)
				require.Equal(t, want, flatVal, "store %q key %x flatkv value mismatch", store, key)
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
func oracleToFlatKVRows(oracle *storeOracle, placement func(store string) backendPlacement) map[string]flatKVExpectedRow {
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
			default: // EVMKeyLegacy: identity-mapped under the "evm/" prefix
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
		ld, err := vtype.DeserializeLegacyData(rawVal)
		require.NoError(t, err, "decode legacy row %x", physKey)
		require.Equal(t, exp.legacyValue, ld.GetValue(), "legacy value mismatch for %x", physKey)
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

// verifyProofRouting samples one key per store and asserts GetProof succeeds
// for memiavl-backed stores (which support ICS-23 proofs) and fails for
// flatkv-backed stores (which do not).
func verifyProofRouting(
	t *testing.T,
	cs *CompositeCommitStore,
	oracle *storeOracle,
	placement func(store string) backendPlacement,
) {
	t.Helper()
	for store, m := range oracle.stores {
		var key []byte
		for k := range m {
			key = []byte(k)
			break
		}
		if key == nil {
			continue
		}
		_, err := cs.GetProof(store, key)
		switch placement(store) {
		case inMemiavlOnly, inBoth:
			require.NoError(t, err, "proof should succeed for memiavl-backed store %q", store)
		case inFlatKVOnly:
			require.Error(t, err, "proof should fail for flatkv-backed store %q", store)
		}
	}
}

// assertMigrationInFlight verifies that, across migratingStores, at least one
// tracked key is still in memiavl (un-migrated) AND at least one is already in
// flatkv (migrated). Use immediately before a mid-migration restart to confirm
// the test actually exercises the in-flight resume path. Ported from the
// migration framework's AssertMigrationInFlight.
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

// randomTestConfig returns a StateCommitConfig for the given mode with fast,
// deterministic snapshot settings: SnapshotInterval=1 (so a memiavl snapshot
// exists at every height for state-sync export) and AsyncCommitBuffer=0 (so
// the on-disk WAL tail reflects each Commit before the next read / restart).
func randomTestConfig(mode config.WriteMode) config.StateCommitConfig {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = mode
	cfg.MemIAVLConfig.SnapshotInterval = 1
	cfg.MemIAVLConfig.SnapshotMinTimeInterval = 0
	cfg.MemIAVLConfig.AsyncCommitBuffer = 0
	return cfg
}

// openComposite constructs, initializes (with the full canonical store set),
// and loads a composite store at the latest version, registering cleanup.
func openComposite(t *testing.T, dir string, cfg config.StateCommitConfig) *CompositeCommitStore {
	t.Helper()
	cs, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, cs.Initialize(keys.MemIAVLStoreKeys))
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// restartComposite closes cs and reopens a fresh handle on the same directory,
// simulating a process restart (snapshot load + WAL-tail replay). cfg may
// differ from the original to exercise a mode flip across the restart.
func restartComposite(t *testing.T, cs *CompositeCommitStore, dir string, cfg config.StateCommitConfig) *CompositeCommitStore {
	t.Helper()
	require.NoError(t, cs.Close())
	return openComposite(t, dir, cfg)
}

// stateSyncClone exports src at the given version, replays the stream into a
// brand-new directory's importer, and returns the reopened clone loaded at
// that version. Mirrors the export/import dance in TestExportImportEVMMigrated.
func stateSyncClone(t *testing.T, src *CompositeCommitStore, version int64, cfg config.StateCommitConfig) *CompositeCommitStore {
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
	t.Cleanup(func() { _ = dst.Close() })
	return dst
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

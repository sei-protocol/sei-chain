package composite

import (
	"encoding/binary"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/testutil"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// oracleStore is the in-memory source of truth that the fuzz suite compares
// every read-path result against. Outer map keys are module/store names,
// inner map keys are stringified store keys.
//
// Live in the composite package's test binary; intentionally distinct from
// the package-internal TestInMemoryRouter that lives under migration/.
type oracleStore struct {
	stores map[string]map[string][]byte
}

// newOracleStore returns an empty oracle.
func newOracleStore() *oracleStore {
	return &oracleStore{stores: make(map[string]map[string][]byte)}
}

// Apply replays a slice of NamedChangeSets against the oracle in the same
// order they would be applied by the composite store.
func (o *oracleStore) Apply(changesets []*proto.NamedChangeSet) {
	for _, ncs := range changesets {
		if ncs == nil {
			continue
		}
		storeMap, ok := o.stores[ncs.Name]
		if !ok {
			storeMap = make(map[string][]byte)
			o.stores[ncs.Name] = storeMap
		}
		for _, pair := range ncs.Changeset.Pairs {
			if pair == nil {
				continue
			}
			if pair.Delete {
				delete(storeMap, string(pair.Key))
				continue
			}
			// Copy the value: the oracle outlives the changeset slice
			// and must not alias caller-owned memory.
			value := make([]byte, len(pair.Value))
			copy(value, pair.Value)
			storeMap[string(pair.Key)] = value
		}
	}
}

// Get returns the value the oracle holds for (store, key) and whether the
// key is present.
func (o *oracleStore) Get(store string, key []byte) ([]byte, bool) {
	storeMap, ok := o.stores[store]
	if !ok {
		return nil, false
	}
	v, ok := storeMap[string(key)]
	if !ok {
		return nil, false
	}
	return v, true
}

// Has is a thin wrapper around Get for symmetry with the composite API.
func (o *oracleStore) Has(store string, key []byte) bool {
	_, ok := o.Get(store, key)
	return ok
}

// Snapshot returns a deep copy of the oracle. Used by the parallel-replica
// state-sync test to freeze the source-of-truth at the moment of export,
// then continue mutating each replica's oracle independently before
// reconciling.
func (o *oracleStore) Snapshot() *oracleStore {
	out := newOracleStore()
	for storeName, storeMap := range o.stores {
		copyMap := make(map[string][]byte, len(storeMap))
		for k, v := range storeMap {
			cv := make([]byte, len(v))
			copy(cv, v)
			copyMap[k] = cv
		}
		out.stores[storeName] = copyMap
	}
	return out
}

// keyPair identifies a single live key in the oracle by (store, key).
type keyPair struct {
	store string
	key   string
}

// liveKeySetFromOracle rebuilds a liveKeySet whose membership matches the
// oracle's current key map. Used by tests that snapshot an oracle, mutate
// it forward, then restore — they only need to clone the oracle (oracle
// already has Snapshot) and can recompute the liveKeySet from the clone.
// Iteration order is non-deterministic, but rng-driven sampling is the
// only consumer and it does not assume insertion order.
func liveKeySetFromOracle(o *oracleStore) *liveKeySet {
	s := newLiveKeySet()
	for storeName, storeMap := range o.stores {
		for k := range storeMap {
			s.Add(keyPair{store: storeName, key: k})
		}
	}
	return s
}

// liveKeySet supports O(1) Add, O(1) Remove, and O(n) deterministic random
// sampling without relying on map iteration order. Mirrors the analogous
// helper used by the migration-package fuzz tests. Sampling is reproducible
// from the rng seed.
type liveKeySet struct {
	keys []keyPair
	idx  map[keyPair]int
}

func newLiveKeySet() *liveKeySet {
	return &liveKeySet{idx: make(map[keyPair]int)}
}

func (s *liveKeySet) Len() int { return len(s.keys) }

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
// algorithm. Output is reproducible from the rng's seed.
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

// randomKVPair returns a non-EVM 8-byte key/value pair. Used for every
// store except keys.EVMStoreKey.
func randomKVPair(rng *testutil.TestRandom) *proto.KVPair {
	return &proto.KVPair{
		Key:   rng.Bytes(8),
		Value: rng.Bytes(8),
	}
}

// evmAddrPoolSize is the size of the deterministic EVM address pool the
// data generators draw from. Picked deliberately small (relative to the
// per-test write volume) so that random nonce / codeHash / code / storage
// draws land on the SAME address often enough that flatkv's accountDB
// merge path (nonce + codeHash combined into a single physical row) is
// stochastically exercised every run. With ~100 EVM writes per block and
// 32 addresses, ~3 nonce writes and ~3 codeHash writes share an address
// per block on average.
const evmAddrPoolSize = 32

// evmSlotPoolSize is the size of the storage-slot pool. Smaller than
// evmAddrPoolSize so that repeat writes to the same (addr, slot) tuple
// happen frequently — this exercises pendingWrites coalescing inside
// flatkv's storageDB path.
const evmSlotPoolSize = 8

// evmAddressFromPool returns a 20-byte EVM address drawn uniformly from a
// finite pool. The pool index lives in the high 4 bytes; the rest are
// zero. Deterministic for a given pool index so that nonces, code hashes
// and codes generated for the same index land on the same accountDB row.
func evmAddressFromPool(rng *testutil.TestRandom) []byte {
	idx := uint32(rng.Intn(evmAddrPoolSize))
	addr := make([]byte, keys.AddressLen)
	binary.BigEndian.PutUint32(addr[:4], idx)
	return addr
}

// evmSlotFromPool returns a 32-byte storage slot drawn uniformly from a
// finite pool. The pool index lives in the high 4 bytes; the rest are
// zero. Deterministic so that repeat draws hit the same flatkv storage
// row.
func evmSlotFromPool(rng *testutil.TestRandom) []byte {
	idx := uint32(rng.Intn(evmSlotPoolSize))
	slot := make([]byte, 32)
	binary.BigEndian.PutUint32(slot[:4], idx)
	return slot
}

// randomEVMKVPair returns a random but structurally valid EVM key-value
// pair for use with the keys.EVMStoreKey store. The four EVM key kinds
// (nonce, code hash, code, storage) are selected uniformly. Addresses
// and storage slots come from a small deterministic pool so that the
// suite frequently writes more than one field to the same accountDB row
// (exercising the nonce + codeHash merge path) and the same storage row
// (exercising slot-level coalescing).
func randomEVMKVPair(rng *testutil.TestRandom) *proto.KVPair {
	addr := evmAddressFromPool(rng)
	switch rng.Intn(4) {
	case 0:
		return &proto.KVPair{Key: keys.BuildEVMKey(keys.EVMKeyNonce, addr), Value: rng.Bytes(8)}
	case 1:
		return &proto.KVPair{Key: keys.BuildEVMKey(keys.EVMKeyCodeHash, addr), Value: rng.Bytes(32)}
	case 2:
		return &proto.KVPair{Key: keys.BuildEVMKey(keys.EVMKeyCode, addr), Value: rng.Bytes(32)}
	default:
		slot := evmSlotFromPool(rng)
		stripped := append(addr, slot...)
		return &proto.KVPair{Key: keys.BuildEVMKey(keys.EVMKeyStorage, stripped), Value: rng.Bytes(32)}
	}
}

// randomEVMValue returns a fresh random value of the correct length for the
// given EVM key. Used when updating an existing EVM key so the new value
// matches the on-disk length constraint for that key kind.
func randomEVMValue(rng *testutil.TestRandom, key []byte) []byte {
	kind, _ := keys.ParseEVMKey(key)
	switch kind {
	case keys.EVMKeyNonce:
		return rng.Bytes(8)
	case keys.EVMKeyCodeHash, keys.EVMKeyCode, keys.EVMKeyStorage:
		return rng.Bytes(32)
	default:
		return rng.Bytes(8)
	}
}

// oracleFlatkvShape converts the oracle map into the per-DB shape that
// flatkv would store the same data in: one accountDB row per address
// that has either a nonce OR a codeHash assignment, one codeDB row per
// address with a code assignment, one storageDB row per (addr, slot),
// and one legacyDB row per remaining (module, key). The returned struct
// is exactly what an end-of-test physical-count probe should expect to
// see on disk in flatkv (plus any migration-metadata rows, which the
// caller layers on separately).
//
// Important: the helper does NOT model flatkv's IsDelete pruning. The
// oracle only ever stores live keys, so this is correct as long as the
// composite test never artificially leaves a "tombstone-with-no-fields"
// row on disk — flatkv's own batch path deletes such rows on Commit, so
// every live oracle entry maps to at least the accountDB / codeDB /
// storageDB / legacyDB row computed here.
type oracleFlatkvShape struct {
	accountRows int64 // unique addresses with a live nonce OR codeHash
	codeRows    int64 // unique addresses with a live code
	storageRows int64 // unique (addr, slot) tuples with live storage
	legacyRows  int64 // remaining non-EVM keys + any legacy-shaped EVM data
}

// total returns the sum of every per-DB row count.
func (s oracleFlatkvShape) total() int64 {
	return s.accountRows + s.codeRows + s.storageRows + s.legacyRows
}

// oracleFlatkvShapeFor partitions the oracle keys that belong to a
// flatkv-backed store (per profile.finalPlacement) into the per-DB shape
// flatkv would physically store them in.
//
// The set of "flatkv-backed" stores is determined by the caller passing
// the set of placements to count. For modes with a single backendID per
// store, that is just the store's placement; for TestOnlyDualWrite the
// EVM store is also counted because the dual-write router mirrors EVM
// data into flatkv.
func oracleFlatkvShapeFor(oracle *oracleStore, includeStore func(store string) bool) oracleFlatkvShape {
	var shape oracleFlatkvShape
	for storeName, storeMap := range oracle.stores {
		if !includeStore(storeName) {
			continue
		}
		if storeName == keys.EVMStoreKey {
			addAccount := make(map[string]struct{})
			addCode := make(map[string]struct{})
			for k := range storeMap {
				kind, stripped := keys.ParseEVMKey([]byte(k))
				switch kind {
				case keys.EVMKeyNonce, keys.EVMKeyCodeHash:
					addAccount[string(stripped)] = struct{}{}
				case keys.EVMKeyCode:
					addCode[string(stripped)] = struct{}{}
				case keys.EVMKeyStorage:
					shape.storageRows++
				case keys.EVMKeyLegacy:
					shape.legacyRows++
				}
			}
			shape.accountRows += int64(len(addAccount))
			shape.codeRows += int64(len(addCode))
			continue
		}
		shape.legacyRows += int64(len(storeMap))
	}
	return shape
}

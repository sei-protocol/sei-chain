package flatkv

import (
	"bytes"
	"flag"
	"hash/fnv"
	"math/rand"
	"os"
	"sort"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

// evmIterSeed overrides the deterministic seed for EVM iterator tests when non-zero.
var evmIterSeed = flag.Int64("evm-iter-seed", 0, "seed for EVM iterator fixture generation (0 = derive from test name)")

func TestMain(m *testing.M) {
	flag.Parse()
	os.Exit(m.Run())
}

type evmIteratorEntry struct {
	Key   []byte
	Value []byte
}

type evmIteratorDisposition int

const (
	dispositionPebbleOnly evmIteratorDisposition = iota
	dispositionPendingOnly
	dispositionOverlap
	dispositionTombstone
)

type evmIteratorFixture struct {
	Seed           int64
	Store          *CommitStore
	Sorted         []evmIteratorEntry
	OverlapSamples []evmIteratorEntry
	TombstonedKeys [][]byte
}

func TestEvmIterator(t *testing.T) {
	seed := iteratorTestSeed(t, "TestEvmIterator")
	fixture := buildEvmIteratorFixture(t, seed)

	baseline, err := sumFlatKVTableIters(fixture.Store)
	require.NoError(t, err)
	t.Cleanup(func() {
		current, sumErr := sumFlatKVTableIters(fixture.Store)
		require.NoError(t, sumErr)
		require.Equal(t, baseline, current, "leaked pebble table iterators")
		require.NoError(t, fixture.Store.Close())
	})

	storageStart := keys.StateKeyPrefix()
	storageEnd := ktype.PrefixEnd(storageStart)
	codeStart := []byte{0x07}
	codeEnd := ktype.PrefixEnd(codeStart)
	legacyStart := []byte{0x09}
	legacyEnd := ktype.PrefixEnd(legacyStart)

	cases := []struct {
		name      string
		start     []byte
		end       []byte
		ascending bool
	}{
		{name: "full module ascending", ascending: true},
		{name: "full module descending", ascending: false},
		{name: "storage prefix range", start: storageStart, end: storageEnd, ascending: true},
		{name: "legacy sub-range", start: legacyStart, end: legacyEnd, ascending: true},
		{name: "code prefix range", start: codeStart, end: codeEnd, ascending: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := subrange(fixture.Sorted, tc.start, tc.end, tc.ascending)
			iter, err := fixture.Store.Iterator(keys.EVMStoreKey, tc.start, tc.end, tc.ascending)
			require.NoError(t, err)
			got := collectIterEntries(t, iter)
			require.NoError(t, iter.Close())
			require.Equal(t, want, got)
		})
	}

	t.Run("overlap keys use pending values", func(t *testing.T) {
		require.NotEmpty(t, fixture.OverlapSamples)

		iter, err := fixture.Store.Iterator(keys.EVMStoreKey, nil, nil, true)
		require.NoError(t, err)
		defer func() { require.NoError(t, iter.Close()) }()

		got := collectIterEntries(t, iter)
		for _, sample := range fixture.OverlapSamples {
			entry, ok := findEntry(got, sample.Key)
			require.True(t, ok, "overlap key %x missing from iterator (seed=%d)", sample.Key, fixture.Seed)
			require.Equal(t, sample.Value, entry.Value, "overlap key %x (seed=%d)", sample.Key, fixture.Seed)
		}
	})

	t.Run("tombstones absent", func(t *testing.T) {
		require.NotEmpty(t, fixture.TombstonedKeys)

		iter, err := fixture.Store.Iterator(keys.EVMStoreKey, nil, nil, true)
		require.NoError(t, err)
		got := collectIterEntries(t, iter)
		require.NoError(t, iter.Close())

		for _, key := range fixture.TombstonedKeys {
			for _, entry := range got {
				require.False(t, bytes.Equal(entry.Key, key),
					"tombstoned key %x should not appear (seed=%d)", key, fixture.Seed)
			}
			for _, entry := range fixture.Sorted {
				require.False(t, bytes.Equal(entry.Key, key),
					"tombstoned key %x should not be in expected sorted output (seed=%d)", key, fixture.Seed)
			}
		}
	})
}

func TestLegacyIteratorNonEVM(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		bankNamedCS(&proto.KVPair{Key: []byte("alpha"), Value: []byte("A")}),
	}))
	commitAndCheck(t, s)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		bankNamedCS(
			&proto.KVPair{Key: []byte("beta"), Value: []byte("B")},
			&proto.KVPair{Key: []byte("alpha"), Value: []byte("A2")},
		),
	}))

	iter, err := s.Iterator("bank", nil, nil, true)
	require.NoError(t, err)
	got := collectIterEntries(t, iter)
	require.NoError(t, iter.Close())

	require.Equal(t, []evmIteratorEntry{
		{Key: []byte("alpha"), Value: []byte("A2")},
		{Key: []byte("beta"), Value: []byte("B")},
	}, got)
}

func iteratorTestSeed(t *testing.T, label string) int64 {
	t.Helper()
	if *evmIterSeed != 0 {
		t.Logf("evm iterator seed=%d (from -evm-iter-seed)", *evmIterSeed)
		return *evmIterSeed
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(t.Name()))
	if label != "" {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(label))
	}
	seed := int64(h.Sum64()) //nolint:gosec // deterministic test data only
	t.Logf("evm iterator seed=%d (reproduce with -evm-iter-seed=%d)", seed, seed)
	return seed
}

func buildEvmIteratorFixture(t *testing.T, seed int64) *evmIteratorFixture {
	t.Helper()
	t.Logf("building EVM iterator fixture with seed=%d", seed)

	s := setupTestStore(t)
	rng := rand.New(rand.NewSource(seed)) //nolint:gosec // deterministic test data only

	latest := make(map[string]evmIteratorEntry)
	var batch1, batch2 []*proto.KVPair
	var overlapSamples []evmIteratorEntry
	var tombstonedKeys [][]byte
	usedAddrs := make(map[byte]struct{}, 32)

	gen := &evmIteratorGenerator{
		rng:        rng,
		latest:     latest,
		batch1:     &batch1,
		batch2:     &batch2,
		overlaps:   &overlapSamples,
		tombstones: &tombstonedKeys,
		usedAddrs:  usedAddrs,
	}

	for _, disp := range []evmIteratorDisposition{
		dispositionPebbleOnly,
		dispositionPendingOnly,
		dispositionOverlap,
		dispositionTombstone,
	} {
		gen.addStorage(disp)
		gen.addCode(disp)
		gen.addLegacy(disp)
		gen.addAccount(disp)
	}

	// Nonce-only account (no codehash key in iterator output).
	gen.addNonceOnlyAccount()

	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{namedCS(batch1...)}))
	commitAndCheck(t, s)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{namedCS(batch2...)}))

	return &evmIteratorFixture{
		Seed:           seed,
		Store:          s,
		Sorted:         sortedEvmEntries(latest),
		OverlapSamples: overlapSamples,
		TombstonedKeys: tombstonedKeys,
	}
}

type evmIteratorGenerator struct {
	rng        *rand.Rand
	latest     map[string]evmIteratorEntry
	batch1     *[]*proto.KVPair
	batch2     *[]*proto.KVPair
	overlaps   *[]evmIteratorEntry
	tombstones *[][]byte
	usedAddrs  map[byte]struct{}
}

func (g *evmIteratorGenerator) uniqueAddr() ktype.Address {
	for attempts := 0; attempts < 512; attempts++ {
		b := byte(g.rng.Intn(256))
		if _, used := g.usedAddrs[b]; used {
			continue
		}
		g.usedAddrs[b] = struct{}{}
		return addrN(b)
	}
	panic("failed to allocate unique test address")
}

func (g *evmIteratorGenerator) uniqueSlot() ktype.Slot {
	var s ktype.Slot
	g.rng.Read(s[:])
	if s == (ktype.Slot{}) {
		s[31] = 1
	}
	return s
}

func (g *evmIteratorGenerator) storageVal() []byte {
	return padLeft32(g.rngByte())
}

func (g *evmIteratorGenerator) codeVal() []byte {
	n := 1 + g.rng.Intn(16)
	out := make([]byte, n)
	g.rng.Read(out)
	return out
}

func (g *evmIteratorGenerator) legacyVal() []byte {
	n := 1 + g.rng.Intn(32)
	out := make([]byte, n)
	g.rng.Read(out)
	return out
}

func (g *evmIteratorGenerator) rngByte() byte {
	return byte(g.rng.Intn(256)) //nolint:gosec
}

func (g *evmIteratorGenerator) rngNonce() uint64 {
	return g.rng.Uint64()
}

func (g *evmIteratorGenerator) rngCodeHash() vtype.CodeHash {
	var h vtype.CodeHash
	g.rng.Read(h[:])
	if h == (vtype.CodeHash{}) {
		h[0] = 1
	}
	return h
}

func (g *evmIteratorGenerator) recordOverlap(key, value []byte) {
	*g.overlaps = append(*g.overlaps, evmIteratorEntry{
		Key:   bytes.Clone(key),
		Value: bytes.Clone(value),
	})
}

func (g *evmIteratorGenerator) recordTombstone(key []byte) {
	*g.tombstones = append(*g.tombstones, bytes.Clone(key))
}

func (g *evmIteratorGenerator) addStorage(disp evmIteratorDisposition) {
	addr := g.uniqueAddr()
	slot := g.uniqueSlot()
	v1 := g.storageVal()
	v2 := g.storageVal()
	for bytes.Equal(v1, v2) {
		v2 = g.storageVal()
	}

	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))
	switch disp {
	case dispositionPebbleOnly:
		*g.batch1 = append(*g.batch1, storagePair(addr, slot, v1))
		recordStorageLatest(g.latest, addr, slot, v1)
	case dispositionPendingOnly:
		*g.batch2 = append(*g.batch2, storagePair(addr, slot, v2))
		recordStorageLatest(g.latest, addr, slot, v2)
	case dispositionOverlap:
		*g.batch1 = append(*g.batch1, storagePair(addr, slot, v1))
		*g.batch2 = append(*g.batch2, storagePair(addr, slot, v2))
		recordStorageLatest(g.latest, addr, slot, v2)
		g.recordOverlap(key, padLeft32(v2...))
	case dispositionTombstone:
		*g.batch1 = append(*g.batch1, storagePair(addr, slot, v1))
		*g.batch2 = append(*g.batch2, storageDeletePair(addr, slot))
		removeStorageLatest(g.latest, addr, slot)
		g.recordTombstone(key)
	}
}

func (g *evmIteratorGenerator) addCode(disp evmIteratorDisposition) {
	addr := g.uniqueAddr()
	v1 := g.codeVal()
	v2 := g.codeVal()
	for bytes.Equal(v1, v2) {
		v2 = g.codeVal()
	}

	key := keys.BuildEVMKey(keys.EVMKeyCode, addr[:])
	switch disp {
	case dispositionPebbleOnly:
		*g.batch1 = append(*g.batch1, codePair(addr, v1))
		recordCodeLatest(g.latest, addr, v1)
	case dispositionPendingOnly:
		*g.batch2 = append(*g.batch2, codePair(addr, v2))
		recordCodeLatest(g.latest, addr, v2)
	case dispositionOverlap:
		*g.batch1 = append(*g.batch1, codePair(addr, v1))
		*g.batch2 = append(*g.batch2, codePair(addr, v2))
		recordCodeLatest(g.latest, addr, v2)
		g.recordOverlap(key, bytes.Clone(v2))
	case dispositionTombstone:
		*g.batch1 = append(*g.batch1, codePair(addr, v1))
		*g.batch2 = append(*g.batch2, codeDeletePair(addr))
		removeCodeLatest(g.latest, addr)
		g.recordTombstone(key)
	}
}

func (g *evmIteratorGenerator) addLegacy(disp evmIteratorDisposition) {
	addr := g.uniqueAddr()
	v1 := g.legacyVal()
	v2 := g.legacyVal()
	for bytes.Equal(v1, v2) {
		v2 = g.legacyVal()
	}

	key := append([]byte{0x09}, addr[:]...)
	switch disp {
	case dispositionPebbleOnly:
		*g.batch1 = append(*g.batch1, legacyPair(addr, v1))
		recordLegacyLatest(g.latest, addr, v1)
	case dispositionPendingOnly:
		*g.batch2 = append(*g.batch2, legacyPair(addr, v2))
		recordLegacyLatest(g.latest, addr, v2)
	case dispositionOverlap:
		*g.batch1 = append(*g.batch1, legacyPair(addr, v1))
		*g.batch2 = append(*g.batch2, legacyPair(addr, v2))
		recordLegacyLatest(g.latest, addr, v2)
		g.recordOverlap(key, bytes.Clone(v2))
	case dispositionTombstone:
		*g.batch1 = append(*g.batch1, legacyPair(addr, v1))
		*g.batch2 = append(*g.batch2, legacyDeletePair(addr))
		removeLegacyLatest(g.latest, addr)
		g.recordTombstone(key)
	}
}

func (g *evmIteratorGenerator) addAccount(disp evmIteratorDisposition) {
	addr := g.uniqueAddr()
	n1 := g.rngNonce()
	n2 := g.rngNonce()
	for n1 == n2 {
		n2 = g.rngNonce()
	}
	ch1 := g.rngCodeHash()
	ch2 := g.rngCodeHash()
	for ch1 == ch2 {
		ch2 = g.rngCodeHash()
	}

	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
	codeHashKey := keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:])

	switch disp {
	case dispositionPebbleOnly:
		*g.batch1 = append(*g.batch1, noncePair(addr, n1), codeHashPair(addr, ch1))
		recordNonceLatest(g.latest, addr, n1)
		recordCodeHashLatest(g.latest, addr, ch1)
	case dispositionPendingOnly:
		*g.batch2 = append(*g.batch2, noncePair(addr, n2), codeHashPair(addr, ch2))
		recordNonceLatest(g.latest, addr, n2)
		recordCodeHashLatest(g.latest, addr, ch2)
	case dispositionOverlap:
		*g.batch1 = append(*g.batch1, noncePair(addr, n1), codeHashPair(addr, ch1))
		*g.batch2 = append(*g.batch2, noncePair(addr, n2), codeHashPair(addr, ch2))
		recordNonceLatest(g.latest, addr, n2)
		recordCodeHashLatest(g.latest, addr, ch2)
		g.recordOverlap(nonceKey, nonceBytes(n2))
		g.recordOverlap(codeHashKey, ch2[:])
	case dispositionTombstone:
		*g.batch1 = append(*g.batch1, noncePair(addr, n1), codeHashPair(addr, ch1))
		*g.batch2 = append(*g.batch2, nonceDeletePair(addr), codeHashDeletePair(addr))
		removeAccountLatest(g.latest, addr)
		g.recordTombstone(nonceKey)
		g.recordTombstone(codeHashKey)
	}
}

func (g *evmIteratorGenerator) addNonceOnlyAccount() {
	addr := g.uniqueAddr()
	n := g.rngNonce()
	*g.batch1 = append(*g.batch1, noncePair(addr, n))
	recordNonceLatest(g.latest, addr, n)
}

func bankNamedCS(pairs ...*proto.KVPair) *proto.NamedChangeSet {
	return &proto.NamedChangeSet{
		Name:      "bank",
		Changeset: proto.ChangeSet{Pairs: pairs},
	}
}

func legacyPair(addr ktype.Address, val []byte) *proto.KVPair {
	return &proto.KVPair{
		Key:   append([]byte{0x09}, addr[:]...),
		Value: val,
	}
}

func legacyDeletePair(addr ktype.Address) *proto.KVPair {
	return &proto.KVPair{
		Key:    append([]byte{0x09}, addr[:]...),
		Delete: true,
	}
}

func setEvmLatest(latest map[string]evmIteratorEntry, key, value []byte) {
	latest[string(key)] = evmIteratorEntry{
		Key:   bytes.Clone(key),
		Value: bytes.Clone(value),
	}
}

func removeEvmLatest(latest map[string]evmIteratorEntry, key []byte) {
	delete(latest, string(key))
}

func recordStorageLatest(latest map[string]evmIteratorEntry, addr ktype.Address, slot ktype.Slot, val []byte) {
	key := keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))
	setEvmLatest(latest, key, padLeft32(val...))
}

func removeStorageLatest(latest map[string]evmIteratorEntry, addr ktype.Address, slot ktype.Slot) {
	removeEvmLatest(latest, keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)))
}

func recordCodeLatest(latest map[string]evmIteratorEntry, addr ktype.Address, bytecode []byte) {
	setEvmLatest(latest, keys.BuildEVMKey(keys.EVMKeyCode, addr[:]), bytes.Clone(bytecode))
}

func removeCodeLatest(latest map[string]evmIteratorEntry, addr ktype.Address) {
	removeEvmLatest(latest, keys.BuildEVMKey(keys.EVMKeyCode, addr[:]))
}

func recordLegacyLatest(latest map[string]evmIteratorEntry, addr ktype.Address, val []byte) {
	key := append([]byte{0x09}, addr[:]...)
	setEvmLatest(latest, key, bytes.Clone(val))
}

func removeLegacyLatest(latest map[string]evmIteratorEntry, addr ktype.Address) {
	removeEvmLatest(latest, append([]byte{0x09}, addr[:]...))
}

func recordNonceLatest(latest map[string]evmIteratorEntry, addr ktype.Address, nonce uint64) {
	setEvmLatest(latest, keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]), nonceBytes(nonce))
}

func recordCodeHashLatest(latest map[string]evmIteratorEntry, addr ktype.Address, ch vtype.CodeHash) {
	key := keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:])
	var zero vtype.CodeHash
	if ch == zero {
		removeEvmLatest(latest, key)
		return
	}
	setEvmLatest(latest, key, ch[:])
}

func removeAccountLatest(latest map[string]evmIteratorEntry, addr ktype.Address) {
	removeEvmLatest(latest, keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]))
	removeEvmLatest(latest, keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:]))
}

func sortedEvmEntries(latest map[string]evmIteratorEntry) []evmIteratorEntry {
	out := make([]evmIteratorEntry, 0, len(latest))
	for _, e := range latest {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		return bytes.Compare(out[i].Key, out[j].Key) < 0
	})
	return out
}

func findEntry(entries []evmIteratorEntry, key []byte) (evmIteratorEntry, bool) {
	for _, e := range entries {
		if bytes.Equal(e.Key, key) {
			return e, true
		}
	}
	return evmIteratorEntry{}, false
}

func subrange(sorted []evmIteratorEntry, start, end []byte, ascending bool) []evmIteratorEntry {
	out := make([]evmIteratorEntry, 0, len(sorted))
	for _, e := range sorted {
		if start != nil && bytes.Compare(e.Key, start) < 0 {
			continue
		}
		if end != nil && bytes.Compare(e.Key, end) >= 0 {
			continue
		}
		out = append(out, e)
	}
	if !ascending {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	return out
}

func collectIterEntries(t *testing.T, iter dbm.Iterator) []evmIteratorEntry {
	t.Helper()
	var out []evmIteratorEntry
	for ; iter.Valid(); iter.Next() {
		out = append(out, evmIteratorEntry{
			Key:   bytes.Clone(iter.Key()),
			Value: bytes.Clone(iter.Value()),
		})
	}
	require.NoError(t, iter.Error())
	return out
}

func sumFlatKVTableIters(s *CommitStore) (int64, error) {
	var sum int64
	for _, db := range s.dataDBs() {
		n, err := pebbledb.TableIters(db)
		if err != nil {
			return 0, err
		}
		sum += n
	}
	return sum, nil
}

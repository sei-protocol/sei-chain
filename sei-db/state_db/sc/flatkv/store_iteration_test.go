package flatkv

import (
	"bytes"
	"flag"
	"fmt"
	"hash/fnv"
	"math/rand"
	"os"
	"sort"
	"sync"
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
	codeHashStart := []byte{0x08}
	codeHashEnd := ktype.PrefixEnd(codeHashStart)
	miscStart := []byte{0x09}
	miscEnd := ktype.PrefixEnd(miscStart)
	nonceStart := []byte{0x0a}
	nonceEnd := ktype.PrefixEnd(nonceStart)
	midAddr := addrN(0x80)
	crossSpanStart := keys.BuildEVMKey(keys.EVMKeyCodeHash, midAddr[:]) // 0x08 || addr
	crossSpanEnd := keys.BuildEVMKey(keys.EVMKeyNonce, midAddr[:])      // 0x0a || addr
	storageResumeStart := evmStorageKey(addrN(0x40), slotN(0x10))       // 0x03 || addr || slot

	cases := []struct {
		name      string
		start     []byte
		end       []byte
		ascending bool
	}{
		{name: "full module ascending", ascending: true},
		{name: "full module descending", ascending: false},
		{name: "storage prefix range", start: storageStart, end: storageEnd, ascending: true},
		{name: "misc sub-range", start: miscStart, end: miscEnd, ascending: true},
		{name: "code prefix range", start: codeStart, end: codeEnd, ascending: true},
		{name: "codehash prefix range ascending", start: codeHashStart, end: codeHashEnd, ascending: true},
		{name: "codehash prefix range descending", start: codeHashStart, end: codeHashEnd, ascending: false},
		{name: "nonce prefix range ascending", start: nonceStart, end: nonceEnd, ascending: true},
		{name: "nonce prefix range descending", start: nonceStart, end: nonceEnd, ascending: false},
		{name: "cross span codehash to nonce ascending", start: crossSpanStart, end: crossSpanEnd, ascending: true},
		{name: "cross span codehash to nonce descending", start: crossSpanStart, end: crossSpanEnd, ascending: false},
		{name: "storage resume ascending", start: storageResumeStart, end: nil, ascending: true},
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

func TestMiscIteratorNonEVM(t *testing.T) {
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

// TestEvmIteratorDomain verifies that Iterator reports the caller's logical
// [start, end) from Domain() (M3), not the underlying physical Pebble bounds.
func TestEvmIteratorDomain(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	t.Run("evm bounded", func(t *testing.T) {
		start := []byte{0x07}
		end := []byte{0x09}
		iter, err := s.Iterator(keys.EVMStoreKey, start, end, true)
		require.NoError(t, err)
		defer func() { require.NoError(t, iter.Close()) }()

		gotStart, gotEnd := iter.Domain()
		require.Equal(t, start, gotStart)
		require.Equal(t, end, gotEnd)
	})

	t.Run("evm unbounded", func(t *testing.T) {
		iter, err := s.Iterator(keys.EVMStoreKey, nil, nil, true)
		require.NoError(t, err)
		defer func() { require.NoError(t, iter.Close()) }()

		gotStart, gotEnd := iter.Domain()
		require.Nil(t, gotStart)
		require.Nil(t, gotEnd)
	})

	t.Run("non-evm bounded", func(t *testing.T) {
		start := []byte("a")
		end := []byte("z")
		iter, err := s.Iterator("bank", start, end, true)
		require.NoError(t, err)
		defer func() { require.NoError(t, iter.Close()) }()

		gotStart, gotEnd := iter.Domain()
		require.Equal(t, start, gotStart)
		require.Equal(t, end, gotEnd)
	})
}

// TestEvmIteratorSnapshotConcurrentWithWrites exercises the RWMutex (M2):
// iterators are stable snapshots that can be built and drained concurrently
// with ApplyChangeSets/Commit, and a snapshot opened before writes is unaffected
// by later commits. Run with -race to detect unsynchronized access to the
// pending-writes maps.
func TestEvmIteratorSnapshotConcurrentWithWrites(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	base := addrN(0x01)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{namedCS(
		noncePair(base, 7),
		codePair(base, []byte{0xaa}),
		storagePair(base, slotN(0x01), []byte{0xbb}),
	)}))
	commitAndCheck(t, s)

	// Expected committed-only state, captured before any concurrent writes.
	wantIter, err := s.Iterator(keys.EVMStoreKey, nil, nil, true)
	require.NoError(t, err)
	want := collectIterEntries(t, wantIter)
	require.NoError(t, wantIter.Close())

	// A snapshot opened before the writer starts must keep returning `want`
	// regardless of the commits that follow.
	snap, err := s.Iterator(keys.EVMStoreKey, nil, nil, true)
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			a := addrN(byte(0x20 + i))
			if applyErr := s.ApplyChangeSets([]*proto.NamedChangeSet{namedCS(
				noncePair(a, uint64(i+1)),
			)}); applyErr != nil {
				t.Errorf("ApplyChangeSets: %v", applyErr)
				return
			}
			if _, commitErr := s.Commit(); commitErr != nil {
				t.Errorf("Commit: %v", commitErr)
				return
			}
		}
	}()

	// Concurrently build and drain fresh iterators (RLock) while the writer
	// holds the write lock, to stress the lock under -race.
	for i := 0; i < 50; i++ {
		it, iterErr := s.Iterator(keys.EVMStoreKey, nil, nil, true)
		require.NoError(t, iterErr)
		_ = collectIterEntries(t, it)
		require.NoError(t, it.Close())
	}

	wg.Wait()

	got := collectIterEntries(t, snap)
	require.NoError(t, snap.Close())
	require.Equal(t, want, got, "pre-write snapshot must be unaffected by concurrent commits")
}

// TestEvmLaneBounds exercises every branch of evmLaneBounds in
// isolation, for an aligned lane (logical == physical byte) and the misaligned
// codehash lane (logical 0x08 -> physical account byte 0x0a).
func TestEvmLaneBounds(t *testing.T) {
	phys := func(b byte, suffix ...byte) []byte {
		return append([]byte{'e', 'v', 'm', '/', b}, suffix...)
	}

	cases := []struct {
		name      string
		start     []byte
		end       []byte
		logical   byte
		physical  byte
		wantEmpty bool
		wantLower []byte
		wantUpper []byte
	}{
		// Aligned lane (nonce: logical 0x0a, physical 0x0a).
		{
			name:      "aligned within span",
			start:     []byte{0x0a, 0x01},
			end:       []byte{0x0a, 0x02},
			logical:   0x0a,
			physical:  0x0a,
			wantLower: phys(0x0a, 0x01),
			wantUpper: phys(0x0a, 0x02),
		},
		{
			name:      "aligned low clamp nil start",
			start:     nil,
			end:       []byte{0x0a, 0x02},
			logical:   0x0a,
			physical:  0x0a,
			wantLower: phys(0x0a),
			wantUpper: phys(0x0a, 0x02),
		},
		{
			name:      "aligned high clamp nil end",
			start:     []byte{0x0a, 0x01},
			end:       nil,
			logical:   0x0a,
			physical:  0x0a,
			wantLower: phys(0x0a, 0x01),
			wantUpper: phys(0x0b),
		},
		{
			name:      "aligned full range",
			start:     nil,
			end:       nil,
			logical:   0x0a,
			physical:  0x0a,
			wantLower: phys(0x0a),
			wantUpper: phys(0x0b),
		},
		{
			name:      "aligned start below span",
			start:     []byte{0x05},
			end:       nil,
			logical:   0x0a,
			physical:  0x0a,
			wantLower: phys(0x0a),
			wantUpper: phys(0x0b),
		},
		{
			name:      "aligned end above span",
			start:     nil,
			end:       []byte{0x20},
			logical:   0x0a,
			physical:  0x0a,
			wantLower: phys(0x0a),
			wantUpper: phys(0x0b),
		},
		{
			name:      "aligned exact bare endpoints",
			start:     []byte{0x0a},
			end:       []byte{0x0b},
			logical:   0x0a,
			physical:  0x0a,
			wantLower: phys(0x0a),
			wantUpper: phys(0x0b),
		},
		{
			name:      "aligned disjoint below",
			start:     nil,
			end:       []byte{0x09},
			logical:   0x0a,
			physical:  0x0a,
			wantEmpty: true,
		},
		{
			name:      "aligned disjoint above",
			start:     []byte{0x0b},
			end:       nil,
			logical:   0x0a,
			physical:  0x0a,
			wantEmpty: true,
		},
		{
			name:      "aligned single key empty",
			start:     []byte{0x0a, 0x01},
			end:       []byte{0x0a, 0x01},
			logical:   0x0a,
			physical:  0x0a,
			wantEmpty: true,
		},

		// Misaligned codehash lane (logical 0x08, physical 0x0a).
		{
			name:      "codehash within span swaps prefix",
			start:     []byte{0x08, 0x01},
			end:       []byte{0x08, 0x02},
			logical:   0x08,
			physical:  0x0a,
			wantLower: phys(0x0a, 0x01),
			wantUpper: phys(0x0a, 0x02),
		},
		{
			name:      "codehash full prefix maps to account region",
			start:     []byte{0x08},
			end:       []byte{0x09},
			logical:   0x08,
			physical:  0x0a,
			wantLower: phys(0x0a),
			wantUpper: phys(0x0b),
		},
		{
			name:      "codehash nil maps to account region",
			start:     nil,
			end:       nil,
			logical:   0x08,
			physical:  0x0a,
			wantLower: phys(0x0a),
			wantUpper: phys(0x0b),
		},
		{
			name:      "codehash disjoint from nonce query",
			start:     []byte{0x0a},
			end:       []byte{0x0b},
			logical:   0x08,
			physical:  0x0a,
			wantEmpty: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lower, upper, empty := evmLaneBounds(tc.start, tc.end, tc.logical, tc.physical)
			require.Equal(t, tc.wantEmpty, empty)
			if tc.wantEmpty {
				return
			}
			require.Equal(t, tc.wantLower, lower, "lower")
			require.Equal(t, tc.wantUpper, upper, "upper")
		})
	}
}

// TestEvmIteratorDifferential compares the EVM iterator against the
// independent subrange oracle across many randomized boundary-relevant ranges
// and both directions. Because the oracle is implementation-independent and the
// comparison is over full ordered slices, this catches out-of-range emission,
// missing keys, mis-ordering, inclusive/exclusive boundary errors, and
// direction bugs.
func TestEvmIteratorDifferential(t *testing.T) {
	seed := iteratorTestSeed(t, "TestEvmIteratorDifferential")
	fixture := buildEvmIteratorFixture(t, seed)
	defer func() { require.NoError(t, fixture.Store.Close()) }()

	rng := rand.New(rand.NewSource(seed ^ 0x5deece66d)) //nolint:gosec // deterministic test data only

	// Pool of boundary-relevant keys: every committed/pending key, plus each
	// type prefix and its prefix-end.
	var pool [][]byte
	for _, e := range fixture.Sorted {
		pool = append(pool, bytes.Clone(e.Key))
	}
	for _, p := range [][]byte{{0x03}, {0x07}, {0x08}, {0x09}, {0x0a}} {
		pool = append(pool, bytes.Clone(p), ktype.PrefixEnd(p))
	}

	pick := func() []byte {
		switch rng.Intn(6) {
		case 0:
			return nil
		case 1:
			return bytes.Clone(pool[rng.Intn(len(pool))])
		case 2:
			return decrementKey(pool[rng.Intn(len(pool))])
		case 3:
			return incrementKey(pool[rng.Intn(len(pool))])
		default:
			n := 1 + rng.Intn(21)
			b := make([]byte, n)
			rng.Read(b)
			return b
		}
	}

	const iterations = 400
	for i := 0; i < iterations; i++ {
		start := pick()
		end := pick()
		ascending := rng.Intn(2) == 0

		want := subrange(fixture.Sorted, start, end, ascending)
		iter, err := fixture.Store.Iterator(keys.EVMStoreKey, start, end, ascending)
		require.NoError(t, err)
		got := collectIterEntries(t, iter)
		require.NoError(t, iter.Close())
		msg := fmt.Sprintf("iter %d start=%x end=%x ascending=%v seed=%d", i, start, end, ascending, fixture.Seed)
		if len(want) == 0 {
			require.Empty(t, got, msg)
		} else {
			require.Equal(t, want, got, msg)
		}
	}
}

// TestEvmIteratorEmptyAndDegenerate covers an empty store and
// degenerate ranges (equal bounds, inverted bounds) on a populated store.
func TestEvmIteratorEmptyAndDegenerate(t *testing.T) {
	t.Run("empty store", func(t *testing.T) {
		s := setupTestStore(t)
		defer s.Close()
		iter, err := s.Iterator(keys.EVMStoreKey, nil, nil, true)
		require.NoError(t, err)
		require.Empty(t, collectIterEntries(t, iter))
		require.NoError(t, iter.Close())
	})

	seed := iteratorTestSeed(t, "TestEvmIteratorEmptyAndDegenerate")
	fixture := buildEvmIteratorFixture(t, seed)
	defer func() { require.NoError(t, fixture.Store.Close()) }()
	require.NotEmpty(t, fixture.Sorted)

	lo := fixture.Sorted[0].Key
	hi := fixture.Sorted[len(fixture.Sorted)-1].Key

	t.Run("equal bounds", func(t *testing.T) {
		iter, err := fixture.Store.Iterator(keys.EVMStoreKey, lo, lo, true)
		require.NoError(t, err)
		require.Empty(t, collectIterEntries(t, iter))
		require.NoError(t, iter.Close())
	})

	t.Run("inverted bounds", func(t *testing.T) {
		iter, err := fixture.Store.Iterator(keys.EVMStoreKey, hi, lo, true)
		require.NoError(t, err)
		require.Empty(t, collectIterEntries(t, iter))
		require.NoError(t, iter.Close())
	})
}

// decrementKey returns the largest key strictly less than k (for boundary
// fuzzing). incrementKey returns the smallest key strictly greater than k.
func decrementKey(k []byte) []byte {
	if len(k) == 0 {
		return nil
	}
	out := bytes.Clone(k)
	if out[len(out)-1] > 0 {
		out[len(out)-1]--
		return out
	}
	return out[:len(out)-1]
}

func incrementKey(k []byte) []byte {
	return append(bytes.Clone(k), 0x00)
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
	usedAddrs := make(map[string]struct{}, 32)

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
		gen.addMisc(disp)
		gen.addAccount(disp)
	}

	// Nonce-only account (no codehash key in iterator output).
	gen.addNonceOnlyAccount()

	// Malformed account-prefixed misc key: lands in the account physical
	// region (evm/0x0a...) but is routed to miscDB, exercising the overlap
	// between the misc lane and the account-derived lanes.
	gen.addMalformedAccountMiscKey()

	// Malformed storage/code-prefixed misc keys: correct type byte but wrong
	// length, so they route to miscDB and physically live in the storage
	// (evm/0x03...) and code (evm/0x07...) keyspaces. Confirms the misc lane
	// interleaves with the storage and code lanes and that the merge does not
	// falsely dedup a misc key against an optimized-lane key of a different
	// length.
	gen.addMalformedStoragePrefixMiscKey()
	gen.addMalformedCodePrefixMiscKey()

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
	usedAddrs  map[string]struct{}
}

func (g *evmIteratorGenerator) uniqueAddr() ktype.Address {
	for attempts := 0; attempts < 512; attempts++ {
		var a ktype.Address
		g.rng.Read(a[:])
		if _, used := g.usedAddrs[string(a[:])]; used {
			continue
		}
		g.usedAddrs[string(a[:])] = struct{}{}
		return a
	}
	panic("failed to allocate unique test address")
}

// addMalformedAccountMiscKey writes a 0x0a-prefixed key whose length does not
// match a well-formed nonce key, so it routes to miscDB while physically
// living in the account keyspace (evm/0x0a...).
func (g *evmIteratorGenerator) addMalformedAccountMiscKey() {
	key := append([]byte{0x0a}, bytes.Repeat([]byte{0x7f}, 19)...)
	val := []byte{0xab, 0xcd}
	*g.batch1 = append(*g.batch1, &proto.KVPair{Key: bytes.Clone(key), Value: bytes.Clone(val)})
	setEvmLatest(g.latest, key, val)
}

// addMalformedStoragePrefixMiscKey writes a 0x03-prefixed key whose length
// does not match a well-formed storage key (1 + 20 + 32), so it routes to
// miscDB while physically living in the storage keyspace (evm/0x03...). It is
// committed (batch1) so the misc and storage lanes interleave over pebble.
func (g *evmIteratorGenerator) addMalformedStoragePrefixMiscKey() {
	key := append([]byte{0x03}, bytes.Repeat([]byte{0x7f}, ktype.AddressLen)...)
	val := []byte{0x12, 0x34}
	*g.batch1 = append(*g.batch1, &proto.KVPair{Key: bytes.Clone(key), Value: bytes.Clone(val)})
	setEvmLatest(g.latest, key, val)
}

// addMalformedCodePrefixMiscKey writes a 0x07-prefixed key whose length does
// not match a well-formed code key (1 + 20), so it routes to miscDB while
// physically living in the code keyspace (evm/0x07...). It is pending-only
// (batch2) so the misc and code lanes interleave over pending writes too.
func (g *evmIteratorGenerator) addMalformedCodePrefixMiscKey() {
	key := append([]byte{0x07}, bytes.Repeat([]byte{0x5a}, ktype.AddressLen-1)...)
	val := []byte{0x56, 0x78}
	*g.batch2 = append(*g.batch2, &proto.KVPair{Key: bytes.Clone(key), Value: bytes.Clone(val)})
	setEvmLatest(g.latest, key, val)
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

func (g *evmIteratorGenerator) miscVal() []byte {
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

func (g *evmIteratorGenerator) addMisc(disp evmIteratorDisposition) {
	addr := g.uniqueAddr()
	v1 := g.miscVal()
	v2 := g.miscVal()
	for bytes.Equal(v1, v2) {
		v2 = g.miscVal()
	}

	key := append([]byte{0x09}, addr[:]...)
	switch disp {
	case dispositionPebbleOnly:
		*g.batch1 = append(*g.batch1, miscPair(addr, v1))
		recordMiscLatest(g.latest, addr, v1)
	case dispositionPendingOnly:
		*g.batch2 = append(*g.batch2, miscPair(addr, v2))
		recordMiscLatest(g.latest, addr, v2)
	case dispositionOverlap:
		*g.batch1 = append(*g.batch1, miscPair(addr, v1))
		*g.batch2 = append(*g.batch2, miscPair(addr, v2))
		recordMiscLatest(g.latest, addr, v2)
		g.recordOverlap(key, bytes.Clone(v2))
	case dispositionTombstone:
		*g.batch1 = append(*g.batch1, miscPair(addr, v1))
		*g.batch2 = append(*g.batch2, miscDeletePair(addr))
		removeMiscLatest(g.latest, addr)
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

func miscPair(addr ktype.Address, val []byte) *proto.KVPair {
	return &proto.KVPair{
		Key:   append([]byte{0x09}, addr[:]...),
		Value: val,
	}
}

func miscDeletePair(addr ktype.Address) *proto.KVPair {
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

func recordMiscLatest(latest map[string]evmIteratorEntry, addr ktype.Address, val []byte) {
	key := append([]byte{0x09}, addr[:]...)
	setEvmLatest(latest, key, bytes.Clone(val))
}

func removeMiscLatest(latest map[string]evmIteratorEntry, addr ktype.Address) {
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

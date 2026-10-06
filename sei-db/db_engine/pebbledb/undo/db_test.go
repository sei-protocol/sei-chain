package undo

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"maps"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cockroachdb/pebble/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

var (
	alice    = common.HexToAddress("0xa000000000000000000000000000000000000000")
	bob      = common.HexToAddress("0xb000000000000000000000000000000000000000")
	carol    = common.HexToAddress("0xc000000000000000000000000000000000000000")
	dave     = common.HexToAddress("0xd000000000000000000000000000000000000000")
	contract = common.HexToAddress("0x000000000000000000000000000000000000dead")
)

// account returns the address whose low bytes are n, such as 0x…dead for 0xdead.
func account(n uint64) common.Address {
	return common.BytesToAddress(binary.BigEndian.AppendUint64(nil, n))
}

func balanceKey(addr common.Address) string {
	return string(keys.BuildEVMKey(keys.EVMKeyBalance, addr[:]))
}

func nonceKey(addr common.Address) string {
	return string(keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]))
}

func codeKey(addr common.Address) string {
	return string(keys.BuildEVMKey(keys.EVMKeyCode, addr[:]))
}

// storageKey returns the key of storage slot slot of addr.
func storageKey(addr common.Address, slot uint64) string {
	return string(keys.BuildEVMKey(keys.EVMKeyStorage, append(addr.Bytes(), word(slot)...)))
}

// word returns n as a 32-byte big-endian EVM word, the encoding of balances, slots and storage values.
func word(n uint64) []byte {
	w := make([]byte, wordLen)
	binary.BigEndian.PutUint64(w[wordLen-8:], n)
	return w
}

// nonce returns n in the 8-byte encoding of an account nonce.
func nonce(n uint64) []byte {
	return binary.BigEndian.AppendUint64(nil, n)
}

// liveState is the state commit store's view of the latest block: each key's current value.
type liveState map[string][]byte

func (s liveState) Get(_ string, key []byte) ([]byte, bool) {
	value, found := s[string(key)]
	return value, found
}

func (liveState) Close() {}

// TestUndoLogByHand spells the store out: the live state after the last block, the value each block
// replaced, and what a read after each block must return.
func TestUndoLogByHand(t *testing.T) {
	db := openTestDB(t, t.TempDir(), 10) // buckets of 10 blocks: 100-109, 110-119
	defer func() { require.NoError(t, db.Close()) }()

	// The live state after block 112.
	live := liveState{
		balanceKey(alice): word(80),
		balanceKey(bob):   word(7),
		balanceKey(carol): word(3),
	}

	// What each block changed, recorded as the value the key held before the block.
	was := func(key string, value []byte) *proto.KVPair { return &proto.KVPair{Key: []byte(key), Value: value} }
	wasAbsent := func(key string) *proto.KVPair { return &proto.KVPair{Key: []byte(key), Delete: true} }
	blocks := map[int64][]*proto.KVPair{
		103: {was(balanceKey(alice), word(10))}, // alice 10 -> 15
		105: {wasAbsent(balanceKey(carol))},     // carol created with 3
		109: {was(balanceKey(alice), word(15))}, // alice 15 -> 25
		110: {was(balanceKey(dave), word(1))},   // dave deleted
		112: {was(balanceKey(alice), word(25))}, // alice 25 -> 80
	}

	require.NoError(t, db.Resume(100, live))
	for height := int64(101); height <= 112; height++ {
		// Every block is handed the final live state: only the last block's is ever read.
		db.ApplyBlock(height, blocks[height], live)
	}
	db.WaitForPendingWrites()

	var absent []byte
	for _, tc := range []struct {
		key    string
		height int64
		want   []byte
	}{
		{balanceKey(alice), 100, word(10)},
		{balanceKey(alice), 102, word(10)},
		{balanceKey(alice), 103, word(15)},
		{balanceKey(alice), 108, word(15)},
		{balanceKey(alice), 109, word(25)},
		{balanceKey(alice), 111, word(25)},
		{balanceKey(alice), 112, word(80)}, // no record above 112: the live state answers
		{balanceKey(bob), 100, word(7)},    // never changed: the live state answers at every height
		{balanceKey(bob), 112, word(7)},
		{balanceKey(carol), 104, absent},
		{balanceKey(carol), 105, word(3)},
		{balanceKey(dave), 101, word(1)}, // bucket 10 holds nothing for dave; bucket 11 does
		{balanceKey(dave), 109, word(1)},
		{balanceKey(dave), 110, absent},
	} {
		require.Equal(t, tc.want, read(t, db, tc.height, tc.key), "%x after block %d", tc.key, tc.height)
	}
}

// mapView is a LiveStateView over an immutable map, counting how often it is closed.
type mapView struct {
	values map[string][]byte
	closes *atomic.Int64
}

func (v *mapView) Get(_ string, key []byte) ([]byte, bool) {
	value, found := v.values[string(key)]
	return value, found
}

func (v *mapView) Close() { v.closes.Add(1) }

func testConfig() config.StateStoreConfig {
	cfg := config.DefaultStateStoreConfig()
	cfg.AsyncWriteBuffer = 16
	return cfg
}

func openTestDB(t testing.TB, dir string, bucketSize uint64) *Database {
	t.Helper()
	cache := pebble.NewCache(cacheSize)
	defer cache.Unref()
	storage, err := pebble.Open(dir, newPebbleOptions(cache))
	require.NoError(t, err)
	stored, err := readMarker(storage, bucketSizeKey)
	require.NoError(t, err)
	if stored == 0 {
		require.NoError(t, writeMarker(storage, bucketSizeKey, bucketSize, pebble.Sync))
	}
	db, err := newDatabase(storage, dir, testConfig())
	require.NoError(t, err)
	return db
}

// chain is a reference model: the full state after every block, and the undo store under test fed
// from it.
type chain struct {
	t       testing.TB
	db      *Database
	base    int64
	states  []map[string][]byte // states[h-base] is the state after block h
	closes  atomic.Int64
	created atomic.Int64
	mu      sync.RWMutex
}

// newChain opens a store in dir resumed at base with an empty state.
func newChain(t testing.TB, dir string, bucketSize uint64, base int64) *chain {
	c := &chain{t: t, db: openTestDB(t, dir, bucketSize), base: base}
	c.states = []map[string][]byte{{}}
	require.NoError(t, c.db.Resume(base, c.view(c.states[0])))
	return c
}

func (c *chain) view(values map[string][]byte) LiveStateView {
	c.created.Add(1)
	return &mapView{values: values, closes: &c.closes}
}

func (c *chain) head() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.base + int64(len(c.states)) - 1
}

// stateAt returns the model's value of key after block height.
func (c *chain) stateAt(height int64, key string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.states[height-c.base][key]
	return v, ok
}

// apply commits one block setting (or, for a nil value, deleting) each key, and feeds its prior
// values to the store.
func (c *chain) apply(changes map[string][]byte, async bool) {
	c.mu.Lock()
	prev := c.states[len(c.states)-1]
	version := c.base + int64(len(c.states))
	next := maps.Clone(prev)
	var prior []*proto.KVPair
	for key, value := range changes {
		old, existed := prev[key]
		prior = append(prior, &proto.KVPair{Key: []byte(key), Value: old, Delete: !existed})
		if value == nil {
			delete(next, key)
		} else {
			next[key] = value
		}
	}
	c.states = append(c.states, next)
	c.mu.Unlock()

	if async {
		c.db.ApplyBlock(version, prior, c.view(next))
		return
	}
	require.NoError(c.t, c.db.applyBlock(version, prior, c.view(next)))
}

// read returns key's value after block height through a view of the store.
func read(t testing.TB, db *Database, height int64, key string) []byte {
	t.Helper()
	view, ok := db.OpenView(height)
	require.True(t, ok, "open a view at %d", height)
	defer view.Close()
	value, err := view.Get([]byte(key))
	require.NoError(t, err, "read %x at %d", key, height)
	return value
}

func (c *chain) requireRead(height int64, key string) {
	c.t.Helper()
	want, found := c.stateAt(height, key)
	got := read(c.t, c.db, height, key)
	if !found {
		require.Nil(c.t, got, "read %x at %d", key, height)
		return
	}
	require.NotNil(c.t, got, "read %x at %d", key, height)
	require.Equal(c.t, want, got, "read %x at %d", key, height)
}

// record is one undo record as stored.
type record struct {
	bucket uint64
	key    string
	height int64
	value  []byte
}

// records returns every undo record in the store, in key order.
func records(t testing.TB, db *Database) []record {
	t.Helper()
	itr, err := db.storage.NewIter(&pebble.IterOptions{UpperBound: bucketBoundary(metadataBucket)})
	require.NoError(t, err)
	defer func() { require.NoError(t, itr.Close()) }()
	var out []record
	for itr.First(); itr.Valid(); itr.Next() {
		height, err := decodeRecordHeight(itr.Key())
		require.NoError(t, err)
		value, err := decodeValue(itr.Value())
		require.NoError(t, err)
		body := itr.Key()[:splitKey(itr.Key())]
		out = append(out, record{
			bucket: binary.BigEndian.Uint64(body),
			key:    string(body[bucketLen:]),
			height: int64(height), //nolint:gosec // test heights are small
			value:  value,
		})
	}
	require.NoError(t, itr.Error())
	return out
}

func TestReadsFollowTheSpecExample(t *testing.T) {
	c := newChain(t, t.TempDir(), 10, 100)
	defer func() { require.NoError(t, c.db.Close()) }()

	balance := balanceKey(alice)
	balances := map[int64][]byte{103: word(15), 109: word(25), 112: word(80)}
	c.apply(map[string][]byte{balance: word(10)}, false) // block 101 creates the account
	for h := int64(102); h <= 118; h++ {
		changes := map[string][]byte{}
		if v, ok := balances[h]; ok {
			changes[balance] = v
		}
		c.apply(changes, false)
	}

	for height, want := range map[int64][]byte{102: word(10), 108: word(15), 110: word(25), 118: word(80)} {
		require.Equal(t, want, read(t, c.db, height, balance), "after block %d", height)
	}
	require.Nil(t, read(t, c.db, 100, balance), "alice did not exist before block 101")

	// 101, 103 and 109 land in bucket 10 and 112 in bucket 11, each holding the value before its block.
	require.Equal(t, []record{
		{10, balance, 101, nil}, {10, balance, 103, word(10)}, {10, balance, 109, word(15)}, {11, balance, 112, word(25)},
	}, records(t, c.db))
}

func TestAbsentAndEmptyValuesStayDistinct(t *testing.T) {
	c := newChain(t, t.TempDir(), 4, 0)
	defer func() { require.NoError(t, c.db.Close()) }()

	code := codeKey(contract)
	c.apply(map[string][]byte{code: {}}, false)                 // 1: created with empty code
	c.apply(map[string][]byte{code: {0x60, 0x00, 0xf3}}, false) // 2: PUSH1 0x00 RETURN
	c.apply(map[string][]byte{code: nil}, false)                // 3: deleted
	c.apply(map[string][]byte{codeKey(account(0xbeef)): {0x00}}, false)

	for h := int64(0); h <= 4; h++ {
		c.requireRead(h, code)
	}
	got := read(t, c.db, 1, code)
	require.NotNil(t, got)
	require.Empty(t, got)
}

func TestRandomHistoryMatchesTheModel(t *testing.T) {
	for _, async := range []bool{false, true} {
		t.Run(fmt.Sprintf("async=%v", async), func(t *testing.T) {
			rng := rand.New(rand.NewPCG(7, 8))
			c := newChain(t, t.TempDir(), 7, 3)
			defer func() { require.NoError(t, c.db.Close()) }()

			slots := make([]string, 40)
			for i := range slots {
				slots[i] = storageKey(contract, uint64(i)) //nolint:gosec // small index
			}
			for range 150 {
				changes := map[string][]byte{}
				// Slots are written at very different rates, so reads cross many buckets of
				// unchanged history.
				for range rng.IntN(6) {
					slot := slots[rng.IntN(1+rng.IntN(len(slots)))]
					if rng.IntN(5) == 0 {
						changes[slot] = nil
					} else {
						changes[slot] = word(rng.Uint64N(1000))
					}
				}
				c.apply(changes, async)
			}
			c.db.WaitForPendingWrites()
			require.Equal(t, c.head(), c.db.GetLatestVersion())
			for h := c.base; h <= c.head(); h++ {
				for _, slot := range slots {
					c.requireRead(h, slot)
				}
			}

			require.NoError(t, c.db.PruneHistory(61))
			require.Equal(t, int64(61), c.db.GetEarliestVersion())
			for h := c.base; h <= c.head(); h++ {
				if h < 61 {
					_, ok := c.db.OpenView(h)
					require.False(t, ok, "height %d is pruned", h)
					continue
				}
				for _, slot := range slots {
					c.requireRead(h, slot)
				}
			}
		})
	}
}

func TestPruneExcisesOnlyExpiredBuckets(t *testing.T) {
	c := newChain(t, t.TempDir(), 10, 0)
	defer func() { require.NoError(t, c.db.Close()) }()
	// Every block moves the hot slot and creates one cold account.
	hot := storageKey(contract, 0)
	for h := uint64(1); h <= 45; h++ {
		c.apply(map[string][]byte{hot: word(h), nonceKey(account(h)): nonce(1)}, false)
	}

	// Bucket 2 spans 20-29 and holds records a read at 25 needs, so only buckets 0 and 1 go.
	require.NoError(t, c.db.PruneHistory(25))
	require.Equal(t, int64(20), records(t, c.db)[0].height)
	for h := int64(25); h <= 45; h++ {
		c.requireRead(h, hot)
		c.requireRead(h, nonceKey(account(30)))
	}

	// A prune that crosses no bucket boundary excises nothing more.
	require.NoError(t, c.db.PruneHistory(28))
	require.Equal(t, uint64(2), c.db.prunedBucket)
	require.NoError(t, c.db.PruneHistory(29))
	require.Equal(t, uint64(3), c.db.prunedBucket)

	// A cut line above the head is clamped to it.
	require.NoError(t, c.db.PruneHistory(1_000))
	require.Equal(t, int64(45), c.db.GetEarliestVersion())
	c.requireRead(45, hot)
}

func TestOpenViewsKeepTheirBucketsThroughAPrune(t *testing.T) {
	c := newChain(t, t.TempDir(), 5, 0)
	defer func() { require.NoError(t, c.db.Close()) }()
	slot := storageKey(contract, 1)
	for h := uint64(1); h <= 30; h++ {
		c.apply(map[string][]byte{slot: word(h)}, false)
	}

	view, ok := c.db.OpenView(7)
	require.True(t, ok)
	require.NoError(t, c.db.PruneHistory(20))
	_, ok = c.db.OpenView(7)
	require.False(t, ok, "no new view opens below the earliest height")
	got, err := view.Get([]byte(slot))
	require.NoError(t, err)
	require.Equal(t, word(7), got, "the open view still reads the buckets it needs")
	require.Equal(t, uint64(1), c.db.prunedBucket, "only the bucket below the view went")

	view.Close()
	require.NoError(t, c.db.PruneHistory(20))
	require.Equal(t, uint64(4), c.db.prunedBucket, "the next prune excises what the view held back")
	c.requireRead(20, slot)
}

func TestReadersNeverSeeExcisedHistory(t *testing.T) {
	c := newChain(t, t.TempDir(), 5, 0)
	defer func() { require.NoError(t, c.db.Close()) }()
	const slots = 8
	for h := uint64(1); h <= 20; h++ {
		c.apply(map[string][]byte{storageKey(contract, h%slots): word(h)}, false)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	var reads, refused atomic.Int64
	for w := range 4 {
		wg.Go(func() {
			rng := rand.New(rand.NewPCG(uint64(w), 9)) //nolint:gosec // test worker index
			for {
				select {
				case <-stop:
					return
				default:
				}
				head := c.db.GetLatestVersion()
				earliest := c.db.GetEarliestVersion()
				if head < earliest {
					continue
				}
				height := earliest + rng.Int64N(head-earliest+1)
				view, ok := c.db.OpenView(height)
				if !ok {
					refused.Add(1)
					continue
				}
				// Several reads over one view, so prunes land while it is open.
				for range 4 {
					key := storageKey(contract, rng.Uint64N(slots))
					got, err := view.Get([]byte(key))
					if err != nil {
						t.Errorf("read %x at %d: %v", key, height, err)
						view.Close()
						return
					}
					if want, _ := c.stateAt(height, key); !bytes.Equal(want, got) {
						t.Errorf("read %x at %d: got %x want %x", key, height, got, want)
						view.Close()
						return
					}
					reads.Add(1)
				}
				view.Close()
			}
		})
	}
	for h := uint64(21); h <= 400; h++ {
		c.apply(map[string][]byte{storageKey(contract, h%slots): word(h)}, true)
		if h%10 == 0 {
			require.NoError(t, c.db.PruneHistory(h-29))
		}
	}
	c.db.WaitForPendingWrites()
	close(stop)
	wg.Wait()
	require.Positive(t, reads.Load())
	t.Logf("%d reads, %d views refused as pruned", reads.Load(), refused.Load())
}

func TestViewsAreClosedOnceReplacedAndUnpinned(t *testing.T) {
	c := newChain(t, t.TempDir(), 4, 0)
	key := nonceKey(alice)
	for h := uint64(1); h <= 10; h++ {
		c.apply(map[string][]byte{key: nonce(h)}, false)
	}
	require.Equal(t, c.created.Load()-1, c.closes.Load(), "every view but the head is closed")

	pinned, ok := c.db.OpenView(5)
	require.True(t, ok)
	c.apply(map[string][]byte{key: nonce(11)}, false)
	require.Equal(t, c.created.Load()-2, c.closes.Load(), "the pinned view outlives its replacement")
	got, err := pinned.Get([]byte(key))
	require.NoError(t, err)
	require.Equal(t, nonce(5), got)
	pinned.Close()
	pinned.Close()
	require.Equal(t, c.created.Load()-1, c.closes.Load())

	require.NoError(t, c.db.Close())
	require.Equal(t, c.created.Load(), c.closes.Load())
	require.NoError(t, c.db.Close(), "Close is idempotent")
}

func TestReopenKeepsMarkersAndBucketSize(t *testing.T) {
	dir := t.TempDir()
	c := newChain(t, dir, 10, 0)
	key := balanceKey(alice)
	for h := uint64(1); h <= 35; h++ {
		c.apply(map[string][]byte{key: word(h)}, true)
	}
	c.db.WaitForPendingWrites()
	require.NoError(t, c.db.PruneHistory(22))
	require.NoError(t, c.db.Close())

	// The bucket size is fixed when the database is created.
	reopened := openTestDB(t, dir, 20)
	require.Equal(t, uint64(10), reopened.bucketSize)
	require.Equal(t, int64(35), reopened.GetLatestVersion())
	require.Equal(t, int64(22), reopened.GetEarliestVersion())
	c.db = reopened
	require.NoError(t, c.db.Resume(35, c.view(c.states[len(c.states)-1])))
	for h := int64(22); h <= 35; h++ {
		c.requireRead(h, key)
	}
	require.Equal(t, int64(20), records(t, c.db)[0].height)
	require.NoError(t, c.db.Close())
}

func TestNewDatabaseUsesDefaultBucketSize(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateStoreConfig()
	db, err := OpenDB(dir, cfg)
	require.NoError(t, err)
	require.Equal(t, uint64(defaultBucketSize), db.bucketSize)
	key := nonceKey(alice)
	require.NoError(t, db.Resume(0, liveState{}))
	db.ApplyBlock(1, []*proto.KVPair{{Key: []byte(key), Delete: true}}, liveState{key: nonce(1)})
	db.WaitForPendingWrites()
	require.Nil(t, read(t, db, 0, key))
	require.Equal(t, nonce(1), read(t, db, 1, key))
	require.NoError(t, db.Close())

	reopened, err := OpenDB(dir, cfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, reopened.Close()) }()
	require.Equal(t, uint64(defaultBucketSize), reopened.bucketSize)
	require.NoError(t, reopened.Resume(1, liveState{key: nonce(1)}))
	require.Nil(t, read(t, reopened, 0, key))
	require.Equal(t, nonce(1), read(t, reopened, 1, key))
}

func TestResumeBehindCurrentStateDropsHistory(t *testing.T) {
	dir := t.TempDir()
	c := newChain(t, dir, 10, 0)
	slot := storageKey(contract, 1)
	for h := uint64(1); h <= 12; h++ {
		c.apply(map[string][]byte{slot: word(h)}, false)
	}
	require.NoError(t, c.db.Close())

	// Current state moved on to 15 without the log: blocks 13-15 left no records.
	reopened := openTestDB(t, dir, 10)
	defer func() { require.NoError(t, reopened.Close()) }()
	var closes atomic.Int64
	require.NoError(t, reopened.Resume(15, &mapView{values: map[string][]byte{slot: word(15)}, closes: &closes}))
	require.Equal(t, int64(15), reopened.GetEarliestVersion())
	_, ok := reopened.OpenView(12)
	require.False(t, ok)
	require.Equal(t, word(15), read(t, reopened, 15, slot))
}

func TestResumeAheadOfCurrentStateIsRefused(t *testing.T) {
	dir := t.TempDir()
	c := newChain(t, dir, 10, 0)
	for h := uint64(1); h <= 12; h++ {
		c.apply(map[string][]byte{storageKey(contract, 1): word(h)}, false)
	}
	require.NoError(t, c.db.Close())

	reopened := openTestDB(t, dir, 10)
	defer func() { require.NoError(t, reopened.Close()) }()
	var closes atomic.Int64
	err := reopened.Resume(9, &mapView{closes: &closes})
	require.ErrorContains(t, err, "ahead of current state")
	require.Equal(t, int64(1), closes.Load(), "a refused view is closed")
}

func TestBlocksMustBeContiguous(t *testing.T) {
	c := newChain(t, t.TempDir(), 10, 0)
	defer func() { require.NoError(t, c.db.Close()) }()
	var closes atomic.Int64
	err := c.db.applyBlock(2, nil, &mapView{closes: &closes})
	require.ErrorContains(t, err, "does not follow")
	require.Equal(t, int64(1), closes.Load(), "a refused block's view is closed")
	_, ok := c.db.OpenView(1)
	require.False(t, ok)
}

func TestDiscardStateAboveKeepsOnlyRecordsAtOrBelowTheTarget(t *testing.T) {
	dir := t.TempDir()
	c := newChain(t, dir, 10, 0)
	// Every block moves one slot and creates one account.
	slot := storageKey(contract, 1)
	for h := uint64(1); h <= 37; h++ {
		c.apply(map[string][]byte{slot: word(h), nonceKey(account(h)): nonce(1)}, false)
	}
	require.NoError(t, c.db.Close())

	require.NoError(t, DiscardStateAbove(dir, testConfig(), 14))
	reopened := openTestDB(t, dir, 10)
	c.db = reopened
	require.Equal(t, int64(14), reopened.GetLatestVersion())
	for _, r := range records(t, reopened) {
		require.LessOrEqual(t, r.height, int64(14))
	}

	c.mu.Lock()
	c.states = c.states[:15]
	c.mu.Unlock()
	require.NoError(t, reopened.Resume(14, c.view(c.states[14])))
	c.apply(map[string][]byte{slot: word(1500)}, false) // block 15 executes again, differently
	for h := int64(0); h <= 15; h++ {
		c.requireRead(h, slot)
		c.requireRead(h, nonceKey(account(20)))
	}
	require.NoError(t, reopened.Close())

	// A log at or below the target, or no log at all, is left alone.
	require.NoError(t, DiscardStateAbove(dir, testConfig(), 100))
	require.NoError(t, DiscardStateAbove(t.TempDir()+"/absent", testConfig(), 1))
}

func TestDiscardStateAboveClearsAHalfWrittenBlock(t *testing.T) {
	// Block 10 opens bucket 1. Its records land, but the crash comes before its marker does.
	for _, target := range []int64{8, 9} {
		t.Run(fmt.Sprintf("target=%d", target), func(t *testing.T) {
			dir := t.TempDir()
			c := newChain(t, dir, 10, 0)
			slot := storageKey(contract, 1)
			for h := uint64(1); h <= 9; h++ {
				c.apply(map[string][]byte{slot: word(h)}, false)
			}
			stale := []*proto.KVPair{{Key: []byte(slot), Value: word(0xbad)}}
			require.NoError(t, c.db.writeRecords(stale, 1, 10))
			require.NoError(t, c.db.Close())

			require.NoError(t, DiscardStateAbove(dir, testConfig(), target))
			c.db = openTestDB(t, dir, 10)
			defer func() { require.NoError(t, c.db.Close()) }()
			for _, r := range records(t, c.db) {
				require.LessOrEqual(t, r.height, target)
			}

			// Execution resumes differently: block 10 no longer touches the slot.
			c.mu.Lock()
			c.states = c.states[:target+1]
			c.mu.Unlock()
			require.NoError(t, c.db.Resume(target, c.view(c.states[target])))
			for h := target + 1; h <= 11; h++ {
				changes := map[string][]byte{nonceKey(alice): nonce(uint64(h))} //nolint:gosec // positive
				if h == 9 {
					changes[slot] = word(90)
				}
				c.apply(changes, false)
			}
			for h := int64(0); h <= 11; h++ {
				c.requireRead(h, slot)
			}
		})
	}
}

func TestRollbackBelowTheEarliestHeightServesTheTarget(t *testing.T) {
	dir := t.TempDir()
	c := newChain(t, dir, 10, 0)
	slot := storageKey(contract, 1)
	for h := uint64(1); h <= 35; h++ {
		c.apply(map[string][]byte{slot: word(h)}, false)
	}
	require.NoError(t, c.db.PruneHistory(30))
	require.NoError(t, c.db.Close())

	require.NoError(t, DiscardStateAbove(dir, testConfig(), 25))
	c.db = openTestDB(t, dir, 10)
	defer func() { require.NoError(t, c.db.Close()) }()
	require.Equal(t, int64(25), c.db.GetLatestVersion())
	require.Equal(t, int64(25), c.db.GetEarliestVersion())

	c.mu.Lock()
	c.states = c.states[:26]
	c.mu.Unlock()
	require.NoError(t, c.db.Resume(25, c.view(c.states[25])))
	for h := uint64(26); h <= 30; h++ {
		c.apply(map[string][]byte{slot: word(100 + h)}, false)
	}
	for h := int64(25); h <= 30; h++ {
		c.requireRead(h, slot)
	}
	_, ok := c.db.OpenView(24)
	require.False(t, ok, "history below the target stays pruned")
}

func TestConcurrentEarliestChangesKeepDiskAndMemoryConsistent(t *testing.T) {
	for _, direction := range []string{"raise", "lower"} {
		t.Run(direction, func(t *testing.T) {
			db := openTestDB(t, t.TempDir(), 10)
			defer func() { require.NoError(t, db.Close()) }()
			for round := uint64(1); round <= 50; round++ {
				base := round * 100
				update := db.raiseEarliest
				want := base + 7
				if direction == "lower" {
					require.NoError(t, db.raiseEarliest(base+8))
					update = db.lowerEarliest
					want = base
				}
				start := make(chan struct{})
				var wg sync.WaitGroup
				for worker := range uint64(8) {
					wg.Go(func() {
						<-start
						if err := update(base + worker); err != nil {
							t.Error(err)
						}
					})
				}
				close(start)
				wg.Wait()
				onDisk, err := readMarker(db.storage, earliestVersionKey)
				require.NoError(t, err)
				require.Equal(t, want, onDisk, "round %d", round)
				require.Equal(t, onDisk, db.earliestHeight.Load(), "round %d", round)
			}
		})
	}
}

func TestResumeAndPruningKeepEarliestOnDisk(t *testing.T) {
	dir := t.TempDir()
	c := newChain(t, dir, 10, 0)
	for range 10 {
		c.apply(nil, false)
	}
	require.NoError(t, c.db.Close())
	db := openTestDB(t, dir, 10)
	defer func() { require.NoError(t, db.Close()) }()
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		<-start
		if err := db.Resume(20, liveState{}); err != nil {
			t.Error(err)
		}
	})
	for height := uint64(1); height <= 10; height++ {
		wg.Go(func() {
			<-start
			if err := db.PruneHistory(height); err != nil {
				t.Error(err)
			}
		})
	}
	close(start)
	wg.Wait()
	onDisk, err := readMarker(db.storage, earliestVersionKey)
	require.NoError(t, err)
	require.Equal(t, uint64(20), onDisk)
	require.Equal(t, onDisk, db.earliestHeight.Load())
}

func TestBlocksRequireSuccessfulResume(t *testing.T) {
	db := openTestDB(t, t.TempDir(), 10)
	defer func() { require.NoError(t, db.Close()) }()
	var closes atomic.Int64
	rejectBlock := func() {
		require.PanicsWithError(t, "undo: store is not resumed", func() {
			db.ApplyBlock(1, nil, &mapView{closes: &closes})
		})
		db.WaitForPendingWrites()
		require.Zero(t, db.GetLatestVersion())
	}
	rejectBlock()
	require.Equal(t, int64(1), closes.Load())
	require.ErrorContains(t, db.Resume(-1, &mapView{closes: &closes}), "negative")
	rejectBlock()
	require.Equal(t, int64(3), closes.Load())

	key := nonceKey(alice)
	require.NoError(t, db.Resume(5, liveState{key: nonce(1)}))
	db.ApplyBlock(6, []*proto.KVPair{{Key: []byte(key), Value: nonce(1)}}, liveState{key: nonce(2)})
	db.WaitForPendingWrites()
	require.Equal(t, int64(6), db.GetLatestVersion())
	require.Equal(t, nonce(1), read(t, db, 5, key))
	require.Equal(t, nonce(2), read(t, db, 6, key))
}

func TestResumeIsAcceptedOnce(t *testing.T) {
	c := newChain(t, t.TempDir(), 10, 0)
	defer func() { require.NoError(t, c.db.Close()) }()
	var closes atomic.Int64
	require.ErrorContains(t, c.db.Resume(0, &mapView{closes: &closes}), "already resumed")
	require.Equal(t, int64(1), closes.Load(), "a refused view is closed")
}

func TestCloseWithoutResume(t *testing.T) {
	dir := t.TempDir()
	db := openTestDB(t, dir, 10)
	db.WaitForPendingWrites()
	require.NoError(t, db.Close())
	db.WaitForPendingWrites()
	var closes atomic.Int64
	require.ErrorContains(t, db.Resume(0, &mapView{closes: &closes}), "closed")
	require.PanicsWithError(t, "undo: write queue is closed", func() {
		db.ApplyBlock(1, nil, &mapView{closes: &closes})
	})
	require.Equal(t, int64(2), closes.Load(), "rejected views are closed")

	reopened := openTestDB(t, dir, 10)
	defer func() { require.NoError(t, reopened.Close()) }()
	require.Zero(t, reopened.GetLatestVersion())
}

func TestPruneAfterCloseFails(t *testing.T) {
	for _, head := range []int64{0, 1} {
		c := newChain(t, t.TempDir(), 10, head)
		require.NoError(t, c.db.Close())
		require.ErrorContains(t, c.db.PruneHistory(0), "closed")
		require.ErrorContains(t, c.db.PruneHistory(1), "closed")
	}
}

func TestCloseRacesWithQueueAndPruning(t *testing.T) {
	for round := range 10 {
		t.Run(fmt.Sprint(round), func(t *testing.T) {
			c := newChain(t, t.TempDir(), 10, 0)
			defer func() { require.NoError(t, c.db.Close()) }()
			for version := int64(1); version <= 32; version++ {
				c.db.ApplyBlock(version, nil, c.view(nil))
			}
			start := make(chan struct{})
			var wg sync.WaitGroup
			var applied atomic.Bool
			wg.Go(func() {
				<-start
				defer func() {
					if r := recover(); r != nil && fmt.Sprint(r) != "undo: write queue is closed" {
						t.Errorf("unexpected panic: %v", r)
					}
				}()
				c.db.ApplyBlock(33, nil, c.view(nil))
				applied.Store(true)
			})
			wg.Go(func() {
				<-start
				c.db.WaitForPendingWrites()
			})
			wg.Go(func() {
				<-start
				if err := c.db.PruneHistory(20); err != nil && err.Error() != "undo: prune on a closed store" {
					t.Error(err)
				}
			})
			wg.Go(func() {
				<-start
				if err := c.db.Close(); err != nil {
					t.Error(err)
				}
			})
			close(start)
			wg.Wait()
			want := int64(32)
			if applied.Load() {
				want++
			}
			require.Equal(t, want, c.db.GetLatestVersion(), "accepted blocks are drained")
			require.Equal(t, c.created.Load(), c.closes.Load(), "every submitted view is closed")
		})
	}
}

func TestResumeRacesWithClose(t *testing.T) {
	for range 10 {
		db := openTestDB(t, t.TempDir(), 10)
		var closes atomic.Int64
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Go(func() {
			<-start
			if err := db.Resume(1, &mapView{closes: &closes}); err != nil && err.Error() != "undo: resume on a closed store" {
				t.Error(err)
			}
		})
		wg.Go(func() {
			<-start
			if err := db.Close(); err != nil {
				t.Error(err)
			}
		})
		close(start)
		wg.Wait()
		require.Equal(t, int64(1), closes.Load())
		_, ok := db.OpenView(1)
		require.False(t, ok, "Resume cannot publish a head after Close")
	}
}

func TestLargeBlocksLandWhole(t *testing.T) {
	c := newChain(t, t.TempDir(), 4, 0)
	defer func() { require.NoError(t, c.db.Close()) }()

	// Many times minBatchRecords, so the block is written as parallel batches.
	const slots = 60_000
	first, second := map[string][]byte{}, map[string][]byte{}
	for i := range uint64(slots) {
		first[storageKey(contract, i)] = word(i)
		second[storageKey(contract, i)] = word(slots + i)
	}
	c.apply(first, false)
	c.apply(second, true)
	c.db.WaitForPendingWrites()
	for i := uint64(0); i < slots; i += 997 {
		for h := int64(0); h <= 2; h++ {
			c.requireRead(h, storageKey(contract, i))
		}
	}
	atTwo := 0
	for _, r := range records(t, c.db) {
		if r.height == 2 {
			atTwo++
		}
	}
	require.Equal(t, slots, atTwo)
}

func TestValidEVMRecords(t *testing.T) {
	db := openTestDB(t, t.TempDir(), 10)
	defer func() { require.NoError(t, db.Close()) }()
	var prior []*proto.KVPair
	for _, kind := range evmTypes {
		_, valueSize := keyLayout([]byte{kind})
		prior = append(prior,
			&proto.KVPair{Key: evmKey(kind, 0), Value: make([]byte, max(valueSize, 0))},
			&proto.KVPair{Key: evmKey(kind, 1), Delete: true},
		)
	}
	require.Zero(t, testing.AllocsPerRun(100, func() {
		if err := validateRecords(prior); err != nil {
			t.Fatal(err)
		}
	}))
	height, err := db.writeBlock(1, prior)
	require.NoError(t, err)
	require.Equal(t, uint64(1), height)
	require.Len(t, records(t, db), len(prior))
	db.latestHeight.Store(height)

	// An empty block must still advance the committed height.
	height, err = db.writeBlock(2, nil)
	require.NoError(t, err)
	require.Equal(t, uint64(2), height)
	require.Len(t, records(t, db), len(prior))
	latest, err := readMarker(db.storage, latestVersionKey)
	require.NoError(t, err)
	require.Equal(t, uint64(2), latest)
}

func TestMalformedEVMRecordsLeaveBlockUnwritten(t *testing.T) {
	db := openTestDB(t, t.TempDir(), 10)
	defer func() { require.NoError(t, db.Close()) }()
	valid := &proto.KVPair{Key: evmKey(CodeKeyPrefix[0], 0), Value: []byte("code")}
	check := func(name string, bad *proto.KVPair) {
		t.Run(name, func(t *testing.T) {
			_, err := db.writeBlock(1, []*proto.KVPair{valid, bad})
			require.Error(t, err)
			require.Empty(t, records(t, db), "validation must precede every batch write")
			latest, err := readMarker(db.storage, latestVersionKey)
			require.NoError(t, err)
			require.Zero(t, latest)
		})
	}
	check("nil", nil)
	check("empty key", &proto.KVPair{})
	check("unknown type", &proto.KVPair{Key: []byte{0xff}, Delete: true})
	check("unsupported family", &proto.KVPair{
		Key: append([]byte{0x09}, make([]byte, addressLen)...), Value: make([]byte, 8),
	})
	for _, kind := range evmTypes {
		key := evmKey(kind, 1)
		for _, size := range []int{1, len(key) - 1, len(key) + 1} {
			malformed := make([]byte, size)
			copy(malformed, key)
			check(fmt.Sprintf("type=%x/key=%d", kind, size), &proto.KVPair{Key: malformed, Delete: true})
		}
		_, valueSize := keyLayout(key)
		if valueSize < 0 {
			continue
		}
		for _, size := range []int{0, 1, valueSize - 1, valueSize + 1} {
			check(fmt.Sprintf("type=%x/value=%d", kind, size), &proto.KVPair{Key: key, Value: make([]byte, size)})
		}
	}
}

func TestEVMFamiliesReadAcrossFlushCompactionAndReopen(t *testing.T) {
	dir := t.TempDir()
	c := newChain(t, dir, 2, 0)
	initial, updated, deleted := map[string][]byte{}, map[string][]byte{}, map[string][]byte{}
	for _, kind := range evmTypes {
		_, size := keyLayout([]byte{kind})
		for _, fill := range []byte{0, 0xff} {
			key := string(evmKey(kind, fill))
			initial[key] = make([]byte, max(size, 1))
			initial[key][len(initial[key])-1] = 1
			updatedSize := size
			if updatedSize < 0 {
				updatedSize = 4096
			}
			updated[key] = make([]byte, updatedSize)
			updated[key][len(updated[key])-1] = 2
			deleted[key] = nil
		}
	}
	c.apply(initial, true)
	c.apply(updated, true)
	c.apply(deleted, true)
	c.db.WaitForPendingWrites()
	require.NoError(t, c.db.storage.Flush())
	require.NoError(t, c.db.storage.Compact(t.Context(), bucketBoundary(0), bucketBoundary(2), true))
	verify := func() {
		for key := range initial {
			for h := int64(0); h <= 3; h++ {
				c.requireRead(h, key)
			}
		}
	}
	verify()
	require.NoError(t, c.db.Close())
	c.db = openTestDB(t, dir, 2)
	defer func() { require.NoError(t, c.db.Close()) }()
	require.NoError(t, c.db.Resume(3, c.view(c.states[3])))
	verify()

	// Query absent addresses and slots in populated SSTables, exercising prefix Bloom filters.
	v, ok := c.db.OpenView(0)
	require.True(t, ok)
	defer v.Close()
	for _, kind := range evmTypes {
		value, err := v.Get(evmKey(kind, 2))
		require.NoError(t, err)
		require.Nil(t, value)
	}
}

func TestReadsRejectUnsupportedAndMalformedKeys(t *testing.T) {
	c := newChain(t, t.TempDir(), 2, 0)
	defer func() { require.NoError(t, c.db.Close()) }()
	c.apply(nil, false)
	for _, height := range []int64{0, 1} {
		v, ok := c.db.OpenView(height)
		require.True(t, ok)
		for _, key := range [][]byte{nil, {0xff}, {StateKeyPrefix[0]}, append([]byte{0x09}, make([]byte, addressLen)...)} {
			_, err := v.Get(key)
			require.ErrorContains(t, err, "unsupported or malformed")
		}
		v.Close()
	}
}

func TestOldComparerIsRefused(t *testing.T) {
	dir := t.TempDir()
	oldComparer := *pebble.DefaultComparer
	oldComparer.Name = "ss_undolog_comparator"
	old, err := pebble.Open(dir, &pebble.Options{Comparer: &oldComparer})
	require.NoError(t, err)
	require.NoError(t, old.Close())
	db, err := OpenDB(dir, testConfig())
	if db != nil {
		require.NoError(t, db.Close())
	}
	require.ErrorContains(t, err, "comparer")
}

package undo

import (
	"encoding/binary"
	"fmt"
	"maps"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// liveState is the state commit store's view of the latest block: each key's current value.
type liveState map[string]string

func (s liveState) Get(_ string, key []byte) ([]byte, bool) {
	value, found := s[string(key)]
	if !found {
		return nil, false
	}
	return []byte(value), true
}

func (liveState) Close() {}

// TestUndoLogByHand spells the store out: the live state after the last block, the value each block
// replaced, and what a read after each block must return.
func TestUndoLogByHand(t *testing.T) {
	db := openTestDB(t, t.TempDir(), 10) // buckets of 10 blocks: 100-109, 110-119
	defer func() { require.NoError(t, db.Close()) }()

	// The live state after block 112.
	live := liveState{
		"alice": "80",
		"bob":   "7",
		"carol": "3",
	}

	// What each block changed, recorded as the value the key held before the block.
	was := func(key, value string) *proto.KVPair { return &proto.KVPair{Key: []byte(key), Value: []byte(value)} }
	wasAbsent := func(key string) *proto.KVPair { return &proto.KVPair{Key: []byte(key), Delete: true} }
	blocks := map[int64][]*proto.KVPair{
		103: {was("alice", "10")}, // alice 10 -> 15
		105: {wasAbsent("carol")}, // carol created with 3
		109: {was("alice", "15")}, // alice 15 -> 25
		110: {was("dave", "1")},   // dave deleted
		112: {was("alice", "25")}, // alice 25 -> 80
	}

	require.NoError(t, db.Resume(100, live))
	for height := int64(101); height <= 112; height++ {
		// Every block is handed the final live state: only the last block's is ever read.
		db.ApplyBlock(height, blocks[height], live)
	}
	db.WaitForPendingWrites()

	get := func(key string, height int64) string {
		view, ok := db.OpenView(height)
		require.True(t, ok, "height %d", height)
		defer view.Close()
		value, err := view.Get([]byte(key))
		require.NoError(t, err)
		if value == nil {
			return "<absent>"
		}
		return string(value)
	}
	for _, tc := range []struct {
		key    string
		height int64
		want   string
	}{
		{"alice", 100, "10"},
		{"alice", 102, "10"},
		{"alice", 103, "15"},
		{"alice", 108, "15"},
		{"alice", 109, "25"},
		{"alice", 111, "25"},
		{"alice", 112, "80"}, // no record above 112: the live state answers
		{"bob", 100, "7"},    // never changed: the live state answers at every height
		{"bob", 112, "7"},
		{"carol", 104, "<absent>"},
		{"carol", 105, "3"},
		{"dave", 101, "1"}, // bucket 10 holds nothing for dave; bucket 11 does
		{"dave", 109, "1"},
		{"dave", 110, "<absent>"},
	} {
		require.Equal(t, tc.want, get(tc.key, tc.height), "%s after block %d", tc.key, tc.height)
	}
}

// mapView is a CurrentView over an immutable map, counting how often it is closed.
type mapView struct {
	values map[string][]byte
	closes *atomic.Int64
}

func (v *mapView) Get(_ string, key []byte) ([]byte, bool) {
	value, found := v.values[string(key)]
	return value, found
}

func (v *mapView) Close() { v.closes.Add(1) }

func testConfig(bucketSize uint64) config.StateStoreConfig {
	cfg := config.DefaultStateStoreConfig()
	cfg.AsyncWriteBuffer = 16
	return cfg
}

func openTestDB(t testing.TB, dir string, bucketSize uint64) *Database {
	t.Helper()
	db, err := OpenDB(dir, testConfig(bucketSize))
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

func (c *chain) view(values map[string][]byte) CurrentView {
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
	require.NoError(t, err, "read %q at %d", key, height)
	return value
}

func (c *chain) requireRead(height int64, key string) {
	c.t.Helper()
	want, found := c.stateAt(height, key)
	got := read(c.t, c.db, height, key)
	if !found {
		require.Nil(c.t, got, "read %q at %d", key, height)
		return
	}
	require.NotNil(c.t, got, "read %q at %d", key, height)
	require.Equal(c.t, want, got, "read %q at %d", key, height)
}

// record is one undo record as stored.
type record struct {
	bucket uint64
	key    string
	height int64
	value  string
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
		body := keyBody(itr.Key())
		out = append(out, record{
			bucket: binary.BigEndian.Uint64(body),
			key:    string(body[bucketLen:]),
			height: int64(height), //nolint:gosec // test heights are small
			value:  string(value),
		})
	}
	require.NoError(t, itr.Error())
	return out
}

func TestReadsFollowTheSpecExample(t *testing.T) {
	c := newChain(t, t.TempDir(), 10, 100)
	defer func() { require.NoError(t, c.db.Close()) }()

	balances := map[int64][]byte{103: []byte("15"), 109: []byte("25"), 112: []byte("80")}
	c.apply(map[string][]byte{"alice": []byte("10")}, false) // block 101 creates the account
	for h := int64(102); h <= 118; h++ {
		changes := map[string][]byte{}
		if v, ok := balances[h]; ok {
			changes["alice"] = v
		}
		c.apply(changes, false)
	}

	for height, want := range map[int64]string{102: "10", 108: "15", 110: "25", 118: "80"} {
		require.Equal(t, want, string(read(t, c.db, height, "alice")), "after block %d", height)
	}
	require.Nil(t, read(t, c.db, 100, "alice"), "alice did not exist before block 101")

	// 101, 103 and 109 land in bucket 10 and 112 in bucket 11, each holding the value before its block.
	require.Equal(t, []record{
		{10, "alice", 101, ""}, {10, "alice", 103, "10"}, {10, "alice", 109, "15"}, {11, "alice", 112, "25"},
	}, records(t, c.db))
}

func TestAbsentAndEmptyValuesStayDistinct(t *testing.T) {
	c := newChain(t, t.TempDir(), 4, 0)
	defer func() { require.NoError(t, c.db.Close()) }()

	c.apply(map[string][]byte{"k": {}}, false)          // 1: created with an empty value
	c.apply(map[string][]byte{"k": []byte("v")}, false) // 2
	c.apply(map[string][]byte{"k": nil}, false)         // 3: deleted
	c.apply(map[string][]byte{"other": []byte("x")}, false)

	for h := int64(0); h <= 4; h++ {
		c.requireRead(h, "k")
	}
	got := read(t, c.db, 1, "k")
	require.NotNil(t, got)
	require.Empty(t, got)
}

func TestRandomHistoryMatchesTheModel(t *testing.T) {
	for _, async := range []bool{false, true} {
		t.Run(fmt.Sprintf("async=%v", async), func(t *testing.T) {
			rng := rand.New(rand.NewPCG(7, 8))
			c := newChain(t, t.TempDir(), 7, 3)
			defer func() { require.NoError(t, c.db.Close()) }()

			keys := make([]string, 40)
			for i := range keys {
				keys[i] = fmt.Sprintf("key-%02d", i)
			}
			for range 150 {
				changes := map[string][]byte{}
				// Keys are written at very different rates, so reads cross many buckets of
				// unchanged history.
				for range rng.IntN(6) {
					key := keys[rng.IntN(1+rng.IntN(len(keys)))]
					if rng.IntN(5) == 0 {
						changes[key] = nil
					} else {
						changes[key] = fmt.Appendf(nil, "v%d", rng.IntN(1000))
					}
				}
				c.apply(changes, async)
			}
			c.db.WaitForPendingWrites()
			require.Equal(t, c.head(), c.db.GetLatestVersion())
			for h := c.base; h <= c.head(); h++ {
				for _, key := range keys {
					c.requireRead(h, key)
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
				for _, key := range keys {
					c.requireRead(h, key)
				}
			}
		})
	}
}

func TestPruneExcisesOnlyExpiredBuckets(t *testing.T) {
	c := newChain(t, t.TempDir(), 10, 0)
	defer func() { require.NoError(t, c.db.Close()) }()
	for h := 1; h <= 45; h++ {
		c.apply(map[string][]byte{"hot": fmt.Appendf(nil, "%d", h), fmt.Sprintf("cold-%d", h): []byte("x")}, false)
	}

	// Bucket 2 spans 20-29 and holds records a read at 25 needs, so only buckets 0 and 1 go.
	require.NoError(t, c.db.PruneHistory(25))
	require.Equal(t, int64(20), records(t, c.db)[0].height)
	for h := int64(25); h <= 45; h++ {
		c.requireRead(h, "hot")
		c.requireRead(h, "cold-30")
	}

	// A prune that crosses no bucket boundary excises nothing more.
	require.NoError(t, c.db.PruneHistory(28))
	require.Equal(t, uint64(2), c.db.prunedBucket)
	require.NoError(t, c.db.PruneHistory(29))
	require.Equal(t, uint64(3), c.db.prunedBucket)

	// A cut line above the head is clamped to it.
	require.NoError(t, c.db.PruneHistory(1_000))
	require.Equal(t, int64(45), c.db.GetEarliestVersion())
	c.requireRead(45, "hot")
}

func TestOpenViewsKeepTheirBucketsThroughAPrune(t *testing.T) {
	c := newChain(t, t.TempDir(), 5, 0)
	defer func() { require.NoError(t, c.db.Close()) }()
	for h := 1; h <= 30; h++ {
		c.apply(map[string][]byte{"k": fmt.Appendf(nil, "%d", h)}, false)
	}

	view, ok := c.db.OpenView(7)
	require.True(t, ok)
	require.NoError(t, c.db.PruneHistory(20))
	_, ok = c.db.OpenView(7)
	require.False(t, ok, "no new view opens below the earliest height")
	got, err := view.Get([]byte("k"))
	require.NoError(t, err)
	require.Equal(t, "7", string(got), "the open view still reads the buckets it needs")
	require.Equal(t, uint64(1), c.db.prunedBucket, "only the bucket below the view went")

	view.Close()
	require.NoError(t, c.db.PruneHistory(20))
	require.Equal(t, uint64(4), c.db.prunedBucket, "the next prune excises what the view held back")
	c.requireRead(20, "k")
}

func TestReadersNeverSeeExcisedHistory(t *testing.T) {
	c := newChain(t, t.TempDir(), 5, 0)
	defer func() { require.NoError(t, c.db.Close()) }()
	const keys = 8
	for h := 1; h <= 20; h++ {
		c.apply(map[string][]byte{fmt.Sprintf("k%d", h%keys): fmt.Appendf(nil, "%d", h)}, false)
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
					key := fmt.Sprintf("k%d", rng.IntN(keys))
					got, err := view.Get([]byte(key))
					if err != nil {
						t.Errorf("read %q at %d: %v", key, height, err)
						view.Close()
						return
					}
					if want, _ := c.stateAt(height, key); string(want) != string(got) {
						t.Errorf("read %q at %d: got %q want %q", key, height, got, want)
						view.Close()
						return
					}
					reads.Add(1)
				}
				view.Close()
			}
		})
	}
	for h := 21; h <= 400; h++ {
		c.apply(map[string][]byte{fmt.Sprintf("k%d", h%keys): fmt.Appendf(nil, "%d", h)}, true)
		if h%10 == 0 {
			require.NoError(t, c.db.PruneHistory(uint64(h-29))) //nolint:gosec // positive
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
	for h := 1; h <= 10; h++ {
		c.apply(map[string][]byte{"k": fmt.Appendf(nil, "%d", h)}, false)
	}
	require.Equal(t, c.created.Load()-1, c.closes.Load(), "every view but the head is closed")

	pinned, ok := c.db.OpenView(5)
	require.True(t, ok)
	c.apply(map[string][]byte{"k": []byte("11")}, false)
	require.Equal(t, c.created.Load()-2, c.closes.Load(), "the pinned view outlives its replacement")
	got, err := pinned.Get([]byte("k"))
	require.NoError(t, err)
	require.Equal(t, "5", string(got))
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
	for h := 1; h <= 35; h++ {
		c.apply(map[string][]byte{"k": fmt.Appendf(nil, "%d", h)}, true)
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
		c.requireRead(h, "k")
	}
	require.Equal(t, int64(20), records(t, c.db)[0].height)
	require.NoError(t, c.db.Close())
}

func TestANewDatabaseNeedsABucketSize(t *testing.T) {
	_, err := OpenDB(t.TempDir(), testConfig(0))
	require.ErrorContains(t, err, "bucket size must be positive")
}

func TestResumeBehindCurrentStateDropsHistory(t *testing.T) {
	dir := t.TempDir()
	c := newChain(t, dir, 10, 0)
	for h := 1; h <= 12; h++ {
		c.apply(map[string][]byte{"k": fmt.Appendf(nil, "%d", h)}, false)
	}
	require.NoError(t, c.db.Close())

	// Current state moved on to 15 without the log: blocks 13-15 left no records.
	reopened := openTestDB(t, dir, 10)
	defer func() { require.NoError(t, reopened.Close()) }()
	var closes atomic.Int64
	require.NoError(t, reopened.Resume(15, &mapView{values: map[string][]byte{"k": []byte("15")}, closes: &closes}))
	require.Equal(t, int64(15), reopened.GetEarliestVersion())
	_, ok := reopened.OpenView(12)
	require.False(t, ok)
	require.Equal(t, "15", string(read(t, reopened, 15, "k")))
}

func TestResumeAheadOfCurrentStateIsRefused(t *testing.T) {
	dir := t.TempDir()
	c := newChain(t, dir, 10, 0)
	for h := 1; h <= 12; h++ {
		c.apply(map[string][]byte{"k": fmt.Appendf(nil, "%d", h)}, false)
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
	for h := 1; h <= 37; h++ {
		c.apply(map[string][]byte{"k": fmt.Appendf(nil, "%d", h), fmt.Sprintf("c%d", h): []byte("x")}, false)
	}
	require.NoError(t, c.db.Close())

	require.NoError(t, DiscardStateAbove(dir, testConfig(10), 14))
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
	c.apply(map[string][]byte{"k": []byte("new-15")}, false)
	for h := int64(0); h <= 15; h++ {
		c.requireRead(h, "k")
		c.requireRead(h, "c20")
	}
	require.NoError(t, reopened.Close())

	// A log at or below the target, or no log at all, is left alone.
	require.NoError(t, DiscardStateAbove(dir, testConfig(10), 100))
	require.NoError(t, DiscardStateAbove(t.TempDir()+"/absent", testConfig(10), 1))
}

func TestLargeBlocksLandWhole(t *testing.T) {
	c := newChain(t, t.TempDir(), 4, 0)
	defer func() { require.NoError(t, c.db.Close()) }()

	// Many times minBatchRecords, so the block is written as parallel batches.
	const keys = 60_000
	first, second := map[string][]byte{}, map[string][]byte{}
	for i := range keys {
		first[fmt.Sprintf("key-%06d", i)] = fmt.Appendf(nil, "v1-%d", i)
		second[fmt.Sprintf("key-%06d", i)] = fmt.Appendf(nil, "v2-%d", i)
	}
	c.apply(first, false)
	c.apply(second, true)
	c.db.WaitForPendingWrites()
	for i := 0; i < keys; i += 997 {
		key := fmt.Sprintf("key-%06d", i)
		for h := int64(0); h <= 2; h++ {
			c.requireRead(h, key)
		}
	}
	atTwo := 0
	for _, r := range records(t, c.db) {
		if r.height == 2 {
			atTwo++
		}
	}
	require.Equal(t, keys, atTwo)
}

func TestValuesStayInlineAcrossHeightsOfAKey(t *testing.T) {
	c := newChain(t, t.TempDir(), 1000, 0)
	defer func() { require.NoError(t, c.db.Close()) }()
	// Every block rewrites the same keys, so each SST holds many heights of one prefix: exactly the
	// shape Pebble would otherwise split into value blocks.
	for h := 1; h <= 50; h++ {
		changes := map[string][]byte{}
		for k := range 200 {
			changes[fmt.Sprintf("key-%03d", k)] = fmt.Appendf(nil, "value-%d-%d-padding-padding-padding", k, h)
		}
		c.apply(changes, false)
	}
	require.NoError(t, c.db.storage.Flush())
	require.NoError(t, c.db.storage.Compact(t.Context(), bucketBoundary(0), bucketBoundary(1), true))
	levels, err := c.db.storage.SSTables(pebble.WithProperties())
	require.NoError(t, err)
	tables := 0
	for _, level := range levels {
		for _, table := range level {
			tables++
			require.Zero(t, table.Properties.NumValuesInValueBlocks, "table %s", table.FileNum)
		}
	}
	require.Positive(t, tables)
	for h := int64(0); h <= 50; h += 7 {
		c.requireRead(h, "key-042")
	}
}

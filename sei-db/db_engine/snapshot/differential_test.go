package snapshot

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-db/common/testutil"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// TestDifferentialAgainstModel drives randomized operation sequences through both the real
// SnapshotEngine and a naive deep-copy oracle (modelEngine), deep-comparing every observable read
// (live + all held snapshots: Get, BatchGet, GetDiff, Iterator) after each step. Any data-integrity
// divergence fails the test. Seeds are fixed for reproducibility.
func TestDifferentialAgainstModel(t *testing.T) {
	configs := []struct {
		name       string
		shardCount uint64
		maxSize    uint64
		seedDB     bool
	}{
		{"1shard", 1, 1 << 20, false},
		{"8shard", 8, 1 << 20, false},
		{"8shard-evicting", 8, 512, false},  // tiny dbCache -> forces eviction; must not affect reads
		{"8shard-seeded", 8, 1 << 20, true}, // pre-seeded DB -> exercises read-through
	}
	seeds := []int64{1, 2, 3}

	for _, cfg := range configs {
		for _, seed := range seeds {
			t.Run(fmt.Sprintf("%s/seed=%d", cfg.name, seed), func(t *testing.T) {
				runDifferential(t, cfg.shardCount, cfg.maxSize, cfg.seedDB, seed)
			})
		}
	}
}

const (
	opSet = iota
	opDelete
	opBatch
	opSnapshot
)

func runDifferential(t *testing.T, shardCount, maxSize uint64, seedDB bool, seed int64) {
	rng := testutil.NewTestRandomNoPrint(seed)
	keys := genKeys(rng, 40)

	var seedData map[string][]byte
	if seedDB {
		seedData = make(map[string][]byte)
		for i := 0; i < 15; i++ {
			seedData[string(pick(rng, keys))] = randVal(rng)
		}
	}

	db := newTestDB(seedData)
	cfg := newTestConfig(shardCount, maxSize)
	cfg.MaxUnflushedVersions = 64 // keep held snapshots from tripping backpressure
	engine := newTestEngineWithConfig(t, cfg, db)
	model := newModelEngine(seedData)

	type openSnap struct {
		snap Snapshot
		ver  uint64
	}
	var opens []openSnap
	const maxOpen = 5
	const iterations = 400

	releaseOldest := func() {
		require.NoError(t, opens[0].snap.Release())
		opens = opens[1:]
	}

	for i := 0; i < iterations; i++ {
		switch pickOp(rng) {
		case opSet:
			k, v := pick(rng, keys), randVal(rng)
			require.NoError(t, engine.Set(k, v))
			model.Set(k, v)
		case opDelete:
			k := pick(rng, keys)
			require.NoError(t, engine.Delete(k))
			model.Delete(k)
		case opBatch:
			muts := randMuts(rng, keys)
			require.NoError(t, engine.BatchSet(muts))
			model.BatchSet(muts)
		case opSnapshot:
			if len(opens) >= maxOpen {
				releaseOldest()
			}
			snap, err := engine.Commit()
			require.NoError(t, err)
			ver := model.Commit()
			// Hash immediately while holding the reservation: this lets the background flusher race
			// ahead of Release, exercising the flush-then-read (merge in-memory + DB) path.
			require.NoError(t, snap.Finalize(hashWrites(testHash)))
			opens = append(opens, openSnap{snap: snap, ver: ver})
		}

		// The live (mutable) version must agree on every operation.
		liveLabel := fmt.Sprintf("live@i=%d", i)
		compareReads(t, liveLabel,
			func(k []byte) ([]byte, bool, error) { return engine.Get(k, true) },
			model.GetLive, keys)
		checkLiveIteration(t, liveLabel, engine, model)

		// Periodically deep-check every held snapshot.
		if i%10 == 0 {
			for _, o := range opens {
				checkSnapshot(t, o.snap, model, o.ver, keys)
			}
		}
	}

	// Final full check, then drain all held snapshots in order.
	for _, o := range opens {
		checkSnapshot(t, o.snap, model, o.ver, keys)
	}
	for len(opens) > 0 {
		releaseOldest()
	}
}

// checkSnapshot deep-compares a held snapshot against the oracle across all read surfaces.
func checkSnapshot(t *testing.T, snap Snapshot, model *modelEngine, ver uint64, keys [][]byte) {
	label := fmt.Sprintf("snap@v=%d", ver)
	lookup := func(k []byte) ([]byte, bool) { return model.GetAt(ver, k) }

	compareReads(t, label, func(k []byte) ([]byte, bool, error) { return snap.Get(k, false) }, lookup, keys)
	compareBatchGet(t, label, snap.BatchGet, lookup, keys)

	gotDiff, err := snap.GetDiff()
	require.NoError(t, err, "%s GetDiff", label)
	require.Equal(t, model.DiffAt(ver), gotDiff, "%s diff mismatch", label)
}

// checkLiveIteration compares the engine's mutable-version iterator against the oracle. The iterator
// must be closed before the caller writes again, since the engine refuses writes while one is open.
func checkLiveIteration(t *testing.T, label string, engine SnapshotEngine, model *modelEngine) {
	it, err := engine.Iterator(nil)
	require.NoError(t, err, "%s Iterator", label)
	compareIterator(t, label, it, model.IterateLive())
}

func compareReads(t *testing.T, label string, get func(key []byte) ([]byte, bool, error),
	lookup func(key []byte) ([]byte, bool), keys [][]byte) {
	for _, k := range keys {
		gotV, gotFound, err := get(k)
		require.NoError(t, err, "%s Get key=%x", label, k)
		expV, expFound := lookup(k)
		require.Equal(t, expFound, gotFound, "%s found mismatch key=%x", label, k)
		if expFound {
			require.True(t, bytes.Equal(expV, gotV), "%s value mismatch key=%x", label, k)
		}
	}
}

func compareBatchGet(t *testing.T, label string, batchGet func([][]byte) (map[string][]byte, error),
	lookup func(key []byte) ([]byte, bool), keys [][]byte) {
	got, err := batchGet(keys)
	require.NoError(t, err, "%s BatchGet", label)
	for _, k := range keys {
		gotV, present := got[string(k)]
		expV, expFound := lookup(k)
		require.Equal(t, expFound, present, "%s BatchGet found key=%x", label, k)
		if expFound {
			require.True(t, bytes.Equal(expV, gotV), "%s BatchGet value key=%x", label, k)
		}
	}
}

func compareIterator(t *testing.T, label string, it dbm.Iterator, expected []kvPair) {
	got := collectIterator(t, it) // closes it, which is what releases the engine's write block
	require.Equal(t, len(expected), len(got), "%s iterator length", label)
	for i := range expected {
		require.True(t, bytes.Equal(expected[i].key, got[i].key), "%s iterator key at %d", label, i)
		require.True(t, bytes.Equal(expected[i].value, got[i].value), "%s iterator value at %d", label, i)
	}
}

// --- random operation generation ---

func pickOp(rng *testutil.TestRandom) int {
	switch r := rng.IntRange(0, 100); {
	case r < 45:
		return opSet
	case r < 60:
		return opDelete
	case r < 80:
		return opBatch
	default:
		return opSnapshot
	}
}

func genKeys(rng *testutil.TestRandom, n int) [][]byte {
	keys := make([][]byte, n)
	keys[0] = []byte{} // include the empty key as an edge case
	for i := 1; i < n; i++ {
		keys[i] = rng.VariableBytes(1, 8) // possibly non-printable
	}
	return keys
}

func randVal(rng *testutil.TestRandom) []byte {
	if rng.BoolWithProbability(0.1) {
		return []byte{} // empty (non-nil) value: a set with a zero-length value, distinct from delete
	}
	return rng.VariableBytes(1, 64)
}

func pick(rng *testutil.TestRandom, keys [][]byte) []byte {
	return keys[rng.IntRange(0, len(keys))]
}

func randMuts(rng *testutil.TestRandom, keys [][]byte) []*proto.KVPair {
	n := rng.IntRange(1, 9)
	muts := make([]*proto.KVPair, n)
	for i := range muts {
		k := pick(rng, keys)
		if rng.BoolWithProbability(0.25) {
			muts[i] = &proto.KVPair{Key: k, Delete: true} // delete
		} else {
			muts[i] = &proto.KVPair{Key: k, Value: randVal(rng)}
		}
	}
	return muts
}

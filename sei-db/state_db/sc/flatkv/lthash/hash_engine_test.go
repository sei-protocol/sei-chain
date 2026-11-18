package lthash

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
)

// These tests drive the engine over synthetic blocks, so they exercise the pipeline — ordering, backpressure,
// shutdown, failure — rather than the arithmetic, which lthash_test.go and the store's golden archive cover.

const (
	engineAccountName = "account"
	engineCodeName    = "code"
	engineStorageName = "storage"
	engineMiscName    = "misc"
)

var engineDBNames = []string{engineAccountName, engineCodeName, engineStorageName, engineMiscName}

// engineModuleOf puts every key in one module, which is all these tests need to distinguish.
func engineModuleOf([]byte) (string, error) { return "m", nil }

// blockMutations builds one block's mutations: diff is what the block wrote, a nil value being a deletion, and
// prior is what those keys held before it. The whole diff goes to the account database; the rest are untouched,
// as most blocks leave most databases alone.
func blockMutations(diff map[string][]byte, prior map[string][]byte) []DatabaseMutations {
	mutations := make([]DatabaseMutations, 0, len(engineDBNames))
	for _, dbName := range engineDBNames {
		dbMutations := DatabaseMutations{DBName: dbName}
		if dbName == engineAccountName {
			for _, key := range slices.Sorted(maps.Keys(diff)) {
				dbMutations.Mutations = append(dbMutations.Mutations, mutation.Mutation{
					Key:           []byte(key),
					NewValue:      diff[key],
					PreviousValue: prior[key],
				})
			}
		}
		mutations = append(mutations, dbMutations)
	}
	return mutations
}

// newTestEngine builds an engine over a small pool, with the given channel depths.
func newTestEngine(t *testing.T, fold uint32, hashes uint32) *HashEngine {
	t.Helper()
	return newTestEngineWithModuleOf(t, fold, hashes, engineModuleOf)
}

// newTestEngineWithModuleOf is newTestEngine with the given module parser.
func newTestEngineWithModuleOf(
	t *testing.T,
	fold uint32,
	hashes uint32,
	moduleOf ModuleParser,
) *HashEngine {
	t.Helper()
	pool := threading.NewFixedPool("lthash-engine-test", 4, 64)
	t.Cleanup(pool.Close)

	cfg := DefaultConfig()
	cfg.CombineQueueSize = fold
	cfg.HashChanSize = hashes

	engine, err := NewHashEngine(t.Context(), cfg, pool, engineDBNames, moduleOf, NewBlockHash(engineDBNames))
	require.NoError(t, err)
	return engine
}

// blockDiff builds a diff of n distinct keys for the given height.
func blockDiff(height int64, n int) map[string][]byte {
	diff := make(map[string][]byte, n)
	for i := 0; i < n; i++ {
		diff[fmt.Sprintf("m/key-%03d", i)] = []byte(fmt.Sprintf("v-%d-%d", height, i))
	}
	return diff
}

// A block's hash must be the same whether it went through the pipeline or was computed in one call, or
// the pipeline has changed the answer — which is the one thing it must not do.
// The reference the engine's pipeline is checked against: hash one block's mutations and combine them
// in one call on this goroutine, with no pipelining and no previous block.
func compute(
	pool threading.Pool,
	dbNames []string,
	moduleOf ModuleParser,
	mutations []DatabaseMutations,
	// How many KV pairs each task carries.
	chunkSize uint32,
) (*BlockHash, error) {
	deltas, err := ComputeModuleHashInfos(pool, moduleOf, mutations, chunkSize)
	if err != nil {
		return nil, err
	}
	return combine(dbNames, deltas, nil, nil, nil), nil
}

func TestHashEngineAgreesWithSynchronousCompute(t *testing.T) {
	pool := threading.NewFixedPool("lthash-sync", 4, 64)
	defer pool.Close()

	diff := blockDiff(1, 250)
	prior := map[string][]byte{"m/key-000": []byte("old"), "m/key-001": []byte("older")}

	engine := newTestEngine(t, 4, 4)
	require.NoError(t, engine.ScheduleHash(1, blockMutations(diff, prior)))
	got := <-engine.AwaitHash()
	require.NoError(t, got.Error)
	require.NoError(t, engine.Close())

	// The same block, folded in one call against the same empty starting state.
	mutations := blockMutations(diff, prior)
	want, err := compute(pool, engineDBNames, engineModuleOf, mutations, DefaultConfig().ChunkSize)
	require.NoError(t, err)

	require.Equal(t, want.Global.Checksum(), got.Global.Checksum(),
		"the pipeline must produce the hash a single-call fold produces")
	require.Equal(t, int64(1), got.BlockNumber)
}

// The stream's contract is exactly one hash per block, in block order. This schedules more blocks than
// the combine queue holds without reading any of them, so several blocks are being hashed at once, then
// checks the order that came out.
func TestHashEngineStreamsOneHashPerBlockInOrder(t *testing.T) {
	const blocks = 12
	engine := newTestEngine(t, 2, blocks)

	for height := int64(1); height <= blocks; height++ {
		require.NoError(t, engine.ScheduleHash(height, blockMutations(blockDiff(height, 120), nil)))
	}

	for height := int64(1); height <= blocks; height++ {
		got := <-engine.AwaitHash()
		require.NoError(t, got.Error)
		require.Equal(t, height, got.BlockNumber, "hashes must arrive in block order with no gaps")
	}
	require.NoError(t, engine.Close())

	_, open := <-engine.AwaitHash()
	require.False(t, open, "the stream closes once a stopped engine has drained")
}

// An engine built with a seed measures its first block against that seed, which is how a store with
// history avoids folding block N onto an empty predecessor.
func TestHashEngineStartsFromItsSeed(t *testing.T) {
	engine := newTestEngine(t, 4, 4)
	require.NoError(t, engine.ScheduleHash(1, blockMutations(blockDiff(1, 40), nil)))
	fromEmpty := (<-engine.AwaitHash()).Global.Checksum()
	require.NoError(t, engine.Close())

	pool := threading.NewFixedPool("lthash-seeded", 4, 64)
	defer pool.Close()
	cfg := DefaultConfig()
	seeded, err := NewHashEngine(t.Context(), cfg, pool, engineDBNames, engineModuleOf, seedWithOneBlock(t))
	require.NoError(t, err)
	require.NoError(t, seeded.ScheduleHash(2, blockMutations(blockDiff(2, 40), nil)))
	fromSeed := (<-seeded.AwaitHash()).Global.Checksum()
	require.NoError(t, seeded.Close())

	require.NotEqual(t, fromEmpty, fromSeed, "a seeded engine must not hash as though it had no history")
}

// seedWithOneBlock returns the state a store would have loaded after one block.
func seedWithOneBlock(t *testing.T) *BlockHash {
	t.Helper()
	engine := newTestEngine(t, 4, 4)
	require.NoError(t, engine.ScheduleHash(1, blockMutations(blockDiff(1, 40), nil)))
	seed := <-engine.AwaitHash()
	require.NoError(t, seed.Error)
	require.NoError(t, engine.Close())
	return seed
}

// Flush is the barrier a caller uses when it needs the engine to have caught up with what it scheduled.
func TestHashEngineFlushWaitsForScheduledBlocks(t *testing.T) {
	const blocks = 6
	engine := newTestEngine(t, 2, blocks)
	defer func() { require.NoError(t, engine.Close()) }()

	for height := int64(1); height <= blocks; height++ {
		require.NoError(t, engine.ScheduleHash(height, blockMutations(blockDiff(height, 80), nil)))
	}
	require.NoError(t, engine.Flush())

	// Every hash is already on the stream, so reading them cannot block.
	for height := int64(1); height <= blocks; height++ {
		select {
		case got := <-engine.AwaitHash():
			require.Equal(t, height, got.BlockNumber)
		default:
			t.Fatalf("Flush returned before block %d was published", height)
		}
	}
}

// Close abandons whatever it has not reached rather than finishing it, and still returns.
func TestHashEngineCloseAbandons(t *testing.T) {
	const blocks = 5
	engine := newTestEngine(t, blocks, blocks)

	for height := int64(1); height <= blocks; height++ {
		require.NoError(t, engine.ScheduleHash(height, blockMutations(blockDiff(height, 30), nil)))
	}

	require.NoError(t, engine.Close())

	// Whatever was hashed before the stop is on the stream, and the stream is closed behind it.
	for range engine.AwaitHash() {
	}
}

// A schedule parked on a full queue has to be released once the engine stops.
func TestScheduleHashIsReleasedWhenTheEngineStops(t *testing.T) {
	pool := threading.NewFixedPool("lthash-schedule-shutdown-test", 4, 64)
	t.Cleanup(pool.Close)

	// One block deep at every stage, so the pipeline saturates within a handful of blocks while nothing
	// reads AwaitHash. Its own context, so the test can stop it; newTestEngine ties one to t.Context().
	cfg := DefaultConfig()
	cfg.CombineQueueSize = 1
	cfg.HashChanSize = 1
	ctx, cancel := context.WithCancel(t.Context())
	engine, err := NewHashEngine(ctx, cfg, pool, engineDBNames, engineModuleOf, NewBlockHash(engineDBNames))
	require.NoError(t, err)
	// Registered after the pool's cleanup so it runs before it: the engine's goroutines submit leaf
	// hashing to the pool, and Close is what waits for them to stop.
	t.Cleanup(func() { require.NoError(t, engine.Close()) })

	// Built here rather than in the goroutine below, which must not touch t.
	const blocks = 8
	pending := make([][]DatabaseMutations, 0, blocks)
	for height := int64(1); height <= blocks; height++ {
		pending = append(pending, blockMutations(blockDiff(height, 4), nil))
	}

	results := make(chan error, blocks)
	go func() {
		for i, block := range pending {
			results <- engine.ScheduleHash(int64(i+1), block)
		}
	}()

	// Once a schedule stops reporting, the pipeline is full and that call is parked on the send.
	accepted := 0
	for parked := false; !parked; {
		select {
		case err := <-results:
			require.NoError(t, err, "a running engine must take block %d", accepted+1)
			accepted++
		case <-time.After(500 * time.Millisecond):
			parked = true
		}
	}
	require.Less(t, accepted, blocks, "the pipeline never filled, so no schedule was left parked")

	cancel()

	select {
	case err := <-results:
		require.Error(t, err, "a schedule parked on a full queue must be released once the engine stops")
	case <-time.After(30 * time.Second):
		t.Fatal("ScheduleHash never returned after the engine was stopped")
	}
}

// failingModuleOf fails every key, so any block with a mutation fails to hash.
func failingModuleOf([]byte) (string, error) { return "", errors.New("injected module failure") }

// The first failure is delivered on the stream, and nothing is published after it: once a block has
// failed, the accumulator describes nothing a later block may be derived from.
func TestHashEngineDeliversFailureAndStops(t *testing.T) {
	engine := newTestEngineWithModuleOf(t, 4, 4, failingModuleOf)

	require.NoError(t, engine.ScheduleHash(1, blockMutations(blockDiff(1, 10), nil)))

	got := <-engine.AwaitHash()
	require.Error(t, got.Error)
	require.ErrorContains(t, got.Error, "injected module failure")
	require.Equal(t, int64(1), got.BlockNumber)

	require.ErrorContains(t, engine.Close(), "injected module failure")

	_, open := <-engine.AwaitHash()
	require.False(t, open, "nothing is published after the failure, and the stream closes with the engine")
}

// A failed engine refuses further work rather than accepting blocks it will never hash.
func TestHashEngineRefusesWorkAfterFailure(t *testing.T) {
	engine := newTestEngineWithModuleOf(t, 4, 4, failingModuleOf)

	require.NoError(t, engine.ScheduleHash(1, blockMutations(blockDiff(1, 10), nil)))
	require.Error(t, (<-engine.AwaitHash()).Error)

	err := engine.ScheduleHash(2, blockMutations(blockDiff(2, 10), nil))
	require.Error(t, err, "a failed engine must refuse a block rather than swallow it")
	require.ErrorContains(t, engine.Close(), "injected module failure")
}

// A flush waits to be told the engine has dealt with everything queued ahead of it, which a stopping
// engine never will. Its caller has to be released by the shutdown rather than left parked.
func TestFlushReturnsOnceTheEngineIsStopped(t *testing.T) {
	pool := threading.NewFixedPool("lthash-flush-shutdown-test", 4, 64)
	t.Cleanup(pool.Close)

	// The engine gets its own context so the test can stop it; newTestEngine ties one to t.Context().
	ctx, cancel := context.WithCancel(t.Context())
	engine, err := NewHashEngine(
		ctx, DefaultConfig(), pool, engineDBNames, engineModuleOf, NewBlockHash(engineDBNames))
	require.NoError(t, err)
	// Registered after the pool's cleanup so it runs before it: the engine's goroutines submit leaf
	// hashing to the pool, and Close is what waits for them to stop.
	t.Cleanup(func() { require.NoError(t, engine.Close()) })

	require.NoError(t, engine.ScheduleHash(1, blockMutations(blockDiff(1, 4), nil)))

	cancel()

	flushed := make(chan error, 1)
	go func() { flushed <- engine.Flush() }()

	select {
	case err := <-flushed:
		require.Error(t, err, "a flush released by shutdown reports that it never flushed")
	case <-time.After(30 * time.Second):
		t.Fatal("Flush never returned after the engine was stopped")
	}
}

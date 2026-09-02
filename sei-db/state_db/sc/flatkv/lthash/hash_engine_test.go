package lthash

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/sview"
)

// These tests drive the engine over stub views, so they exercise the pipeline — ordering, backpressure,
// shutdown, failure — rather than the arithmetic, which lthash_test.go and the store's golden archive
// cover.

const (
	engineAccountName = "account"
	engineCodeName    = "code"
	engineStorageName = "storage"
	engineMiscName    = "misc"
)

var engineDBNames = []string{engineAccountName, engineCodeName, engineStorageName, engineMiscName}

// engineModuleOf puts every key in one module, which is all these tests need to distinguish.
func engineModuleOf([]byte) (string, error) { return "m", nil }

var _ view.View = (*pipeView)(nil)

// pipeView is a view holding one block's diff for one database, over a fixed prior state. Only the
// three methods the gatherer reaches are implemented.
type pipeView struct {
	name string

	// diff is what this block changed, as GetDiff reports it. A nil value is a deletion.
	diff map[string][]byte

	// prior is the state BatchGet answers from, i.e. what the keys held before this block.
	prior map[string][]byte

	// getDiffErr, when set, fails the read.
	getDiffErr error

	// reserves and releases count reservations, so a test can assert the engine balanced them.
	reserves int
	releases int
}

func (v *pipeView) Name() string { return v.name }

func (v *pipeView) GetDiff() (map[string][]byte, error) {
	if v.getDiffErr != nil {
		return nil, v.getDiffErr
	}
	return v.diff, nil
}

func (v *pipeView) BatchGet(keys [][]byte) (map[string][]byte, error) {
	out := make(map[string][]byte, len(keys))
	for _, key := range keys {
		if value, ok := v.prior[string(key)]; ok {
			out[string(key)] = value
		}
	}
	return out, nil
}

func (v *pipeView) Reserve() error { v.reserves++; return nil }
func (v *pipeView) Release() error { v.releases++; return nil }

func (v *pipeView) Get([]byte, bool) ([]byte, bool, error) { panic("pipeView: unexpected Get") }
func (v *pipeView) Finalize([]*proto.KVPair) error         { panic("pipeView: unexpected Finalize") }
func (v *pipeView) AwaitFlush(context.Context) error       { panic("pipeView: unexpected AwaitFlush") }

// blockViews builds the pair of store views for one block: current carries the diff, previous answers
// for the values it replaced.
func blockViews(
	t *testing.T,
	height int64,
	diff map[string][]byte,
	prior map[string][]byte,
) (current *sview.StoreView, previous *sview.StoreView, views []*pipeView) {
	t.Helper()
	var currents, previouses []view.View
	for _, dbName := range engineDBNames {
		// The whole diff goes to the account database; the rest are untouched, as most blocks leave
		// most databases alone.
		blockDiff := map[string][]byte{}
		if dbName == engineAccountName {
			blockDiff = diff
		}
		cur := &pipeView{name: dbName, diff: blockDiff}
		prev := &pipeView{name: dbName, prior: prior}
		views = append(views, cur, prev)
		currents = append(currents, cur)
		previouses = append(previouses, prev)
	}
	current, err := sview.NewStoreView(height, currents[0], currents[1], currents[2], currents[3])
	require.NoError(t, err)
	previous, err = sview.NewStoreView(height-1, previouses[0], previouses[1], previouses[2], previouses[3])
	require.NoError(t, err)
	return current, previous, views
}

// newTestEngine builds an engine over a small pool, with the given channel depths.
func newTestEngine(t *testing.T, schedule uint32, fold uint32, hashes uint32) *HashEngine {
	t.Helper()
	pool := threading.NewFixedPool("lthash-engine-test", 4, 64)
	t.Cleanup(pool.Close)

	cfg := DefaultConfig()
	cfg.ScheduleQueueSize = schedule
	cfg.CombineQueueSize = fold
	cfg.HashChanSize = hashes

	engine, err := NewHashEngine(t.Context(), cfg, pool, engineDBNames, engineModuleOf, NewBlockHash(engineDBNames))
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

	engine := newTestEngine(t, 4, 4, 4)
	current, previous, _ := blockViews(t, 1, diff, prior)
	require.NoError(t, engine.ScheduleHash(current, previous))
	got := <-engine.AwaitHash()
	require.NoError(t, got.Error)
	require.NoError(t, engine.Close())

	// The same block, folded in one call against the same empty starting state.
	mutations, err := gatherChangesFromAllStores(mustViews(t, 1, diff, prior))
	require.NoError(t, err)
	want, err := compute(pool, engineDBNames, engineModuleOf, mutations, DefaultConfig().ChunkSize)
	require.NoError(t, err)

	require.Equal(t, want.Global.Checksum(), got.Global.Checksum(),
		"the pipeline must produce the hash a single-call fold produces")
	require.Equal(t, int64(1), got.BlockNumber)
}

// mustViews is blockViews without the stub handles, for a caller that only wants the views.
func mustViews(t *testing.T, height int64, diff map[string][]byte, prior map[string][]byte) (
	*sview.StoreView, *sview.StoreView,
) {
	t.Helper()
	current, previous, _ := blockViews(t, height, diff, prior)
	return current, previous
}

// The stream's contract is exactly one hash per block, in block order. This schedules more blocks than
// the combine queue holds without reading any of them, so the gatherer runs ahead and several blocks are
// being hashed at once, then checks the order that came out.
func TestHashEngineStreamsOneHashPerBlockInOrder(t *testing.T) {
	const blocks = 12
	engine := newTestEngine(t, 2, 2, blocks)

	for height := int64(1); height <= blocks; height++ {
		current, previous := mustViews(t, height, blockDiff(height, 120), nil)
		require.NoError(t, engine.ScheduleHash(current, previous))
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

// The gatherer owns both blocks' reservations and must hand them back as soon as it has read them —
// a reservation left held stalls its database's flushes forever.
func TestHashEngineReleasesBothViews(t *testing.T) {
	engine := newTestEngine(t, 4, 4, 4)

	current, previous, views := blockViews(t, 1, blockDiff(1, 10), nil)
	require.NoError(t, engine.ScheduleHash(current, previous))
	require.NoError(t, (<-engine.AwaitHash()).Error)
	require.NoError(t, engine.Close())

	for _, v := range views {
		require.Equal(t, 1, v.reserves, "%s: the engine must take its own reservation", v.name)
		require.Equal(t, 1, v.releases, "%s: the engine must release the reservation it took", v.name)
	}
}

// An engine built with a seed measures its first block against that seed, which is how a store with
// history avoids folding block N onto an empty predecessor.
func TestHashEngineStartsFromItsSeed(t *testing.T) {
	engine := newTestEngine(t, 4, 4, 4)
	current, previous := mustViews(t, 1, blockDiff(1, 40), nil)
	require.NoError(t, engine.ScheduleHash(current, previous))
	fromEmpty := (<-engine.AwaitHash()).Global.Checksum()
	require.NoError(t, engine.Close())

	pool := threading.NewFixedPool("lthash-seeded", 4, 64)
	defer pool.Close()
	cfg := DefaultConfig()
	seeded, err := NewHashEngine(t.Context(), cfg, pool, engineDBNames, engineModuleOf, seedWithOneBlock(t))
	require.NoError(t, err)
	current, previous = mustViews(t, 2, blockDiff(2, 40), nil)
	require.NoError(t, seeded.ScheduleHash(current, previous))
	fromSeed := (<-seeded.AwaitHash()).Global.Checksum()
	require.NoError(t, seeded.Close())

	require.NotEqual(t, fromEmpty, fromSeed, "a seeded engine must not hash as though it had no history")
}

// seedWithOneBlock returns the state a store would have loaded after one block.
func seedWithOneBlock(t *testing.T) *BlockHash {
	t.Helper()
	engine := newTestEngine(t, 4, 4, 4)
	current, previous := mustViews(t, 1, blockDiff(1, 40), nil)
	require.NoError(t, engine.ScheduleHash(current, previous))
	seed := <-engine.AwaitHash()
	require.NoError(t, seed.Error)
	require.NoError(t, engine.Close())
	return seed
}

// Flush is the barrier a caller uses when it needs the engine to have caught up with what it scheduled.
func TestHashEngineFlushWaitsForScheduledBlocks(t *testing.T) {
	const blocks = 6
	engine := newTestEngine(t, blocks, 2, blocks)
	defer func() { require.NoError(t, engine.Close()) }()

	for height := int64(1); height <= blocks; height++ {
		current, previous := mustViews(t, height, blockDiff(height, 80), nil)
		require.NoError(t, engine.ScheduleHash(current, previous))
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

// Close abandons whatever it has not reached rather than finishing it, but every abandoned block still
// has to hand its reservations back — a view left reserved can never flush, and the store above could
// never finish tearing down.
func TestHashEngineCloseAbandonsAndReleases(t *testing.T) {
	const blocks = 5
	engine := newTestEngine(t, blocks, blocks, blocks)

	var scheduled [][]*pipeView
	for height := int64(1); height <= blocks; height++ {
		current, previous, views := blockViews(t, height, blockDiff(height, 30), nil)
		scheduled = append(scheduled, views)
		require.NoError(t, engine.ScheduleHash(current, previous))
	}

	require.NoError(t, engine.Close())

	for _, views := range scheduled {
		for _, v := range views {
			require.Equal(t, 1, v.reserves, "%s: the engine must take its own reservation", v.name)
			require.Equal(t, 1, v.releases,
				"%s: a block abandoned at Close must still hand its reservation back", v.name)
		}
	}

	// Whatever was hashed before the stop is on the stream, and the stream is closed behind it.
	for range engine.AwaitHash() {
	}
}

// The first failure is delivered on the stream, and nothing is published after it: once a block has
// failed, the accumulator describes nothing a later block may be derived from.
func TestHashEngineDeliversFailureAndStops(t *testing.T) {
	engine := newTestEngine(t, 4, 4, 4)

	current, previous, views := blockViews(t, 1, blockDiff(1, 10), nil)
	for _, v := range views {
		v.getDiffErr = errors.New("injected diff failure")
	}
	require.NoError(t, engine.ScheduleHash(current, previous))

	got := <-engine.AwaitHash()
	require.Error(t, got.Error)
	require.ErrorContains(t, got.Error, "injected diff failure")
	require.Equal(t, int64(1), got.BlockNumber)

	require.ErrorContains(t, engine.Close(), "injected diff failure")

	_, open := <-engine.AwaitHash()
	require.False(t, open, "nothing is published after the failure, and the stream closes with the engine")

	for _, v := range views {
		require.Equal(t, 1, v.reserves, "%s: the engine must take its own reservation", v.name)
		require.Equal(t, 1, v.releases, "%s: a failed read must still hand its reservation back", v.name)
	}
}

// A failed engine refuses further work rather than accepting blocks it will never hash.
func TestHashEngineRefusesWorkAfterFailure(t *testing.T) {
	engine := newTestEngine(t, 4, 4, 4)

	current, previous, views := blockViews(t, 1, blockDiff(1, 10), nil)
	for _, v := range views {
		v.getDiffErr = errors.New("injected diff failure")
	}
	require.NoError(t, engine.ScheduleHash(current, previous))
	require.Error(t, (<-engine.AwaitHash()).Error)

	next, nextPrev, nextViews := blockViews(t, 2, blockDiff(2, 10), nil)
	err := engine.ScheduleHash(next, nextPrev)
	require.Error(t, err, "a failed engine must refuse a block rather than swallow it")
	for _, v := range nextViews {
		require.Equal(t, 1, v.reserves, "%s: the engine must take its own reservation", v.name)
		require.Equal(t, 1, v.releases, "%s: a refused block's reservation must be released", v.name)
	}
	require.ErrorContains(t, engine.Close(), "injected diff failure")
}

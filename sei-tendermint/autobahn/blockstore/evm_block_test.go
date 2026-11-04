package blockstore_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

func newDurableTestStore(t *testing.T) (*blockstore.Store, []batch) {
	t.Helper()
	store, err := blockstore.New(memblock.NewBlockDB())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	committee, keys := buildCommittee()
	return store, generateBatches(committee, keys)
}

func writeBatch(t *testing.T, store *blockstore.Store, b batch) {
	t.Helper()
	require.NoError(t, store.WriteBlock(types.NewRoadBlock(b.qc, b.blocks)))
}

// canceledContext returns a context that is already canceled, so EVMBlock
// returns at once: the block when it is durable, and the cancellation otherwise.
func canceledContext(t *testing.T) context.Context {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	return ctx
}

// TestEVMBlockServesABlockOnlyAfterFlush verifies that a written block reaches
// EVMBlock only once Flush has made it durable.
func TestEVMBlockServesABlockOnlyAfterFlush(t *testing.T) {
	store, batches := newDurableTestStore(t)
	writeBatch(t, store, batches[0])

	_, err := store.EVMBlock(canceledContext(t), batches[0].first)
	require.ErrorIs(t, err, context.Canceled)

	require.NoError(t, store.Flush())
	got, err := store.EVMBlock(canceledContext(t), batches[0].first)
	require.NoError(t, err)
	require.Equal(t, batches[0].first, got.SuperBlock().Height)
}

var errFlushFailed = errors.New("flush failed")

// failingFlushDB is a BlockDB whose Flush fails while fail is set.
type failingFlushDB struct {
	blocktypes.BlockDB
	fail *atomic.Bool
}

func (d failingFlushDB) Flush() error {
	if d.fail.Load() {
		return errFlushFailed
	}
	return d.BlockDB.Flush()
}

// TestEVMBlockKeepsBlocksAcrossAFailedFlush verifies that a failed Flush
// leaves its blocks for the next Flush, even one with no new writes.
func TestEVMBlockKeepsBlocksAcrossAFailedFlush(t *testing.T) {
	var fail atomic.Bool
	store, err := blockstore.New(failingFlushDB{BlockDB: memblock.NewBlockDB(), fail: &fail})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	writeBatch(t, store, batches[0])

	fail.Store(true)
	require.ErrorIs(t, store.Flush(), errFlushFailed)
	_, err = store.EVMBlock(canceledContext(t), batches[0].first)
	require.ErrorIs(t, err, context.Canceled)

	fail.Store(false)
	require.NoError(t, store.Flush())
	got, err := store.EVMBlock(canceledContext(t), batches[0].first)
	require.NoError(t, err)
	require.Equal(t, batches[0].first, got.SuperBlock().Height)
}

// TestEVMBlockWaitsForFlush verifies that a reader waiting on a block wakes
// up once a Flush makes it durable.
func TestEVMBlockWaitsForFlush(t *testing.T) {
	store, batches := newDurableTestStore(t)

	var got *types.RoadBlock
	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error {
			block, err := store.EVMBlock(ctx, batches[1].first)
			got = block
			return err
		})
		for _, b := range batches[:2] {
			if err := store.WriteBlock(types.NewRoadBlock(b.qc, b.blocks)); err != nil {
				return err
			}
		}
		return store.Flush()
	}))
	require.Equal(t, batches[1].first, got.SuperBlock().Height)
}

// TestEVMBlockReadsStoredBlocksAfterReopen verifies the restart path: a store
// reopened over existing data serves every stored block at once, from the BlockDB.
func TestEVMBlockReadsStoredBlocksAfterReopen(t *testing.T) {
	db := memblock.NewBlockDB()
	first, err := blockstore.New(db)
	require.NoError(t, err)
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	for _, b := range batches {
		writeBatch(t, first, b)
	}
	require.NoError(t, first.Flush())

	reopened, err := blockstore.New(db)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })
	for _, b := range batches {
		got, err := reopened.EVMBlock(canceledContext(t), b.first)
		require.NoError(t, err)
		require.Equal(t, b.first, got.SuperBlock().Height)
	}
}

// TestEVMBlockBelowFirstIsPruned verifies that EVMBlock refuses a block
// below the retention floor, even one it still has cached.
func TestEVMBlockBelowFirstIsPruned(t *testing.T) {
	store, batches := newDurableTestStore(t)
	_, keys := buildCommittee()
	for _, b := range batches {
		writeBatch(t, store, b)
	}
	writeAppData(t, store, utils.TestRng(), keys, batches)
	require.NoError(t, store.Flush())
	require.NoError(t, store.PruneBefore(batches[1].first))
	require.Equal(t, batches[1].first, store.First())

	_, err := store.EVMBlock(t.Context(), batches[0].first)
	require.ErrorIs(t, err, types.ErrPruned)
	got, err := store.EVMBlock(t.Context(), batches[1].first)
	require.NoError(t, err)
	require.Equal(t, batches[1].first, got.SuperBlock().Height)
}

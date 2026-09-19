package evmonly

import (
	"context"
	"math"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestOCCShardOfIsMonotonicAndCoversEveryShard(t *testing.T) {
	seen := map[int]bool{}
	previous := 0
	for first := range 256 {
		addr := common.Address{byte(first), 0xff}
		shard := occShardOf(addr)
		require.GreaterOrEqual(t, shard, previous, "shards must follow address order")
		require.Less(t, shard, occStateShards)
		seen[shard] = true
		previous = shard
	}
	require.Len(t, seen, occStateShards)
}

func TestOCCShardsOwnedByPartitionsTheShards(t *testing.T) {
	for _, workers := range []int{1, 3, 8, 64, 100} {
		var union occShardSet
		for workerID := range workers {
			owned := occShardsOwnedBy(workerID, workers)
			require.False(t, union.intersects(owned), "workers=%d worker %d overlaps another", workers, workerID)
			union |= owned
		}
		require.Equal(t, occAllShards, union, "workers=%d", workers)
	}
}

func TestConflictsWithinHonoursBothBounds(t *testing.T) {
	addr := testAddress(0xa1)
	key := stateAccessKey{kind: stateAccessBalance, address: addr}
	writes := newStateAccessIndex()
	writes.addAllAt(5, map[stateAccessKey]struct{}{key: {}})

	require.False(t, writes.conflictsWithin(key, 0, 5), "a write at the reader's own index is not a conflict")
	require.False(t, writes.conflictsWithin(key, 0, 3), "a write above the reader is not a conflict")
	require.False(t, writes.conflictsWithin(key, 6, 10), "a write below the source prefix is not a conflict")
	require.True(t, writes.conflictsWithin(key, 0, 6))
	require.True(t, writes.conflictsWithin(key, 5, 6))
	require.False(t, writes.conflictsWithin(key, 6, 6), "an empty range holds no writes")
	require.False(t, writes.conflictsWithin(stateAccessKey{kind: stateAccessBalance, address: testAddress(0xa2)}, 0, 10))
}

func TestConflictsWithinSpanOnlyOverApproximates(t *testing.T) {
	addr := testAddress(0xa1)
	key := stateAccessKey{kind: stateAccessBalance, address: addr}
	writes := newStateAccessIndex()
	writes.addAllAt(2, map[stateAccessKey]struct{}{key: {}})
	writes.addAllAt(9, map[stateAccessKey]struct{}{key: {}})

	require.True(t, writes.conflictsWithin(key, 4, 7), "a gap inside the span is reported as a conflict")
	require.False(t, writes.conflictsWithin(key, 0, 2))
	require.False(t, writes.conflictsWithin(key, 10, 20))
}

func TestCumulativeGasFromStopsBeforeOverflow(t *testing.T) {
	results := []occTxExecution{{gasUsed: 10}, {gasUsed: 20}, {gasUsed: math.MaxUint64}, {gasUsed: 1}}

	cumulative, to := cumulativeGasFrom(results, 1, 100)
	require.Equal(t, 2, to)
	require.Equal(t, []uint64{100, 120}, cumulative)

	cumulative, to = cumulativeGasFrom(results[:2], 0, 0)
	require.Equal(t, 2, to)
	require.Equal(t, []uint64{0, 10, 30}, cumulative)
}

func TestFirstUnacceptedResultReturnsTheLowestRejection(t *testing.T) {
	pool := newOCCWorkerPool(8)
	defer pool.Close()
	const count = 1000
	results := make([]occTxExecution, count)
	for i := range results {
		results[i] = occTxExecution{gasLimit: 1, gasUsed: 1}
	}
	cumulative, to := cumulativeGasFrom(results, 0, 0)
	require.Equal(t, count, to)
	writes := newStateAccessIndex()

	stop, err := firstUnacceptedResult(context.Background(), pool, results, writes, math.MaxUint64, 0, to, cumulative)
	require.NoError(t, err)
	require.Equal(t, count, stop, "every result is acceptable")

	for _, rejected := range []int{999, 640, 333, 1} {
		results[rejected].err = errOCCMaxIncarnation
		stop, err = firstUnacceptedResult(context.Background(), pool, results, writes, math.MaxUint64, 0, to, cumulative)
		require.NoError(t, err)
		require.Equal(t, rejected, stop)
	}

	results[0].err = nil
	stop, err = firstUnacceptedResult(context.Background(), pool, results, writes, math.MaxUint64, 0, to, cumulative)
	require.NoError(t, err)
	require.Equal(t, 1, stop, "the lowest rejection wins even when higher ones are found first")
}

func TestFirstUnacceptedResultRejectsGasAndConflicts(t *testing.T) {
	pool := newOCCWorkerPool(4)
	defer pool.Close()
	addr := testAddress(0xb1)
	key := stateAccessKey{kind: stateAccessNonce, address: addr}
	results := make([]occTxExecution, 200)
	for i := range results {
		results[i] = occTxExecution{gasLimit: 10, gasUsed: 10}
	}
	results[150].readSet = map[stateAccessKey]struct{}{key: {}}
	writes := newStateAccessIndex()
	writes.addAllAt(20, map[stateAccessKey]struct{}{key: {}})
	cumulative, to := cumulativeGasFrom(results, 0, 0)

	stop, err := firstUnacceptedResult(context.Background(), pool, results, writes, math.MaxUint64, 0, to, cumulative)
	require.NoError(t, err)
	require.Equal(t, 150, stop, "a read of a write inside [sourcePrefix, txIndex) is rejected")

	results[150].sourcePrefix = 21
	stop, err = firstUnacceptedResult(context.Background(), pool, results, writes, math.MaxUint64, 0, to, cumulative)
	require.NoError(t, err)
	require.Equal(t, len(results), stop, "a write below the source prefix is already accounted for")

	stop, err = firstUnacceptedResult(context.Background(), pool, results, writes, 10*100, 0, to, cumulative)
	require.NoError(t, err)
	require.Equal(t, 100, stop, "the first result over the block gas limit is rejected")
}

func TestTouchedShardsCoversEveryAddressTheResultChanged(t *testing.T) {
	written := common.Address{0x00}
	credited := common.Address{0x40}
	changed := common.Address{0x80}
	cleared := common.Address{0xc0}
	result := occTxExecution{
		writeSet:                 map[stateAccessKey]struct{}{{kind: stateAccessNonce, address: written}: {}},
		commutativeBalanceDeltas: map[common.Address]*big.Int{credited: big.NewInt(1)},
	}
	result.changeSet.Storage = append(result.changeSet.Storage, StorageChange{Address: changed})
	result.changeSet.StorageClears = append(result.changeSet.StorageClears, cleared)

	touched := result.touchedShards()
	for _, addr := range []common.Address{written, credited, changed, cleared} {
		require.True(t, touched.has(occShardOf(addr)), "%s", addr)
	}
	require.False(t, touched.has(occShardOf(common.Address{0x20})))
}

func TestSmallMergeStaysOnTheCallingGoroutine(t *testing.T) {
	pool := newOCCWorkerPool(4)
	pool.Close()
	state := newBlockSTMState(NewMemoryState())
	state.shards[0].nonces[common.Address{0x00}] = 1
	require.Less(t, state.keyCount(), occMinParallelMergeKeys)

	var changes StateChangeSet
	require.NoError(t, state.changeSetIntoParallel(context.Background(), pool, &changes), "a closed pool is never touched below the threshold")
	require.Equal(t, state.ChangeSet(), changes)
}

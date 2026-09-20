package evmonly

import (
	"context"
	"math"
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
)

// occStateShards is the number of address shards the accepted prefix and the write index are split
// into. Shards are contiguous address ranges, so concatenating per-shard output in shard order keeps
// it in canonical address order.
const occStateShards = 64

// occMinParallelValidation is the fewest results a parallel validation pass is worth running for;
// below it, waking the pool costs more than validating on the calling goroutine.
const occMinParallelValidation = 64

// occMinParallelMergeKeys is the fewest accepted keys a parallel merge is worth running for; below
// it, the merge runs on the calling goroutine and appends straight into the pooled changeset.
const occMinParallelMergeKeys = 256

// occCancellationCheckInterval is how many items a worker handles between context checks.
const occCancellationCheckInterval = 64

// occShardSet is a set of shards, one bit per shard.
type occShardSet uint64

var _ = [1]struct{}{}[occStateShards-64] // occShardSet has exactly one bit per shard.

const occAllShards = occShardSet(math.MaxUint64)

func occShardOf(addr common.Address) int {
	return int(addr[0]) * occStateShards / 256
}

func (s occShardSet) has(shard int) bool { return s&(1<<shard) != 0 }

func (s occShardSet) with(shard int) occShardSet { return s | 1<<shard }

func (s occShardSet) intersects(other occShardSet) bool { return s&other != 0 }

// occShardsOwnedBy assigns the shards round-robin across the pool's workers.
func occShardsOwnedBy(workerID int, workers int) occShardSet {
	var owned occShardSet
	for shard := workerID; shard < occStateShards; shard += workers {
		owned = owned.with(shard)
	}
	return owned
}

// touchedShards returns the shards of every address the result wrote, credited, or changed, so a
// worker can skip the result outright when none of them is its own.
func (r occTxExecution) touchedShards() occShardSet {
	var touched occShardSet
	for key := range r.writeSet {
		touched = touched.with(occShardOf(key.address))
	}
	for addr := range r.commutativeBalanceDeltas {
		touched = touched.with(occShardOf(addr))
	}
	for _, change := range r.changeSet.Balances {
		touched = touched.with(occShardOf(change.Address))
	}
	for _, change := range r.changeSet.Nonces {
		touched = touched.with(occShardOf(change.Address))
	}
	for _, change := range r.changeSet.Code {
		touched = touched.with(occShardOf(change.Address))
	}
	for _, addr := range r.changeSet.StorageClears {
		touched = touched.with(occShardOf(addr))
	}
	for _, change := range r.changeSet.Storage {
		touched = touched.with(occShardOf(change.Address))
	}
	return touched
}

// indexResults records every result's writes, each worker filling the shards it owns.
func (i *stateAccessIndex) indexResults(ctx context.Context, pool *occWorkerPool, results []occTxExecution) error {
	if len(results) < occMinParallelValidation {
		for txIndex, result := range results {
			i.addAllAt(txIndex, result.writeSet)
			i.addCommutativeBalanceDeltasAt(txIndex, result.commutativeBalanceDeltas)
		}
		return ctx.Err()
	}
	return pool.Run(ctx, occStateShards, func(workerCtx context.Context, workerID int, workers int) error {
		owns := occShardsOwnedBy(workerID, workers)
		for txIndex, result := range results {
			if txIndex%occCancellationCheckInterval == 0 {
				if err := workerCtx.Err(); err != nil {
					return err
				}
			}
			if !result.shards.intersects(owns) {
				continue
			}
			i.addSpan(txIndexSpan{first: txIndex, last: txIndex}, result.writeSet, owns)
			i.addCommutativeBalanceDeltas(txIndex, result.commutativeBalanceDeltas, owns)
		}
		return nil
	})
}

// acceptValidatedPrefix validates the results from the frontier onward across the pool, stopping at
// the first one the serial frontier would not accept as it stands, and folds the accepted run into
// the prefix. It returns how many results it accepted.
func (e *Executor) acceptValidatedPrefix(
	ctx context.Context,
	runner occSpeculativeRunner,
	pool *occWorkerPool,
	results []occTxExecution,
	state *blockSTMValidationState,
	validation *occValidationResult,
) (int, error) {
	from := state.nextToValidate
	if len(results)-from < occMinParallelValidation {
		return 0, nil
	}
	cumulative, to := cumulativeGasFrom(results, from, state.cumulativeGasUsed)
	stop, err := firstUnacceptedResult(ctx, pool, results, state.writes, runner.blockGasLimit, from, to, cumulative)
	if err != nil {
		return 0, err
	}
	if stop == from {
		return 0, nil
	}
	if err := state.prefix.applyRange(ctx, pool, results[from:stop]); err != nil {
		return 0, err
	}
	validation.validationCount += uint64(stop - from) //nolint:gosec // stop > from.
	state.cumulativeGasUsed = cumulative[stop-from]
	state.nextToValidate = stop
	return stop - from, nil
}

// cumulativeGasFrom returns, for each result at or after from, the block gas used before it, plus one
// trailing entry for the gas used after the last. It stops at the first result whose gas would
// overflow the counter and returns that index, or len(results) when none does.
func cumulativeGasFrom(results []occTxExecution, from int, gasUsedBefore uint64) ([]uint64, int) {
	cumulative := make([]uint64, 1, len(results)-from+1)
	cumulative[0] = gasUsedBefore
	for i := from; i < len(results); i++ {
		gasUsed := results[i].gasUsed
		if gasUsed > math.MaxUint64-cumulative[i-from] {
			return cumulative, i
		}
		cumulative = append(cumulative, cumulative[i-from]+gasUsed)
	}
	return cumulative, len(results)
}

// firstUnacceptedResult returns the lowest index in [from, to) whose result the serial frontier
// would not accept given every result before it accepted, or to when it would accept them all.
func firstUnacceptedResult(
	ctx context.Context,
	pool *occWorkerPool,
	results []occTxExecution,
	writes *stateAccessIndex,
	blockGasLimit uint64,
	from int,
	to int,
	cumulative []uint64,
) (int, error) {
	var stop atomic.Int64
	stop.Store(int64(to))
	err := pool.Run(ctx, to-from, func(workerCtx context.Context, workerID int, workers int) error {
		for i := from + workerID; i < to; i += workers {
			if int64(i) >= stop.Load() {
				return nil
			}
			if (i-from)%occCancellationCheckInterval < workers {
				if err := workerCtx.Err(); err != nil {
					return err
				}
			}
			if stmFrontierAccepts(results[i], writes, cumulative[i-from], blockGasLimit, i) {
				continue
			}
			lowerStop(&stop, int64(i))
			return nil
		}
		return nil
	})
	return int(stop.Load()), err
}

func lowerStop(stop *atomic.Int64, index int64) {
	for {
		current := stop.Load()
		if current <= index || stop.CompareAndSwap(current, index) {
			return
		}
	}
}

// stmFrontierAccepts mirrors the serial frontier's decision for a result at txIndex, without
// recording anything: the frontier records the outcome itself when it reaches the result.
func stmFrontierAccepts(result occTxExecution, writes *stateAccessIndex, cumulativeGasUsed uint64, blockGasLimit uint64, txIndex int) bool {
	if result.err != nil {
		return false
	}
	if _, err := stmGasFailure(result, cumulativeGasUsed, blockGasLimit); err != nil {
		return false
	}
	for key := range result.readSet {
		if writes.conflictsWithin(key, result.sourcePrefix, txIndex) {
			return false
		}
	}
	for key := range result.writeSet {
		if writes.conflictsWithin(key, result.sourcePrefix, txIndex) {
			return false
		}
	}
	return true
}

// applyRange folds a run of accepted results into the prefix in block order, each worker applying
// the shards it owns.
func (s *blockSTMState) applyRange(ctx context.Context, pool *occWorkerPool, results []occTxExecution) error {
	return pool.Run(ctx, occStateShards, func(workerCtx context.Context, workerID int, workers int) error {
		owns := occShardsOwnedBy(workerID, workers)
		for txIndex, result := range results {
			if txIndex%occCancellationCheckInterval == 0 {
				if err := workerCtx.Err(); err != nil {
					return err
				}
			}
			if result.shards.intersects(owns) {
				s.applyOwned(result, owns)
			}
		}
		return nil
	})
}

// occChangeSetFragments holds one changeset per shard for a parallel merge to fill.
type occChangeSetFragments [occStateShards]StateChangeSet

// occFragmentPool recycles fragment arrays so their capacity survives across blocks.
var occFragmentPool = sync.Pool{New: func() any { return new(occChangeSetFragments) }}

// changeSetIntoParallel writes the block's net state changes, in canonical order, computing each
// shard's part on the pool. The shards' base-state reads are what the merge mostly spends its time
// on, and they are independent of each other.
func (s *blockSTMState) changeSetIntoParallel(ctx context.Context, pool *occWorkerPool, changes *StateChangeSet) error {
	if pool == nil || s.keyCount() < occMinParallelMergeKeys {
		s.ChangeSetInto(changes)
		return ctx.Err()
	}
	changes.resetForReuse()
	fragments := occFragmentPool.Get().(*occChangeSetFragments)
	defer occFragmentPool.Put(fragments)
	err := pool.Run(ctx, occStateShards, func(workerCtx context.Context, workerID int, workers int) error {
		for shard := workerID; shard < occStateShards; shard += workers {
			if err := workerCtx.Err(); err != nil {
				return err
			}
			fragments[shard].resetForReuse()
			s.shards[shard].changeSetInto(s.source, &fragments[shard])
		}
		return nil
	})
	if err != nil {
		return err
	}
	for i := range fragments {
		fragment := &fragments[i]
		changes.Balances = append(changes.Balances, fragment.Balances...)
		changes.Nonces = append(changes.Nonces, fragment.Nonces...)
		changes.Code = append(changes.Code, fragment.Code...)
		changes.StorageClears = append(changes.StorageClears, fragment.StorageClears...)
		changes.Storage = append(changes.Storage, fragment.Storage...)
	}
	return nil
}

// keyCount returns how many keys the accepted prefix holds across all shards.
func (s *blockSTMState) keyCount() int {
	count := 0
	for i := range s.shards {
		shard := &s.shards[i]
		count += len(shard.balances) + len(shard.nonces) + len(shard.code) + len(shard.storageClears) + len(shard.storage)
	}
	return count
}

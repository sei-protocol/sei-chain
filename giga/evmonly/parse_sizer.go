package evmonly

import (
	"sync/atomic"
	"time"
)

// perTxCostWeight is the denominator of the exponential moving average of the
// per-transaction decode cost; each block moves the estimate 1/perTxCostWeight
// of the way to what it measured.
const perTxCostWeight = 8

// parseSizer picks how many workers decode a block from the block's size, the
// time available to decode it, and a running estimate of the per-transaction
// decode cost measured on the blocks before it.
type parseSizer struct {
	maxWorkers int
	// perTx is the estimated processor time to decode one transaction, in
	// nanoseconds. 0 until the first block has been measured.
	perTx atomic.Int64
}

func newParseSizer(maxWorkers int) *parseSizer {
	return &parseSizer{maxWorkers: max(maxWorkers, 1)}
}

// workers returns how many workers decode txs transactions within budget: the
// fewest the estimated cost fits in, and every worker when there is no budget
// or no estimate yet.
func (s *parseSizer) workers(txs int, budget time.Duration) int {
	if txs <= 1 {
		return 1
	}
	perTx := s.perTx.Load()
	if budget <= 0 || perTx <= 0 {
		return min(s.maxWorkers, txs)
	}
	needed := (int64(txs)*perTx + int64(budget) - 1) / int64(budget)
	return int(max(1, min(needed, int64(min(s.maxWorkers, txs)))))
}

// observe folds a decode of txs transactions on workers workers that took
// elapsed into the per-transaction cost estimate.
func (s *parseSizer) observe(txs, workers int, elapsed time.Duration) {
	if txs <= 0 || workers <= 0 || elapsed <= 0 {
		return
	}
	measured := int64(elapsed) * int64(workers) / int64(txs)
	for {
		current := s.perTx.Load()
		next := measured
		if current > 0 {
			next = current + (measured-current)/perTxCostWeight
		}
		if s.perTx.CompareAndSwap(current, next) {
			return
		}
	}
}

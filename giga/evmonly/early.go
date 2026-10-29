package evmonly

import (
	"context"
	"math/big"
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/core/vm"
)

// earlyRecentBlocks is how many executed blocks' changes are kept for checking early results; a block
// speculated against an older state is executed without its early results.
const earlyRecentBlocks = 8

// earlyChunk is how many transactions an early speculation worker claims at a time.
const earlyChunk = 8

// earlySpeculation is a block's transactions speculated before the block executes.
type earlySpeculation struct {
	block PreparedBlock
	// base is the last block executed when the state was read; the state holds at least its writes.
	base    uint64
	results []occTxExecution
	// done[i] reports that results[i] holds a finished speculation.
	done    []atomic.Bool
	claimed atomic.Int64
	stop    atomic.Bool
	wg      sync.WaitGroup
}

// SpeculateEarly starts speculating b's transactions against the state the newest executed block
// left, so that executing b reuses every result no block executed in between invalidated. It returns
// without waiting, and does nothing unless early speculation is configured.
func (e *Executor) SpeculateEarly(b PreparedBlock) {
	if e.earlySlots == nil || !e.useOCC(len(b.Txs)) {
		return
	}
	number := b.Context.Number
	// Read in this order so that the state holds at least base's writes: a block finishing in between
	// only adds writes that the check at execution treats as changes.
	base := e.lastExecuted.Load()
	if number <= base {
		return
	}
	pending := e.pipelinePendingIndex()
	view := e.stateStore.OpenView()
	if view == nil {
		return
	}
	source := pending.overlay(gigaSnapshotStateReader{snapshot: view, missingState: e.missingState})

	s := &earlySpeculation{
		block:   b,
		base:    base,
		results: make([]occTxExecution, len(b.Txs)),
		done:    make([]atomic.Bool, len(b.Txs)),
	}
	workers := cap(e.earlySlots)
	e.earlyMu.Lock()
	if e.closed.Load() || e.early[number] != nil {
		e.earlyMu.Unlock()
		view.Close()
		return
	}
	e.early[number] = s
	s.wg.Add(workers)
	e.earlyMu.Unlock()

	for range workers {
		go s.run(e, source)
	}
	go func() {
		s.wg.Wait()
		view.Close()
	}()
}

// run speculates claimed chunks of the block until every transaction is claimed or the speculation
// is stopped, holding one of the executor's early slots per chunk.
func (s *earlySpeculation) run(e *Executor, source StateReader) {
	defer s.wg.Done()
	runner := newOCCSpeculativeRunner(e, s.block)
	stateDB := e.acquireStateDB(nil)
	defer e.releaseStateDB(stateDB)
	evm := vm.NewEVM(runner.blockCtx, stateDB, runner.chainConfig, vm.Config{}, e.customContracts)
	stateDB.SetEVM(evm)
	ctx := context.Background()
	n := len(s.block.Txs)
	for {
		start := int(s.claimed.Add(earlyChunk)) - earlyChunk
		if start >= n || s.stop.Load() {
			return
		}
		e.earlySlots <- struct{}{}
		for i := start; i < min(start+earlyChunk, n) && !s.stop.Load(); i++ {
			task := occExecutionTask{
				txIndex:     i,
				txIndexUint: uint(i), //nolint:gosec // i is a non-negative transaction index.
				source:      source,
				gasLimit:    runner.blockGasLimit,
			}
			if err := runner.executeTaskInto(ctx, evm, stateDB, task, s.results); err != nil {
				break
			}
			s.done[i].Store(true)
		}
		<-e.earlySlots
	}
}

// recordExecutedChanges keeps the changes of the block whose commit just started, for checking early
// results against, and marks it the last block executed.
func (e *Executor) recordExecutedChanges(blockNumber uint64, changes *pendingIndex) {
	if e.earlySlots == nil {
		return
	}
	e.earlyMu.Lock()
	e.recentChanges[blockNumber] = changes
	for number := range e.recentChanges {
		if number+earlyRecentBlocks <= blockNumber {
			delete(e.recentChanges, number)
		}
	}
	e.earlyMu.Unlock()
	e.lastExecuted.Store(blockNumber)
}

// reuseEarlySpeculation fills results with the early speculation of req's block that no block executed
// since invalidated, and reports which results it filled. It returns nil when the block was not
// speculated early, or was speculated for another context or against state too old to check.
func (e *Executor) reuseEarlySpeculation(ctx context.Context, req PreparedBlock, results []occTxExecution) []bool {
	if e.earlySlots == nil {
		return nil
	}
	number := req.Context.Number
	e.earlyMu.Lock()
	s := e.early[number]
	for other, speculation := range e.early {
		if other <= number {
			speculation.stop.Store(true)
			delete(e.early, other)
		}
	}
	var between []*pendingIndex
	checkable := s != nil
	for block := s.baseOrZero() + 1; checkable && block < number; block++ {
		changes := e.recentChanges[block]
		checkable = changes != nil
		between = append(between, changes)
	}
	e.earlyMu.Unlock()
	if s == nil {
		return nil
	}
	s.wg.Wait()
	if !checkable || len(s.results) != len(results) || !sameBlockContext(s.block.Context, req.Context) {
		recordEarlyOutcome(ctx, earlyOutcomeDiscarded, len(results))
		return nil
	}
	for _, changes := range between {
		<-changes.ready
	}

	filled := make([]bool, len(results))
	var reused, stale, unfinished atomic.Int64
	chunks := (len(results) + earlyChunk*8 - 1) / (earlyChunk * 8)
	err := e.occPool.Run(ctx, chunks, func(workerCtx context.Context, workerID int, workers int) error {
		for c := workerID; c < chunks; c += workers {
			for i := c * earlyChunk * 8; i < min((c+1)*earlyChunk*8, len(results)); i++ {
				switch {
				case !s.done[i].Load() || s.block.Txs[i].Tx.Hash() != req.Txs[i].Tx.Hash():
					unfinished.Add(1)
				case readsAnyChange(s.results[i].readSet, between):
					stale.Add(1)
				default:
					results[i] = s.results[i]
					filled[i] = true
					reused.Add(1)
				}
			}
		}
		return workerCtx.Err()
	})
	if err != nil {
		clear(results)
		return nil
	}
	recordEarlyOutcome(ctx, earlyOutcomeReused, int(reused.Load()))
	recordEarlyOutcome(ctx, earlyOutcomeStale, int(stale.Load()))
	recordEarlyOutcome(ctx, earlyOutcomeUnfinished, int(unfinished.Load()))
	return filled
}

func (s *earlySpeculation) baseOrZero() uint64 {
	if s == nil {
		return 0
	}
	return s.base
}

// stopEarlySpeculation stops every early speculation and waits for its workers.
func (e *Executor) stopEarlySpeculation() {
	if e.earlySlots == nil {
		return
	}
	e.earlyMu.Lock()
	stopped := make([]*earlySpeculation, 0, len(e.early))
	for number, s := range e.early {
		s.stop.Store(true)
		stopped = append(stopped, s)
		delete(e.early, number)
	}
	e.earlyMu.Unlock()
	for _, s := range stopped {
		s.wg.Wait()
	}
}

// readsAnyChange reports whether a transaction that read reads would read a value one of blocks
// changed.
func readsAnyChange(reads map[stateAccessKey]struct{}, blocks []*pendingIndex) bool {
	for key := range reads {
		for _, changes := range blocks {
			if key.kind == stateAccessStorage {
				if _, ok := changes.storage[storageChangeKey{address: key.address, key: key.slot}]; ok {
					return true
				}
				continue
			}
			if _, ok := changes.accounts[key.address]; ok {
				return true
			}
		}
	}
	return false
}

// sameBlockContext reports whether a and b give execution the same block constants.
func sameBlockContext(a BlockContext, b BlockContext) bool {
	return a.Number == b.Number && a.Time == b.Time && a.GasLimit == b.GasLimit &&
		a.Coinbase == b.Coinbase && a.ParentHash == b.ParentHash && a.BlockHash == b.BlockHash &&
		a.PrevRandao == b.PrevRandao && sameOptionalBig(a.ChainID, b.ChainID) &&
		sameOptionalBig(a.BaseFee, b.BaseFee) && sameOptionalBig(a.BlobBaseFee, b.BlobBaseFee)
}

// sameOptionalBig reports whether a and b hold the same value, treating nil as its own value.
func sameOptionalBig(a *big.Int, b *big.Int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Cmp(b) == 0
}

package evmonly

import (
	"context"
	"math/big"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
)

// speculationChangeBlocks is how many executed blocks' changes are kept for checking early results.
// A block speculated against state further behind than this is discarded rather than checked.
const speculationChangeBlocks = 5

// speculationBatch is how many transactions one early-speculation worker claims at a time.
const speculationBatch = 8

// speculationReuseBatch is how many transactions one OCC task checks when choosing results to reuse.
const speculationReuseBatch = speculationBatch * 8

// speculationState is the executor's early-speculation bookkeeping.
type speculationState struct {
	// blocks holds each block being speculated ahead of execution, by block number.
	blocks map[uint64]*earlySpeculation
	// recentChanges indexes the changes of the last blocks executed, by block number.
	recentChanges map[uint64]*pendingIndex
}

// earlySpeculation is a block's transactions speculated before the block executes.
type earlySpeculation struct {
	block PreparedBlock
	// base is the last block executed when the state was read; the state holds at least its writes.
	base    uint64
	results []occTxExecution
	// done[i] reports that results[i] holds a finished speculation.
	done    []atomic.Bool
	claimed atomic.Int64
	// cancel stops the speculation's workers.
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// SpeculateEarly starts speculating b's transactions against the state the newest executed block
// left, so that executing b reuses every result no block executed in between invalidated. It returns
// without waiting, and does nothing unless early speculation is configured. The speculation stops
// when ctx ends or the block executes.
func (e *Executor) SpeculateEarly(ctx context.Context, b PreparedBlock) {
	if e.speculationSlots == nil || !e.useOCC(len(b.Txs)) {
		return
	}
	number := b.Context.Number
	// Read in this order so that the state holds at least base's writes: a block finishing in between
	// only adds writes that the check at execution treats as changes.
	base := e.lastExecuted.Load()
	if number <= base {
		return
	}
	pending := e.pipelinePending()
	view := e.stateStore.OpenView()
	if view == nil {
		return
	}
	source := pending.overlay(gigaSnapshotStateReader{snapshot: view, missingState: e.missingState})

	ctx, cancel := context.WithCancel(ctx)
	s := &earlySpeculation{
		block:   b,
		base:    base,
		results: make([]occTxExecution, len(b.Txs)),
		done:    make([]atomic.Bool, len(b.Txs)),
		cancel:  cancel,
	}
	for state := range e.speculation.Lock() {
		if e.closed.Load() || state.blocks[number] != nil {
			cancel()
			view.Close()
			return
		}
		state.blocks[number] = s
		// Started under the lock, so a caller that finds s can wait for it.
		s.wg.Go(func() {
			defer view.Close()
			var workers sync.WaitGroup
			for range e.cfg.EarlySpeculationWorkers {
				workers.Go(func() { s.run(ctx, e, source) })
			}
			workers.Wait()
		})
	}
}

// run speculates claimed chunks of the block until every transaction is claimed or ctx ends,
// holding one of the executor's early slots per chunk.
func (s *earlySpeculation) run(ctx context.Context, e *Executor, source StateReader) {
	runner := newOCCSpeculativeRunner(e, s.block)
	stateDB := e.acquireStateDB(nil)
	defer e.releaseStateDB(stateDB)
	evm := vm.NewEVMWithCustomPrecompiles(runner.blockCtx, stateDB, runner.chainConfig, vm.Config{}, e.customContracts)
	stateDB.SetEVM(evm)
	n := len(s.block.Txs)
	for {
		start := int(s.claimed.Add(speculationBatch)) - speculationBatch
		if start >= n {
			return
		}
		release, err := e.speculationSlots.Acquire(ctx)
		if err != nil {
			return
		}
		for i := start; i < min(start+speculationBatch, n) && ctx.Err() == nil; i++ {
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
		release()
	}
}

// recordExecutedChanges keeps the changes of the block whose commit just started, for checking early
// results against, and marks it the last block executed.
func (e *Executor) recordExecutedChanges(blockNumber uint64, changes *pendingIndex) {
	if e.speculationSlots == nil {
		return
	}
	for state := range e.speculation.Lock() {
		state.recentChanges[blockNumber] = changes
		for number := range state.recentChanges {
			if number+speculationChangeBlocks <= blockNumber {
				delete(state.recentChanges, number)
			}
		}
	}
	e.lastExecuted.Store(blockNumber)
}

// reuseEarlySpeculation fills results with the early speculation of req's block that neither a block
// executed since nor begin, the block's begin-blocker writes, invalidated, and reports which results it
// filled. It returns nil when the block was not speculated early, or was speculated for another context
// or against state too old to check.
func (e *Executor) reuseEarlySpeculation(ctx context.Context, req PreparedBlock, begin *StateChangeSet, results []occTxExecution) []bool {
	if e.speculationSlots == nil {
		return nil
	}
	number := req.Context.Number
	var s *earlySpeculation
	var between []*pendingIndex
	checkable := false
	for state := range e.speculation.Lock() {
		s = state.blocks[number]
		for other, speculation := range state.blocks {
			if other <= number {
				speculation.cancel()
				delete(state.blocks, other)
			}
		}
		checkable = s != nil
		for block := s.baseOrZero() + 1; checkable && block < number; block++ {
			changes := state.recentChanges[block]
			checkable = changes != nil
			between = append(between, changes)
		}
	}
	if s == nil {
		return nil
	}
	// The block's transactions execute after its begin blockers, which the speculation did not run.
	if begin != nil && !begin.IsEmpty() {
		between = append(between, newPendingIndex(begin))
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
	chunks := (len(results) + speculationReuseBatch - 1) / speculationReuseBatch
	err := e.occPool.Run(ctx, chunks, func(workerCtx context.Context, workerID int, workers int) error {
		for c := workerID; c < chunks; c += workers {
			for i := c * speculationReuseBatch; i < min((c+1)*speculationReuseBatch, len(results)); i++ {
				switch {
				case !s.done[i].Load() || s.block.Txs[i].Tx.Hash() != req.Txs[i].Tx.Hash():
					unfinished.Add(1)
				// A write without a read carries the old value as its prior, so writes are checked too.
				case touchesAnyChange(s.results[i].readSet, between) || touchesAnyChange(s.results[i].writeSet, between):
					stale.Add(1)
				default:
					results[i] = rebaseCredits(s.results[i], between)
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
	if e.speculationSlots == nil {
		return
	}
	var stopped []*earlySpeculation
	for state := range e.speculation.Lock() {
		for number, s := range state.blocks {
			s.cancel()
			stopped = append(stopped, s)
			delete(state.blocks, number)
		}
	}
	for _, s := range stopped {
		s.wg.Wait()
	}
}

// touchesAnyChange reports whether any of keys names state one of blocks changed.
func touchesAnyChange(keys map[stateAccessKey]struct{}, blocks []*pendingIndex) bool {
	for key := range keys {
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

// rebaseCredits returns result with each account it only credited, and so never read, restarted from the
// state the newest of blocks left it in. The speculation saw the account as an older block left it.
func rebaseCredits(result occTxExecution, blocks []*pendingIndex) occTxExecution {
	var accounts []AccountChange
	for i, change := range result.changeSet.Accounts {
		delta := result.commutativeBalanceDeltas[change.Address]
		if delta == nil {
			continue
		}
		if _, ok := result.writeSet[stateAccessKey{kind: stateAccessBalance, address: change.Address}]; ok {
			continue
		}
		latest, ok := newestAccountState(blocks, change.Address, change.Prior.Balance)
		if !ok {
			continue
		}
		if accounts == nil {
			accounts = slices.Clone(result.changeSet.Accounts)
		}
		accounts[i].Prior = AccountState{Balance: cloneBig(latest.Balance), Nonce: latest.Nonce, Code: cloneBytes(latest.Code)}
		accounts[i].Account = AccountState{
			Balance: new(big.Int).Add(cloneBig(latest.Balance), delta),
			Nonce:   latest.Nonce,
			Code:    cloneBytes(latest.Code),
		}
	}
	if accounts != nil {
		result.changeSet.Accounts = accounts
	}
	return result
}

// newestAccountState returns addr's state after the last of blocks that changed it, and false when
// none did. Its balance is the one the last block that wrote the balance left, or prior when none did.
func newestAccountState(blocks []*pendingIndex, addr common.Address, prior *big.Int) (AccountState, bool) {
	var state AccountState
	found := false
	for i := len(blocks) - 1; i >= 0; i-- {
		account, ok := blocks[i].accounts[addr]
		if !ok {
			continue
		}
		if !found {
			state, found = account.state, true
		}
		if account.balanceChanged {
			state.Balance = account.state.Balance
			return state, true
		}
	}
	state.Balance = prior
	return state, found
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

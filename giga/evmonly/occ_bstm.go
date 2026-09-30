package evmonly

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"math/bits"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
)

// errBlockSTMUnverified marks a final Block-STM result whose reads no longer hold against the block's
// final writes, which the frontier reruns against the exact prefix.
var errBlockSTMUnverified = errors.New("block-stm result not verified against the final writes")

// bstmVerifyChunk is how many final results one worker verifies per claim.
const bstmVerifyChunk = 64

// bstmCounts is what the Block-STM phase of one block did.
type bstmCounts struct {
	// executions counts incarnations started, including those stopped at an estimate.
	executions int64
	// estimateAborts counts executions stopped because they read an estimate.
	estimateAborts int64
	// validationAborts counts completed incarnations that failed validation.
	validationAborts int64
	// validationWaits counts executions stopped to wait for a lower write to pass validation.
	validationWaits int64
	// maxValidationWaits is the most executions of any one transaction stopped to wait for a lower
	// write to pass validation.
	maxValidationWaits int
	// maxStarts is the most executions started for any one transaction.
	maxStarts int
	// maxIncarnation is the highest incarnation any transaction started, counting from 0.
	maxIncarnation int
	// maxFailures is the most incarnations of any one transaction that failed validation.
	maxFailures int
	// maxParks is the most executions of any one transaction that stopped at an estimate.
	maxParks int
	// delayed counts incarnations held back until every transaction below them was committed.
	delayed int64
	// capped counts transactions left to the frontier at occMaxTxIncarnations.
	capped int64
	// unverified is the lowest transaction, counted from the phase's first, whose final result did not
	// verify, or the phase's length.
	unverified int
	// frontierReruns counts the reruns the frontier needed after the phase.
	frontierReruns uint64
	// narrowedAt is the execution count at which the phase narrowed to occBlockSTMNarrowWorkers, or 0.
	narrowedAt int64
}

// occBlockSTMNarrowWorkers is how many workers a Block-STM phase keeps once its aborts show the block's
// dependency chains: further workers would run mostly incarnations that abort, so they wait for the
// block to end.
const occBlockSTMNarrowWorkers = 8

// occBlockSTMNarrowAborts is the fewest aborted executions, and occBlockSTMNarrowShare the share of
// executions as a divisor, at which a Block-STM phase narrows to occBlockSTMNarrowWorkers.
const (
	occBlockSTMNarrowAborts = 64
	occBlockSTMNarrowShare  = 4
)

// runBlockSTM executes the block's transactions from first on under Block-STM on the pool, against
// prefix, the state after every transaction below first, which used gasBefore of the block's gas. It
// leaves in results, which holds the results from first on, each one's final incarnation, marked as
// executed against the exact prefix below it up to the first result the frontier must rerun.
func (r occSpeculativeRunner) runBlockSTM(ctx context.Context, pool *occWorkerPool, prefix StateReader, results []occTxExecution, first int, gasBefore uint64) (bstmCounts, error) {
	workers := min(pool.workers, len(results))
	run := newBlockSTMRun(r, prefix, first, len(results), workers)
	run.seedSenderNonces()
	err := pool.Run(ctx, workers, run.work)
	if err == nil {
		err = run.failure()
	}
	if err != nil {
		return run.counts(), err
	}
	run.raiseVerifiedPanic()
	run.handOff(results, gasBefore)
	return run.counts(), nil
}

// executeBlockBlockSTM executes the block under Block-STM from its first transaction and hands the final
// results to the frontier, which validates them against the exact prefix and reruns any that do not hold.
// It reports as conflicts the reads and writes of keys a lower result wrote, so the count is a function
// of the block.
func (e *Executor) executeBlockBlockSTM(ctx context.Context, req PreparedBlock, source StateReader, runner occSpeculativeRunner, pool *occWorkerPool) (*BlockResult, error) {
	results := make([]occTxExecution, len(req.Txs))
	e.blockPhases.SetPhase("occ_speculate")
	counts, err := runner.runBlockSTM(ctx, pool, source, results, 0, 0)
	if bstmObserve != nil {
		bstmObserve(counts)
	}
	if err != nil {
		if errors.Is(err, errOCCWorkerPoolClosed) {
			return e.executeBlockOCCSequentialFallback(ctx, req, source, occValidationResult{}, occFallbackReasonWorkerPoolClosed)
		}
		return nil, err
	}
	e.blockPhases.SetPhase("occ_validate")
	state := newBlockSTMValidationState(source)
	results, validation, err := e.validateInto(ctx, runner, pool, results, state)
	finalState := state.prefix
	if errors.Is(err, errOCCMaxIncarnation) || errors.Is(err, errOCCWorkerPoolClosed) {
		reason := validation.fallbackReason
		switch {
		case errors.Is(err, errOCCWorkerPoolClosed):
			reason = occFallbackReasonWorkerPoolClosed
		case errors.Is(err, errOCCMaxIncarnation) && reason == "":
			reason = occFallbackReasonMaxIncarnation
		}
		return e.executeBlockOCCSequentialFallback(ctx, req, source, validation, reason)
	}
	if err != nil {
		return nil, err
	}
	e.blockPhases.SetPhase("occ_merge")
	result, err := e.mergeOCCResults(ctx, results, finalState)
	if errors.Is(err, errOCCWorkerPoolClosed) {
		return e.executeBlockOCCSequentialFallback(ctx, req, source, validation, occFallbackReasonWorkerPoolClosed)
	}
	if err != nil {
		return nil, err
	}
	reported, err := reportedConflicts(ctx, pool, req.Txs, results, finalWrites(state, validation))
	if err != nil {
		result.Release()
		if errors.Is(err, errOCCWorkerPoolClosed) {
			return e.executeBlockOCCSequentialFallback(ctx, req, source, validation, occFallbackReasonWorkerPoolClosed)
		}
		return nil, err
	}
	result.OCCStats = reported.stats(false)
	return result, nil
}

// finalWrites returns the validation's write index when it holds exactly the final results' writes,
// which it does when validation reran nothing, or nil.
func finalWrites(state *blockSTMValidationState, validation occValidationResult) *stateAccessIndex {
	if validation.rerunCount > 0 {
		return nil
	}
	return state.writes
}

// reportedConflicts counts, over the block's final results, those that read or wrote a key a lower
// result wrote: the reruns a first pass against the block's start would need. writes indexes the final
// results' writes, or is nil for reportedConflicts to index them.
func reportedConflicts(ctx context.Context, pool *occWorkerPool, txs []PreparedTx, results []occTxExecution, writes *stateAccessIndex) (occValidationResult, error) {
	if writes == nil {
		writes = newStateAccessIndex()
		if err := writes.indexResults(ctx, pool, results); err != nil {
			return occValidationResult{}, err
		}
	}
	var reported occValidationResult
	if err := reported.recordLowerWriteConflicts(ctx, pool, writes, results, txs); err != nil {
		return occValidationResult{}, err
	}
	reported.validationCount = uint64(len(results)) + reported.rerunCount
	return reported, nil
}

// recordLowerWriteConflicts counts one rerun for every result that read or wrote a key a lower result
// wrote, and records each such key as a conflict, except a key of the result's own sender: a sender's
// later transaction depends on its earlier one through the nonce, a chain link rather than a conflict.
func (r *occValidationResult) recordLowerWriteConflicts(ctx context.Context, pool *occWorkerPool, writes *stateAccessIndex, results []occTxExecution, txs []PreparedTx) error {
	chunks := (len(results) + occCancellationCheckInterval - 1) / occCancellationCheckInterval
	found := make([]occValidationResult, chunks)
	var next atomic.Int64
	err := pool.Run(ctx, chunks, func(workerCtx context.Context, _ int, _ int) error {
		for chunk := int(next.Add(1)) - 1; chunk < chunks; chunk = int(next.Add(1)) - 1 {
			if err := workerCtx.Err(); err != nil {
				return err
			}
			first := chunk * occCancellationCheckInterval
			for tx := first; tx < min(first+occCancellationCheckInterval, len(results)); tx++ {
				read := found[chunk].addLowerWriteConflicts("read", writes, results[tx].readSet, tx, txs[tx].Sender)
				written := found[chunk].addLowerWriteConflicts("write", writes, results[tx].writeSet, tx, txs[tx].Sender)
				if read || written {
					found[chunk].rerunCount++
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for i := range found {
		r.add(found[i])
	}
	return nil
}

// addLowerWriteConflicts records the keys of set a result below tx wrote, other than sender's account
// fields, and reports whether set holds any such key.
func (r *occValidationResult) addLowerWriteConflicts(access string, writes *stateAccessIndex, set map[stateAccessKey]struct{}, tx int, sender common.Address) bool {
	conflicted := false
	for key := range set {
		if !writes.conflictsWithin(key, 0, tx) {
			continue
		}
		conflicted = true
		if key.address == sender && key.kind != stateAccessStorage && key.kind != stateAccessCode {
			continue
		}
		if r.conflicts == nil {
			r.conflicts = map[occConflictAggregationKey]uint64{}
		}
		r.conflictCount++
		r.conflicts[occConflictAggregationKey{access: access, kind: key.kind, address: key.address, slot: key.slot}]++
	}
	return conflicted
}

// bstmObserve, when set, receives what each Block-STM phase did.
var bstmObserve func(counts bstmCounts)

// bstmRun is one block's Block-STM phase: the multi-version memory, the scheduler, and each
// transaction's latest incarnation.
type bstmRun struct {
	runner  occSpeculativeRunner
	base    StateReader
	baseRow accountSnapshotReader
	// first is the block index of the run's transaction 0.
	first   int
	n       int
	memory  *bstmMemory
	sched   *bstmScheduler
	current []atomic.Pointer[bstmIncarnation]
	// logs holds each worker's read log, reused by every incarnation the worker runs.
	logs []bstmReadLog

	failed atomic.Pointer[error]

	verifyNext atomic.Int64
	// unverified is the lowest transaction whose final result's reads do not hold, or n.
	unverified atomic.Int64

	executions       atomic.Int64
	estimateAborts   atomic.Int64
	validationAborts atomic.Int64
	validationWaits  atomic.Int64
	// capped counts validations of a transaction left to the frontier.
	capped atomic.Int64
	// narrowedAt is the execution count at which the phase narrowed, or 0.
	narrowedAt atomic.Int64
}

// bstmIncarnation is one completed execution of a transaction. It is immutable once stored.
type bstmIncarnation struct {
	number int
	result occTxExecution
	// checked holds the reads validation re-checks: those the result's read set names, or every read
	// of an execution that panicked.
	checked []bstmRead
	written []bstmKey
	// senderNonce is the sender's nonce the incarnation read and writes back unchanged.
	senderNonce bstmValue
	// panicked is the value an execution panicked with, or nil.
	panicked any
	// coinbaseUnfolded is set when validation checks a coinbase balance served without the fee credits
	// below the transaction.
	coinbaseUnfolded bool
}

func newBlockSTMRun(runner occSpeculativeRunner, source StateReader, first int, n int, workers int) *bstmRun {
	b := &bstmRun{
		runner:  runner,
		base:    source,
		first:   first,
		n:       n,
		memory:  newBlockSTMMemory(n, runner.req.Context.Coinbase),
		sched:   newBlockSTMScheduler(n, workers),
		current: make([]atomic.Pointer[bstmIncarnation], n),
		logs:    make([]bstmReadLog, workers),
	}
	b.baseRow, _ = source.(accountSnapshotReader)
	b.unverified.Store(int64(n))
	return b
}

// seedSenderNonces marks, for every transaction whose sender sends a later one in the block, a write of
// that sender's nonce as an estimate, so the later transaction waits for the earlier one to execute
// instead of executing against a nonce it would fail on.
func (b *bstmRun) seedSenderNonces() {
	txs := b.runner.req.Txs[b.first:]
	last := make(map[common.Address]int, len(txs))
	for tx := range txs {
		sender := txs[tx].Sender
		if earlier, ok := last[sender]; ok {
			key := bstmKey{kind: bstmNonce, addr: sender}
			b.memory.put(key, earlier, -1, bstmValue{})
			b.memory.markEstimate(key, earlier)
			b.current[earlier].Store(&bstmIncarnation{number: -1, written: []bstmKey{key}})
		}
		last[sender] = tx
	}
}

// work runs scheduler tasks on one worker until the block is done, then verifies final results.
func (b *bstmRun) work(ctx context.Context, worker int, _ int) error {
	var task bstmTask
	for !b.sched.done.Load() {
		switch task.kind {
		case bstmExecute:
			task = b.execute(ctx, &b.logs[worker], task)
		case bstmValidate:
			task = b.validate(task)
		default:
			if worker >= occBlockSTMNarrowWorkers && b.narrowed() {
				b.sched.retire()
				continue
			}
			if task = b.sched.nextTask(); task.kind == bstmNoTask && !b.sched.park() {
				return b.verify()
			}
		}
	}
	return b.verify()
}

// narrowed reports whether the phase's aborted executions have reached occBlockSTMNarrowAborts and
// 1/occBlockSTMNarrowShare of its executions.
func (b *bstmRun) narrowed() bool {
	if b.narrowedAt.Load() != 0 {
		return true
	}
	aborts := b.estimateAborts.Load() + b.validationAborts.Load()
	executions := b.executions.Load()
	if aborts < occBlockSTMNarrowAborts || aborts*occBlockSTMNarrowShare < executions {
		return false
	}
	b.narrowedAt.CompareAndSwap(0, executions)
	return true
}

// execute runs an incarnation of the task's transaction until it completes or waits on a lower
// transaction, and returns the task the scheduler hands back.
func (b *bstmRun) execute(ctx context.Context, log *bstmReadLog, task bstmTask) bstmTask {
	for {
		b.sched.recordStart(task.tx)
		inc, blocking, unvalidated := b.runIncarnation(ctx, log, task)
		b.executions.Add(1)
		if blocking >= 0 && unvalidated {
			b.validationWaits.Add(1)
			if b.sched.addValidationDependency(task.tx, blocking) {
				return bstmTask{}
			}
			continue
		}
		if blocking >= 0 {
			b.estimateAborts.Add(1)
			if b.sched.addDependency(task.tx, blocking) {
				return bstmTask{}
			}
			continue
		}
		if err := ctx.Err(); err != nil {
			b.fail(err)
			return bstmTask{}
		}
		newPath := b.publish(task.tx, inc)
		return b.sched.finishExecution(task.tx, task.inc, newPath)
	}
}

// runIncarnation executes one incarnation of the task's transaction against the memory, recording its
// reads in log. It returns the writer the execution waits for, and whether it waits for that writer's
// incarnation to pass validation rather than to execute, or -1 with the completed incarnation. Every
// incarnation after a transaction's first waits for the lower writes it reads to pass validation.
func (b *bstmRun) runIncarnation(ctx context.Context, log *bstmReadLog, task bstmTask) (*bstmIncarnation, int, bool) {
	if b.sched.held(task.tx) {
		// An incarnation released from the hold runs against the committed prefix, and must not fail
		// again on a coinbase balance served without the fee credits below it.
		b.memory.foldCoinbase[task.tx].Store(true)
	}
	reader := &bstmReader{memory: b.memory, base: b.base, baseRow: b.baseRow, tx: task.tx, bstmReadLog: log.reset()}
	if task.inc > 0 {
		reader.validatedAt = b.sched.validatedAt
	}
	defer func() { *log = reader.bstmReadLog }()
	inc := &bstmIncarnation{number: task.inc}
	blocking := -1
	unvalidated := false
	func() {
		defer func() {
			if p := recover(); p != nil {
				if blocked, ok := p.(bstmBlocked); ok {
					blocking = int(blocked)
					return
				}
				if writer, ok := p.(bstmUnvalidated); ok {
					blocking, unvalidated = int(writer), true
					return
				}
				// A speculative execution can read a mix of versions and fail where the sequential
				// path would not; validation decides whether the panic is genuine.
				inc.panicked = p
			}
		}()
		var err error
		txIndex := b.first + task.tx
		inc.result, err = b.runner.executeTx(ctx, reader, txIndex, uint(txIndex), b.runner.blockGasLimit) //nolint:gosec // txIndex is non-negative.
		if err != nil {
			inc.result.err = err
		}
	}()
	if blocking >= 0 {
		return nil, blocking, unvalidated
	}
	if inc.panicked != nil {
		inc.result = occTxExecution{err: fmt.Errorf("execute tx %d: panic: %v", b.first+task.tx, inc.panicked)}
	}
	inc.result.incarnation = 0
	inc.result.sourcePrefix = b.first + task.tx
	inc.result.shards = inc.result.touchedShards()
	inc.checked = checkedReads(reader.reads, &inc.result, inc.panicked != nil || inc.result.err != nil)
	inc.coinbaseUnfolded = reader.coinbaseUnfolded && slices.ContainsFunc(inc.checked, func(read bstmRead) bool {
		return read.key == bstmKey{kind: bstmBalance, addr: b.memory.coinbase}
	})
	inc.written = inc.writes(reader, b.runner.req.Txs[b.first+task.tx].Sender)
	return inc, -1, false
}

// checkedReads returns, in a slice of its own, the reads validation re-checks: every read when all is
// set, as it is for an execution that stopped before its read set was complete, otherwise those the
// result's read set names.
func checkedReads(reads []bstmRead, result *occTxExecution, all bool) []bstmRead {
	if all {
		return slices.Clone(reads)
	}
	named := make([]bool, len(reads))
	count := 0
	for i := range reads {
		named[i] = reads[i].namedIn(result.readSet)
		if named[i] {
			count++
		}
	}
	checked := make([]bstmRead, 0, count)
	for i := range reads {
		if named[i] {
			checked = append(checked, reads[i])
		}
	}
	return checked
}

// writes returns the keys the incarnation writes. A sender's nonce the incarnation read and left
// unchanged is written back at the value read, so the sender's later transactions read it through this
// one and wait for it once it is aborted.
func (inc *bstmIncarnation) writes(reader *bstmReader, sender common.Address) []bstmKey {
	result := &inc.result
	changes := &result.changeSet
	keys := make([]bstmKey, 0, len(changes.Balances)+len(result.commutativeBalanceDeltas)+len(changes.Nonces)+len(changes.Code)+len(changes.StorageClears)+len(changes.Storage)+1)
	for _, change := range changes.Balances {
		if !result.isFeeCredit(change.Address) {
			keys = append(keys, bstmKey{kind: bstmBalance, addr: change.Address})
		}
	}
	for addr, delta := range result.commutativeBalanceDeltas {
		if delta != nil && delta.Sign() != 0 {
			keys = append(keys, bstmKey{kind: bstmDelta, addr: addr})
		}
	}
	senderWritten := false
	for _, change := range changes.Nonces {
		keys = append(keys, bstmKey{kind: bstmNonce, addr: change.Address})
		senderWritten = senderWritten || change.Address == sender
	}
	for _, change := range changes.Code {
		keys = append(keys, bstmKey{kind: bstmCode, addr: change.Address})
	}
	for _, addr := range changes.StorageClears {
		keys = append(keys, bstmKey{kind: bstmClear, addr: addr})
	}
	for _, change := range changes.Storage {
		keys = append(keys, bstmKey{kind: bstmSlot, addr: change.Address, slot: change.Key})
	}
	senderKey := bstmKey{kind: bstmNonce, addr: sender}
	if read, ok := reader.cached(senderKey); ok && !senderWritten {
		keys = append(keys, senderKey)
		inc.senderNonce = read.value
	}
	return keys
}

// publish writes the incarnation's values into the memory, removes the ones its previous incarnation
// wrote and this one does not, and makes it the transaction's current incarnation. It reports whether
// the incarnation wrote a key the previous one did not.
func (b *bstmRun) publish(tx int, inc *bstmIncarnation) bool {
	newPath := false
	for _, key := range inc.written {
		if b.memory.put(key, tx, inc.number, inc.valueOf(key)) {
			newPath = true
		}
	}
	if previous := b.current[tx].Load(); previous != nil {
		for _, key := range staleKeys(previous.written, inc.written) {
			b.memory.remove(key, tx)
		}
	}
	b.current[tx].Store(inc)
	return newPath
}

// valueOf returns the value the incarnation writes to key.
func (inc *bstmIncarnation) valueOf(key bstmKey) bstmValue {
	result := &inc.result
	changes := &result.changeSet
	switch key.kind {
	case bstmBalance:
		for _, change := range changes.Balances {
			if change.Address == key.addr {
				return bstmValue{balance: change.Balance}
			}
		}
	case bstmDelta:
		return bstmValue{balance: result.commutativeBalanceDeltas[key.addr]}
	case bstmNonce:
		for _, change := range changes.Nonces {
			if change.Address == key.addr {
				return bstmValue{nonce: change.Nonce}
			}
		}
		return inc.senderNonce
	case bstmCode:
		for _, change := range changes.Code {
			if change.Address == key.addr {
				if change.Delete {
					return bstmValue{}
				}
				return bstmValue{code: change.Code}
			}
		}
	case bstmSlot:
		for _, change := range changes.Storage {
			if change.Address == key.addr && change.Key == key.slot {
				if change.Delete {
					return bstmValue{}
				}
				return bstmValue{slot: change.Value}
			}
		}
	}
	return bstmValue{}
}

// bstmStaleScan is the largest product of two write sets staleKeys compares pairwise.
const bstmStaleScan = 1024

// staleKeys returns the keys of previous that current does not hold.
func staleKeys(previous []bstmKey, current []bstmKey) []bstmKey {
	var stale []bstmKey
	if len(previous)*len(current) <= bstmStaleScan {
		for _, key := range previous {
			if !slices.Contains(current, key) {
				stale = append(stale, key)
			}
		}
		return stale
	}
	held := make(map[bstmKey]struct{}, len(current))
	for _, key := range current {
		held[key] = struct{}{}
	}
	for _, key := range previous {
		if _, ok := held[key]; !ok {
			stale = append(stale, key)
		}
	}
	return stale
}

// validate re-checks the task's incarnation against the memory and returns the task the scheduler
// hands back. A failed incarnation's writes become estimates.
func (b *bstmRun) validate(task bstmTask) bstmTask {
	inc := b.current[task.tx].Load()
	if inc == nil || inc.number != task.inc {
		// A newer incarnation replaced the one this task names; its own validation follows.
		b.sched.active.Add(-1)
		return bstmTask{}
	}
	if b.holds(task.tx, inc) {
		b.sched.markValidated(task.tx, task.inc, task.wave)
		b.releaseValidated(task.tx)
		b.sched.tryCommit()
		return b.sched.finishValidation(task.tx, bstmNotAborted)
	}
	outcome := b.sched.tryValidationAbort(task.tx, task.inc)
	switch outcome {
	case bstmCapped:
		b.leaveToFrontier(task.tx)
	case bstmAborted, bstmAbortedDelayed:
		b.validationAborts.Add(1)
		if inc.coinbaseUnfolded {
			b.memory.foldCoinbase[task.tx].Store(true)
		}
		for _, key := range inc.written {
			b.memory.markEstimate(key, task.tx)
		}
	}
	return b.sched.finishValidation(task.tx, outcome)
}

// releaseValidated records that tx's executed incarnation passed validation, for the later incarnations
// above it that read its writes, and releases those that waited for it.
func (b *bstmRun) releaseValidated(tx int) {
	s := b.sched
	inc := b.current[tx].Load()
	st := &s.txs[tx]
	st.mu.Lock()
	if inc == nil || st.status != bstmExecuted || st.incarnation != inc.number || st.validatedInc != inc.number {
		st.mu.Unlock()
		return
	}
	st.passed.Store(int64(inc.number) + 1)
	waiters := st.validationWaiters
	st.validationWaiters = nil
	st.mu.Unlock()
	s.releaseValidationWaiters(waiters)
}

// leaveToFrontier stops re-executing tx in the phase. Its latest result stays in the memory for the
// transactions above it, and the frontier reruns it against the exact prefix and validates every result
// above it against the writes from it on.
func (b *bstmRun) leaveToFrontier(tx int) {
	lowerStop(&b.unverified, int64(tx))
	b.capped.Add(1)
}

// holds reports whether every read the incarnation checks returns the same value from the memory as it
// stands for tx.
func (b *bstmRun) holds(tx int, inc *bstmIncarnation) bool {
	for i := range inc.checked {
		if !b.memory.readHolds(&inc.checked[i], tx, b.base) {
			return false
		}
	}
	return true
}

// verify re-checks, once the scheduler is done, every final result against the final memory a chunk
// at a time, and records the lowest one that no longer holds.
func (b *bstmRun) verify() error {
	if b.failure() != nil {
		return nil
	}
	for {
		first := int(b.verifyNext.Add(bstmVerifyChunk)) - bstmVerifyChunk
		if first >= b.n {
			return nil
		}
		for tx := first; tx < min(first+bstmVerifyChunk, b.n); tx++ {
			// A transaction still delayed when the block ended, behind one left to the frontier, has no
			// incarnation that completed.
			if inc := b.current[tx].Load(); inc == nil || inc.number < 0 || !b.holds(tx, inc) {
				lowerStop(&b.unverified, int64(tx))
				break
			}
		}
	}
}

// raiseVerifiedPanic re-raises the panic of the lowest final incarnation that panicked, if its reads
// hold: the sequential path reaches the same panic.
func (b *bstmRun) raiseVerifiedPanic() {
	for tx := range int(b.unverified.Load()) {
		if inc := b.current[tx].Load(); inc.panicked != nil {
			panic(inc.panicked)
		}
	}
}

// handOff copies each final result into results. From the lowest result that did not verify or does
// not fit the block's gas on, the results are marked for the frontier to validate against the writes
// above that result, and that result itself to rerun against the exact prefix.
func (b *bstmRun) handOff(results []occTxExecution, gasBefore uint64) {
	for tx := range results {
		if inc := b.current[tx].Load(); inc != nil && inc.number >= 0 {
			results[tx] = inc.result
		} else {
			results[tx] = occTxExecution{err: fmt.Errorf("tx %d: %w", b.first+tx, errBlockSTMUnverified)}
		}
	}
	first := min(int(b.unverified.Load()), firstGasFailure(results, gasBefore, b.runner.blockGasLimit))
	if first >= len(results) {
		return
	}
	if first == int(b.unverified.Load()) {
		results[first].err = fmt.Errorf("tx %d: %w", b.first+first, errBlockSTMUnverified)
	}
	// A source prefix below the result's own index sends it to rerun against the exact prefix.
	results[first].sourcePrefix = b.first + first - 1
	for tx := first + 1; tx < len(results); tx++ {
		results[tx].sourcePrefix = b.first + first
	}
}

// firstGasFailure returns the lowest index whose result does not fit the block gas left by gasBefore
// and the results before it, or len(results).
func firstGasFailure(results []occTxExecution, gasBefore uint64, gasLimit uint64) int {
	cumulative := gasBefore
	for tx := range results {
		if _, err := stmGasFailure(results[tx], cumulative, gasLimit); err != nil {
			return tx
		}
		cumulative += results[tx].gasUsed
	}
	return len(results)
}

// fail halts the run with err, unless it already failed.
func (b *bstmRun) fail(err error) {
	if b.failed.CompareAndSwap(nil, &err) {
		b.sched.halt()
	}
}

// failure returns the error that halted the run, or nil.
func (b *bstmRun) failure() error {
	if err := b.failed.Load(); err != nil {
		return *err
	}
	return nil
}

func (b *bstmRun) counts() bstmCounts {
	counts := bstmCounts{
		executions:       b.executions.Load(),
		estimateAborts:   b.estimateAborts.Load(),
		validationAborts: b.validationAborts.Load(),
		validationWaits:  b.validationWaits.Load(),
		delayed:          b.sched.delayedCount.Load(),
		unverified:       int(b.unverified.Load()),
		capped:           b.capped.Load(),
		narrowedAt:       b.narrowedAt.Load(),
	}
	for tx := range b.sched.txs {
		counts.maxIncarnation = max(counts.maxIncarnation, b.sched.txs[tx].incarnation)
		counts.maxFailures = max(counts.maxFailures, b.sched.txs[tx].failures)
		counts.maxParks = max(counts.maxParks, b.sched.txs[tx].parks)
		counts.maxValidationWaits = max(counts.maxValidationWaits, b.sched.txs[tx].validationWaits)
		counts.maxStarts = max(counts.maxStarts, b.sched.txs[tx].starts)
	}
	return counts
}

// add sums other's counts and conflicts into r, counting a validation for each of other's reruns.
func (r *occValidationResult) add(other occValidationResult) {
	r.rerunCount += other.rerunCount
	r.validationCount += other.validationCount + other.rerunCount
	r.conflictCount += other.conflictCount
	if other.rerunCount > 0 {
		r.maxIncarnation = max(r.maxIncarnation, 1)
	}
	for key, count := range other.conflicts {
		if r.conflicts == nil {
			r.conflicts = map[occConflictAggregationKey]uint64{}
		}
		r.conflicts[key] += count
	}
}

// bstmKind is the kind of state a memory key names.
type bstmKind uint8

const (
	bstmBalance bstmKind = iota
	bstmNonce
	bstmCode
	bstmSlot
	// bstmClear is an address's storage cleared.
	bstmClear
	// bstmDelta is a commutative change of an address's balance.
	bstmDelta
)

// bstmKey names one piece of state in the memory.
type bstmKey struct {
	kind bstmKind
	addr common.Address
	slot common.Hash
}

func (k bstmKey) shard() int {
	return (int(k.addr[19]) ^ int(k.addr[7])<<1 ^ int(k.slot[31]) ^ int(k.slot[3])<<2 ^ int(k.kind)*37) % bstmShards
}

// bstmValue is the value of one key: a balance or delta, a nonce, code (nil for none), or a slot.
type bstmValue struct {
	balance *big.Int
	nonce   uint64
	code    []byte
	slot    common.Hash
}

func (v bstmValue) equal(kind bstmKind, other bstmValue) bool {
	switch kind {
	case bstmBalance, bstmDelta:
		return v.balance.Cmp(other.balance) == 0
	case bstmNonce:
		return v.nonce == other.nonce
	case bstmCode:
		return bytes.Equal(v.code, other.code)
	default:
		return v.slot == other.slot
	}
}

// bstmVersion names the incarnation of the transaction that wrote a value; tx -1 is the block's
// start state.
type bstmVersion struct {
	tx  int
	inc int
}

var bstmBaseVersion = bstmVersion{tx: -1}

// bstmEntry is one transaction's write of a key.
type bstmEntry struct {
	version  bstmVersion
	estimate bool
	value    bstmValue
}

// bstmShards is the number of shards the memory's key index is split into.
const bstmShards = 256

// bstmMemory is the block's multi-version memory: for every key, each transaction's latest write
// in transaction order.
type bstmMemory struct {
	shards [bstmShards]bstmShard
	// coinbase is the block's fee recipient. Its fee credits are held per transaction in fees rather
	// than in a cell, since every transaction writes one.
	coinbase common.Address
	fees     []atomic.Pointer[bstmFee]
	// foldCoinbase marks a transaction whose earlier incarnation failed validation on a coinbase
	// balance served without the fee credits below it.
	foldCoinbase []atomic.Bool
}

// bstmFee is one transaction's fee credit to the coinbase.
type bstmFee struct {
	amount   *big.Int
	estimate bool
}

func newBlockSTMMemory(n int, coinbase common.Address) *bstmMemory {
	return &bstmMemory{
		coinbase:     coinbase,
		fees:         make([]atomic.Pointer[bstmFee], n),
		foldCoinbase: make([]atomic.Bool, n),
	}
}

// isCoinbaseFee reports whether key is the coinbase's fee credits.
func (m *bstmMemory) isCoinbaseFee(key bstmKey) bool {
	return key.kind == bstmDelta && key.addr == m.coinbase
}

type bstmShard struct {
	// cells maps a key to its *bstmCell. A cell, once stored, is never replaced, so a read finds it
	// without a lock.
	cells sync.Map
	_     [40]byte
}

// bstmCell holds one key's writes in transaction order. A delta cell also caches running sums of
// its writes.
type bstmCell struct {
	mu        sync.Mutex
	entries   []bstmEntry
	estimates int
	// sums[i] is the sum of the first i entries' deltas, for i <= summed.
	sums   []big.Int
	summed int
}

func (m *bstmMemory) cell(key bstmKey, create bool) *bstmCell {
	shard := &m.shards[key.shard()]
	if c, ok := shard.cells.Load(key); ok {
		return c.(*bstmCell)
	}
	if !create {
		return nil
	}
	c, _ := shard.cells.LoadOrStore(key, &bstmCell{})
	return c.(*bstmCell)
}

// find returns the position of tx's entry, or where it would be inserted.
func (c *bstmCell) find(tx int) (int, bool) {
	return slices.BinarySearchFunc(c.entries, tx, func(e bstmEntry, tx int) int { return e.version.tx - tx })
}

// put records tx's write of key and reports whether tx had no entry for it before.
func (m *bstmMemory) put(key bstmKey, tx int, inc int, value bstmValue) bool {
	if m.isCoinbaseFee(key) {
		return m.fees[tx].Swap(&bstmFee{amount: value.balance}) == nil
	}
	c := m.cell(key, true)
	c.mu.Lock()
	defer c.mu.Unlock()
	i, found := c.find(tx)
	entry := bstmEntry{version: bstmVersion{tx: tx, inc: inc}, value: value}
	c.summed = min(c.summed, i)
	if found {
		if c.entries[i].estimate {
			c.estimates--
		}
		c.entries[i] = entry
		return false
	}
	c.entries = slices.Insert(c.entries, i, entry)
	return true
}

func (m *bstmMemory) remove(key bstmKey, tx int) {
	if m.isCoinbaseFee(key) {
		m.fees[tx].Store(nil)
		return
	}
	c := m.cell(key, false)
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if i, found := c.find(tx); found {
		if c.entries[i].estimate {
			c.estimates--
		}
		c.entries = slices.Delete(c.entries, i, i+1)
		c.summed = min(c.summed, i)
	}
}

func (m *bstmMemory) markEstimate(key bstmKey, tx int) {
	if m.isCoinbaseFee(key) {
		if fee := m.fees[tx].Load(); fee != nil {
			m.fees[tx].Store(&bstmFee{amount: fee.amount, estimate: true})
		}
		return
	}
	c := m.cell(key, false)
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if i, found := c.find(tx); found && !c.entries[i].estimate {
		c.entries[i].estimate = true
		c.estimates++
	}
}

// below returns the entry of the highest transaction under tx that wrote key.
func (m *bstmMemory) below(key bstmKey, tx int) (bstmEntry, bool) {
	c := m.cell(key, false)
	if c == nil {
		return bstmEntry{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	i, _ := c.find(tx)
	if i == 0 {
		return bstmEntry{}, false
	}
	return c.entries[i-1], true
}

// deltaBetween returns the sum of addr's deltas written by the transactions in (after, before), nil
// for none. Unless through is set, it returns instead the first such transaction whose delta is an
// estimate.
func (m *bstmMemory) deltaBetween(addr common.Address, after int, before int, through bool) (*big.Int, int) {
	key := bstmKey{kind: bstmDelta, addr: addr}
	if m.isCoinbaseFee(key) {
		return m.feesBetween(after, before, through)
	}
	c := m.cell(key, false)
	if c == nil {
		return nil, -1
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	lo, _ := c.find(after + 1)
	hi, _ := c.find(before)
	if lo >= hi {
		return nil, -1
	}
	if !through && c.estimates > 0 {
		for i := lo; i < hi; i++ {
			if c.entries[i].estimate {
				return nil, c.entries[i].version.tx
			}
		}
	}
	c.sumTo(hi)
	return new(big.Int).Sub(&c.sums[hi], &c.sums[lo]), -1
}

// feesBetween is deltaBetween for the coinbase's fee credits.
func (m *bstmMemory) feesBetween(after int, before int, through bool) (*big.Int, int) {
	var sum *big.Int
	for tx := after + 1; tx < before; tx++ {
		fee := m.fees[tx].Load()
		if fee == nil {
			continue
		}
		if fee.estimate && !through {
			return nil, tx
		}
		if sum == nil {
			sum = new(big.Int)
		}
		sum.Add(sum, fee.amount)
	}
	return sum, -1
}

// sumTo extends the cached running sums to cover the first hi entries.
func (c *bstmCell) sumTo(hi int) {
	if len(c.sums) < hi+1 {
		c.sums = append(c.sums, make([]big.Int, hi+1-len(c.sums))...)
	}
	for i := c.summed; i < hi; i++ {
		c.sums[i+1].Add(&c.sums[i], c.entries[i].value.balance)
	}
	c.summed = max(c.summed, hi)
}

// bstmBalance is an address's balance as one transaction sees it: the latest absolute write below
// it, or the block's start balance, plus every delta after that write.
type bstmBalanceRead struct {
	value   *big.Int
	version bstmVersion
	// base is the start balance the value was built on, when version is bstmBaseVersion.
	base *big.Int
}

// balance returns addr's balance as tx sees it, building on base() when no transaction below tx wrote
// it. Unless through is set, it returns instead the writer of the first estimate it meets.
func (m *bstmMemory) balance(addr common.Address, tx int, through bool, base func() *big.Int) (bstmBalanceRead, int) {
	read := bstmBalanceRead{version: bstmBaseVersion}
	entry, written := m.below(bstmKey{kind: bstmBalance, addr: addr}, tx)
	if written {
		if entry.estimate && !through {
			return read, entry.version.tx
		}
		read.version, read.value = entry.version, entry.value.balance
	} else {
		read.base = base()
		read.value = read.base
	}
	if read.value == nil {
		read.value = new(big.Int)
	}
	delta, blocked := m.deltaBetween(addr, read.version.tx, tx, through)
	if blocked >= 0 {
		return read, blocked
	}
	if delta != nil {
		read.value = delta.Add(delta, read.value)
	}
	return read, -1
}

// slot returns the entry that decides addr's slot as tx sees it: its latest write or the latest clear of
// addr's storage below tx, whichever is higher, with the slot's value.
func (m *bstmMemory) slot(addr common.Address, slot common.Hash, tx int) (bstmEntry, bool) {
	written, slotWritten := m.below(bstmKey{kind: bstmSlot, addr: addr, slot: slot}, tx)
	cleared, wasCleared := m.below(bstmKey{kind: bstmClear, addr: addr}, tx)
	switch {
	case wasCleared && (!slotWritten || cleared.version.tx > written.version.tx):
		cleared.value = bstmValue{}
		return cleared, true
	case slotWritten:
		return written, true
	default:
		return bstmEntry{}, false
	}
}

// entry returns the entry that decides key as tx sees it.
func (m *bstmMemory) entry(key bstmKey, tx int) (bstmEntry, bool) {
	if key.kind == bstmSlot {
		return m.slot(key.addr, key.slot, tx)
	}
	return m.below(key, tx)
}

// readHolds reports whether read returns the same value for tx from the memory as it stands, with no
// estimate on the way.
func (m *bstmMemory) readHolds(read *bstmRead, tx int, base StateReader) bool {
	if read.key.kind == bstmBalance {
		addr := read.key.addr
		now, blocked := m.balance(addr, tx, false, func() *big.Int {
			if read.version == bstmBaseVersion {
				return read.base
			}
			return base.GetBalance(addr)
		})
		return blocked < 0 && now.value.Cmp(read.value.balance) == 0
	}
	entry, written := m.entry(read.key, tx)
	if !written {
		if read.version == bstmBaseVersion {
			return true
		}
		return baseValue(read.key, base).equal(read.key.kind, read.value)
	}
	if entry.estimate {
		return false
	}
	return entry.version == read.version || entry.value.equal(read.key.kind, read.value)
}

// baseValue returns key's value in the block's start state.
func baseValue(key bstmKey, base StateReader) bstmValue {
	switch key.kind {
	case bstmNonce:
		return bstmValue{nonce: base.GetNonce(key.addr)}
	case bstmCode:
		return bstmValue{code: base.GetCode(key.addr)}
	default:
		return bstmValue{slot: base.GetState(key.addr, key.slot)}
	}
}

// bstmRead is one value an incarnation read, and the version it read it from.
type bstmRead struct {
	key     bstmKey
	version bstmVersion
	value   bstmValue
	// base is the start balance a balance read built on.
	base *big.Int
}

// namedIn reports whether the read set names the read's key.
func (r bstmRead) namedIn(readSet map[stateAccessKey]struct{}) bool {
	if _, ok := readSet[stateAccessKey{kind: stateAccessAccount, address: r.key.addr}]; ok {
		return true
	}
	var key stateAccessKey
	switch r.key.kind {
	case bstmBalance:
		key = stateAccessKey{kind: stateAccessBalance, address: r.key.addr}
	case bstmNonce:
		key = stateAccessKey{kind: stateAccessNonce, address: r.key.addr}
	case bstmCode:
		key = stateAccessKey{kind: stateAccessCode, address: r.key.addr}
	default:
		key = stateAccessKey{kind: stateAccessStorage, address: r.key.addr, slot: r.key.slot}
	}
	_, ok := readSet[key]
	return ok
}

// bstmBlocked is the panic value that stops an execution which read an estimate: the writer to
// wait for.
type bstmBlocked int

// bstmReadIndexAt is the read count above which a reader indexes its reads by key.
const bstmReadIndexAt = 8

// bstmReader is the StateReader one incarnation executes against: each key as the memory holds it
// for the transaction, over the block's start state. It records every value it serves and serves a
// key read twice the value it served first.
type bstmReader struct {
	memory  *bstmMemory
	base    StateReader
	baseRow accountSnapshotReader
	tx      int
	// validatedAt, when set, reports whether a lower write has passed validation; a read of one that has not stops
	// the execution to wait for it.
	validatedAt func(bstmVersion) bool
	bstmReadLog
	// coinbaseUnfolded is set once the reader served the coinbase balance without the fee credits
	// below tx.
	coinbaseUnfolded bool
}

// bstmReadLog is the reads one incarnation recorded, indexed by key once there are more than
// bstmReadIndexAt of them.
type bstmReadLog struct {
	reads   []bstmRead
	index   map[bstmKey]int
	indexed bool
}

// reset empties the log for the next incarnation, keeping its storage.
func (l bstmReadLog) reset() bstmReadLog {
	clear(l.reads)
	if l.indexed {
		clear(l.index)
	}
	return bstmReadLog{reads: l.reads[:0], index: l.index}
}

// cached returns the read already recorded for key.
func (r *bstmReader) cached(key bstmKey) (bstmRead, bool) {
	if r.indexed {
		i, ok := r.index[key]
		if !ok {
			return bstmRead{}, false
		}
		return r.reads[i], true
	}
	for i := range r.reads {
		if r.reads[i].key == key {
			return r.reads[i], true
		}
	}
	return bstmRead{}, false
}

func (r *bstmReader) record(read bstmRead) {
	r.reads = append(r.reads, read)
	switch {
	case r.indexed:
		r.index[read.key] = len(r.reads) - 1
	case len(r.reads) > bstmReadIndexAt:
		if r.index == nil {
			r.index = make(map[bstmKey]int, 4*bstmReadIndexAt)
		}
		r.indexed = true
		for i := range r.reads {
			r.index[r.reads[i].key] = i
		}
	}
}

// waitFor stops the execution to wait for writer.
func (r *bstmReader) waitFor(writer int) {
	panic(bstmBlocked(writer))
}

// bstmUnvalidated is the panic value that stops an execution to wait for a lower write to pass validation.
type bstmUnvalidated int

// requireValidated stops the execution to wait for v's transaction when the reader waits for lower writes
// to pass validation and v has not.
func (r *bstmReader) requireValidated(v bstmVersion) {
	if r.validatedAt != nil && v.tx >= 0 && !r.validatedAt(v) {
		panic(bstmUnvalidated(v.tx))
	}
}

// balance reads addr's balance, building on base() when no transaction below wrote it. It reads through
// estimates: a balance loaded only to credit or debit it as a delta is not read, and validation decides.
func (r *bstmReader) balance(addr common.Address, base func() *big.Int) *big.Int {
	key := bstmKey{kind: bstmBalance, addr: addr}
	if read, ok := r.cached(key); ok {
		return read.value.balance
	}
	if addr == r.memory.coinbase && !r.memory.foldCoinbase[r.tx].Load() {
		// A coinbase balance is loaded to credit a fee, and folding every fee below would serialise the
		// block on one read. It is served from the start state, and validation checks it only if read.
		value := base()
		if value == nil {
			value = new(big.Int)
		}
		r.coinbaseUnfolded = true
		r.record(bstmRead{key: key, version: bstmBaseVersion, value: bstmValue{balance: value}, base: value})
		return value
	}
	read, _ := r.memory.balance(addr, r.tx, true, base)
	r.requireValidated(read.version)
	r.record(bstmRead{key: key, version: read.version, value: bstmValue{balance: read.value}, base: read.base})
	return read.value
}

// field reads a nonce or code key, waiting for the writer of an estimate.
func (r *bstmReader) field(key bstmKey, base func() bstmValue) bstmValue {
	if read, ok := r.cached(key); ok {
		return read.value
	}
	read := bstmRead{key: key, version: bstmBaseVersion}
	entry, written := r.memory.entry(key, r.tx)
	switch {
	case written && entry.estimate:
		r.waitFor(entry.version.tx)
	case written:
		r.requireValidated(entry.version)
		read.version, read.value = entry.version, entry.value
	default:
		read.value = base()
	}
	r.record(read)
	return read.value
}

func (r *bstmReader) GetBalance(addr common.Address) *big.Int {
	return r.balance(addr, func() *big.Int { return r.base.GetBalance(addr) })
}

func (r *bstmReader) GetNonce(addr common.Address) uint64 {
	return r.field(bstmKey{kind: bstmNonce, addr: addr}, func() bstmValue {
		return bstmValue{nonce: r.base.GetNonce(addr)}
	}).nonce
}

func (r *bstmReader) GetCode(addr common.Address) []byte {
	return r.field(bstmKey{kind: bstmCode, addr: addr}, func() bstmValue {
		return bstmValue{code: r.base.GetCode(addr)}
	}).code
}

func (r *bstmReader) GetState(addr common.Address, slot common.Hash) common.Hash {
	return r.field(bstmKey{kind: bstmSlot, addr: addr, slot: slot}, func() bstmValue {
		return bstmValue{slot: r.base.GetState(addr, slot)}
	}).slot
}

// ReadAccount satisfies accountSnapshotReader: it reads the start state's row once for every field no
// transaction below wrote. It declines when the start state does not serve the row.
func (r *bstmReader) ReadAccount(addr common.Address) (accountSnapshot, bool) {
	if r.baseRow == nil {
		return accountSnapshot{}, false
	}
	row, served := r.baseRow.ReadAccount(addr)
	if !served {
		return accountSnapshot{}, false
	}
	nonce := r.field(bstmKey{kind: bstmNonce, addr: addr}, func() bstmValue { return bstmValue{nonce: row.Nonce} })
	codeKey := bstmKey{kind: bstmCode, addr: addr}
	code := r.field(codeKey, func() bstmValue { return bstmValue{code: row.Code} })
	row.Balance = r.balance(addr, func() *big.Int { return row.Balance })
	row.Nonce = nonce.nonce
	row.Code = code.code
	return row, true
}

// bstmTaskKind is what a scheduler task asks a worker to do.
type bstmTaskKind uint8

const (
	bstmNoTask bstmTaskKind = iota
	bstmExecute
	bstmValidate
)

type bstmTask struct {
	kind bstmTaskKind
	tx   int
	inc  int
	// wave is, for a validation, the validation wave current when it was handed out, before it read.
	wave uint64
}

type bstmStatus uint8

const (
	bstmReady bstmStatus = iota
	bstmExecuting
	bstmExecuted
	bstmAborting
	// bstmDelayed is a transaction whose next incarnation waits until every transaction below it is
	// committed.
	bstmDelayed
)

// bstmTxState is one transaction's place in the schedule.
type bstmTxState struct {
	mu          sync.Mutex
	status      bstmStatus
	incarnation int
	// failures counts the transaction's incarnations that failed validation.
	failures int
	// parks counts the transaction's executions that stopped at a lower transaction's estimate.
	parks int
	// validationWaits counts the transaction's executions stopped to wait for a lower write to pass
	// validation.
	validationWaits int
	// starts counts the transaction's executions started.
	starts int
	// validatedInc is the incarnation that last passed validation, or -1, and validatedWave the
	// validation wave that validation was handed out in.
	validatedInc  int
	validatedWave uint64
	// dependents are the transactions waiting for this one to execute.
	dependents []int
	// passed is 1 + the executed incarnation that passed validation, or 0; a committed transaction's
	// incarnation has passed.
	passed atomic.Int64
	// validationWaiters are the transactions waiting for this one's incarnation to pass validation
	// before they run again.
	validationWaiters []int
}

// bstmSpins is how many times an idle worker looks for work before it parks.
const bstmSpins = 64

// bstmValidationsPerWorker is the validation backlog one awake worker is expected to take.
const bstmValidationsPerWorker = 32

// bstmScheduler is Block-STM's collaborative scheduler: workers take the lower of the next
// transaction to execute and the next to validate off two counters, which move back when an execution
// or a failed validation leaves work below them. Idle workers park.
type bstmScheduler struct {
	n       int
	workers int64
	execIdx atomic.Int64
	valIdx  atomic.Int64
	// decreases counts moves of either counter back, so a done check can tell that one raced it.
	decreases atomic.Int64
	// active counts tasks handed out and not yet finished.
	active atomic.Int64
	done   atomic.Bool
	txs    []bstmTxState
	// ready and executed mark the transactions in those statuses, so a counter skips the others.
	ready      bstmBits
	executed   bstmBits
	readyCount atomic.Int64
	// valWave counts moves of the validation counter back; waveAt[tx] is the latest wave that moved it
	// back to tx or below it to tx, so a validation handed out in an earlier wave may be stale.
	valWave atomic.Uint64
	waveAt  []atomic.Uint64
	// committed is how many transactions from the first are final: each executed, validated in a wave
	// no later move of the counter to it or below it followed, above transactions that were final.
	committed atomic.Int64
	// commitWanted asks the worker advancing committed to look again; commitMu admits one such worker,
	// and requiredWave, which it alone touches, is the latest wave at or below committed.
	commitWanted atomic.Bool
	commitMu     sync.Mutex
	requiredWave uint64
	delayedCount atomic.Int64

	parkMu sync.Mutex
	parked atomic.Int64
	wake   *sync.Cond
	// retired wakes the workers retire holds, once the block is done.
	retired *sync.Cond
}

func newBlockSTMScheduler(n int, workers int) *bstmScheduler {
	s := &bstmScheduler{
		n:        n,
		workers:  int64(workers),
		txs:      make([]bstmTxState, n),
		ready:    newBlockSTMBits(n, true),
		executed: newBlockSTMBits(n, false),
		waveAt:   make([]atomic.Uint64, n+1),
	}
	for tx := range s.txs {
		s.txs[tx].validatedInc = -1
	}
	s.readyCount.Store(int64(n))
	s.wake = sync.NewCond(&s.parkMu)
	s.retired = sync.NewCond(&s.parkMu)
	return s
}

// nextTask returns the lower of the next validation and the next execution, or no task.
func (s *bstmScheduler) nextTask() bstmTask {
	if s.valIdx.Load() < s.execIdx.Load() {
		return s.nextToValidate()
	}
	return s.nextToExecute()
}

func (s *bstmScheduler) nextToExecute() bstmTask {
	tx, ok := s.claim(&s.execIdx, s.ready, s.decreaseExecution)
	if !ok {
		return bstmTask{}
	}
	if inc, ok := s.tryIncarnate(tx); ok {
		return bstmTask{kind: bstmExecute, tx: tx, inc: inc}
	}
	return bstmTask{}
}

func (s *bstmScheduler) nextToValidate() bstmTask {
	tx, ok := s.claim(&s.valIdx, s.executed, s.decreaseValidation)
	if !ok {
		return bstmTask{}
	}
	wave := s.valWave.Load()
	st := &s.txs[tx]
	st.mu.Lock()
	if st.status == bstmExecuted {
		inc := st.incarnation
		st.mu.Unlock()
		return bstmTask{kind: bstmValidate, tx: tx, inc: inc, wave: wave}
	}
	st.mu.Unlock()
	s.active.Add(-1)
	return bstmTask{}
}

// claim moves counter past the lowest transaction at or above it whose bit is set and returns that
// transaction, counted as an active task. It returns false, counting nothing, when there is none.
func (s *bstmScheduler) claim(counter *atomic.Int64, marked bstmBits, decrease func(int)) (int, bool) {
	s.active.Add(1)
	for {
		from := int(counter.Load())
		if from >= s.n {
			s.active.Add(-1)
			s.checkDone()
			return 0, false
		}
		next := marked.next(from, s.n)
		if !counter.CompareAndSwap(int64(from), int64(min(next+1, s.n))) {
			continue
		}
		// A bit set in the skipped range after it was read went with a decrease that found the counter
		// not yet past it.
		if missed := marked.next(from, next); missed < next {
			decrease(missed)
		}
		if next >= s.n {
			s.active.Add(-1)
			s.checkDone()
			return 0, false
		}
		return next, true
	}
}

func (s *bstmScheduler) tryIncarnate(tx int) (int, bool) {
	st := &s.txs[tx]
	st.mu.Lock()
	if st.status == bstmReady {
		st.status = bstmExecuting
		s.ready.clear(tx)
		inc := st.incarnation
		st.mu.Unlock()
		s.readyCount.Add(-1)
		return inc, true
	}
	st.mu.Unlock()
	s.active.Add(-1)
	return 0, false
}

// recordStart counts an execution started for tx.
func (s *bstmScheduler) recordStart(tx int) {
	st := &s.txs[tx]
	st.mu.Lock()
	st.starts++
	st.mu.Unlock()
}

// addDependency parks tx behind blocking unless blocking has executed since tx read its estimate, and
// reports whether it did.
func (s *bstmScheduler) addDependency(tx int, blocking int) bool {
	b := &s.txs[blocking]
	b.mu.Lock()
	t := &s.txs[tx]
	t.mu.Lock()
	t.parks++
	if t.held() {
		t.status = bstmDelayed
		t.mu.Unlock()
		b.mu.Unlock()
		s.active.Add(-1)
		s.delayedCount.Add(1)
		s.tryCommit()
		return true
	}
	if b.status == bstmExecuted {
		t.mu.Unlock()
		b.mu.Unlock()
		return false
	}
	t.status = bstmAborting
	t.mu.Unlock()
	b.dependents = append(b.dependents, tx)
	b.mu.Unlock()
	s.active.Add(-1)
	return true
}

// validatedAt reports whether v's transaction is committed or v's incarnation has passed validation.
func (s *bstmScheduler) validatedAt(v bstmVersion) bool {
	return int64(v.tx) < s.committed.Load() || s.txs[v.tx].passed.Load() == int64(v.inc)+1
}

// addValidationDependency parks tx until blocking's incarnation passes validation unless it already has,
// and reports whether it did. The wait is not a failed incarnation.
func (s *bstmScheduler) addValidationDependency(tx int, blocking int) bool {
	b := &s.txs[blocking]
	b.mu.Lock()
	t := &s.txs[tx]
	t.mu.Lock()
	t.validationWaits++
	if t.held() {
		t.status = bstmDelayed
		t.mu.Unlock()
		b.mu.Unlock()
		s.active.Add(-1)
		s.delayedCount.Add(1)
		s.tryCommit()
		return true
	}
	if int64(blocking) < s.committed.Load() || (b.status == bstmExecuted && b.passed.Load() == int64(b.incarnation)+1) {
		t.mu.Unlock()
		b.mu.Unlock()
		return false
	}
	t.status = bstmAborting
	t.mu.Unlock()
	b.validationWaiters = append(b.validationWaiters, tx)
	b.mu.Unlock()
	s.active.Add(-1)
	return true
}

// releaseValidationWaiters makes the transactions that waited for a write to pass validation ready to run the same
// incarnation again.
func (s *bstmScheduler) releaseValidationWaiters(waiters []int) {
	if len(waiters) == 0 {
		return
	}
	lowest := s.n
	for _, tx := range waiters {
		st := &s.txs[tx]
		st.mu.Lock()
		st.status = bstmReady
		s.ready.set(tx)
		st.mu.Unlock()
		s.readyCount.Add(1)
		lowest = min(lowest, tx)
	}
	s.decreaseExecution(lowest)
	s.wakeIfBehind()
}

// releaseCommitted marks committed tx's incarnation as passed and releases what waited for it.
func (s *bstmScheduler) releaseCommitted(tx int) {
	st := &s.txs[tx]
	st.mu.Lock()
	st.passed.Store(int64(st.incarnation) + 1)
	waiters := st.validationWaiters
	st.validationWaiters = nil
	st.mu.Unlock()
	s.releaseValidationWaiters(waiters)
}

func (s *bstmScheduler) setReady(tx int) {
	st := &s.txs[tx]
	st.mu.Lock()
	st.incarnation++
	st.status = bstmReady
	s.ready.set(tx)
	st.mu.Unlock()
	s.readyCount.Add(1)
}

// finishExecution marks tx executed, releases the transactions waiting for it, and returns its
// validation when the validation counter has passed it and it wrote no key its previous incarnation
// did not.
func (s *bstmScheduler) finishExecution(tx int, inc int, newPath bool) bstmTask {
	if newPath && s.valIdx.Load() > int64(tx) {
		// Before tx can commit, so the committed prefix never passes a validation above tx that read
		// before tx wrote the new key. One that read before the counter last came back to tx or below is
		// already behind that move's wave.
		s.bumpWave(tx)
	}
	st := &s.txs[tx]
	st.mu.Lock()
	st.status = bstmExecuted
	s.executed.set(tx)
	dependents := st.dependents
	st.dependents = nil
	st.mu.Unlock()
	if len(dependents) > 0 {
		lowest := s.n
		for _, d := range dependents {
			if s.readyOrDelay(d) {
				lowest = min(lowest, d)
			}
		}
		if lowest < s.n {
			s.decreaseExecution(lowest)
			s.wakeIfBehind()
		}
	}
	if s.valIdx.Load() > int64(tx) {
		if !newPath {
			return bstmTask{kind: bstmValidate, tx: tx, inc: inc, wave: s.valWave.Load()}
		}
		s.lowerValidation(tx)
		s.wakeIfBehind()
	}
	s.active.Add(-1)
	return bstmTask{}
}

// held reports whether tx's next incarnation waits until every transaction below it is committed.
func (s *bstmScheduler) held(tx int) bool {
	st := &s.txs[tx]
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.held()
}

// held reports whether the transaction has exhausted a retry budget and must wait until every
// transaction below it is committed. The caller holds st.mu.
func (st *bstmTxState) held() bool {
	return st.failures >= bstmDelayAfter || st.parks >= bstmParkBudget ||
		st.validationWaits >= bstmValidationWaitBudget
}

// tryValidationAbort aborts incarnation inc of tx if it is still the executed one. A held transaction
// waits to be committed below before it runs again, and one that has failed occMaxTxIncarnations times
// is left in place for the frontier.
func (s *bstmScheduler) tryValidationAbort(tx int, inc int) bstmAbort {
	st := &s.txs[tx]
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.status != bstmExecuted || st.incarnation != inc {
		return bstmNotAborted
	}
	if st.failures >= occMaxTxIncarnations {
		return bstmCapped
	}
	s.executed.clear(tx)
	st.failures++
	st.status = bstmAborting
	st.passed.Store(0)
	if st.held() {
		return bstmAbortedDelayed
	}
	return bstmAborted
}

// bstmAbort is what a failed validation did to its transaction.
type bstmAbort uint8

const (
	bstmNotAborted bstmAbort = iota
	bstmAborted
	// bstmAbortedDelayed is an abort whose next incarnation waits until the transaction is next to commit.
	bstmAbortedDelayed
	// bstmCapped leaves the incarnation in place for the frontier: the transaction failed validation
	// occMaxTxIncarnations times.
	bstmCapped
)

// bstmDelayAfter is the number of failed incarnations after which a transaction runs again only once
// every transaction below it is committed, so that incarnation reads only final values.
const bstmDelayAfter = occMaxTxIncarnations - 1

// bstmParkBudget is the number of executions stopped at an estimate after which a transaction is held.
// It is a variable so tests can make it bind.
var bstmParkBudget = 64

// bstmValidationWaitBudget is the number of executions stopped at unvalidated lower writes after which
// a transaction is held. It is a variable so tests can make it bind.
var bstmValidationWaitBudget = 64

// bstmStartBudget is the maximum number of executions one transaction can start.
func bstmStartBudget() int {
	return bstmParkBudget + bstmValidationWaitBudget + occMaxTxIncarnations + 1
}

// readyOrDelay makes tx, released by the writer it waited for, ready to execute again, or holds it until
// it is next to commit. It reports whether tx is ready.
func (s *bstmScheduler) readyOrDelay(tx int) bool {
	st := &s.txs[tx]
	st.mu.Lock()
	if !st.held() {
		st.mu.Unlock()
		s.setReady(tx)
		return true
	}
	st.status = bstmDelayed
	st.mu.Unlock()
	s.delayedCount.Add(1)
	s.tryCommit()
	return false
}

// markValidated records that incarnation inc of tx passed a validation handed out in wave.
func (s *bstmScheduler) markValidated(tx int, inc int, wave uint64) {
	st := &s.txs[tx]
	st.mu.Lock()
	if st.status == bstmExecuted && st.incarnation == inc && (st.validatedInc != inc || wave > st.validatedWave) {
		st.validatedInc, st.validatedWave = inc, wave
	}
	st.mu.Unlock()
}

// tryCommit advances the committed prefix as far as it goes, unless another worker is doing so, in
// which case that worker looks again.
func (s *bstmScheduler) tryCommit() {
	for {
		s.commitWanted.Store(true)
		if !s.commitMu.TryLock() {
			return
		}
		s.commitWanted.Store(false)
		s.advanceCommitted()
		s.commitMu.Unlock()
		if !s.commitWanted.Load() {
			return
		}
	}
}

// advanceCommitted commits transactions in order while the next is executed and validated in a wave no
// later move of the validation counter to it or below it followed. It releases a delayed transaction
// once it is next.
func (s *bstmScheduler) advanceCommitted() {
	for {
		tx := int(s.committed.Load())
		if tx >= s.n {
			return
		}
		required := max(s.requiredWave, s.waveAt[tx].Load())
		st := &s.txs[tx]
		st.mu.Lock()
		if st.status == bstmDelayed {
			st.mu.Unlock()
			s.release(tx)
			return
		}
		final := st.status == bstmExecuted && st.validatedInc == st.incarnation && st.validatedWave >= required
		st.mu.Unlock()
		if !final {
			return
		}
		s.requiredWave = required
		s.committed.Store(int64(tx + 1))
		s.releaseCommitted(tx)
	}
}

// release makes delayed tx ready to execute against the committed transactions below it.
func (s *bstmScheduler) release(tx int) {
	s.setReady(tx)
	s.decreaseExecution(tx)
	s.wakeIfBehind()
}

// finishValidation schedules an aborted transaction's next incarnation, returning it to the caller
// when the execution counter has passed it, and moves validation back to the transactions above it.
func (s *bstmScheduler) finishValidation(tx int, outcome bstmAbort) bstmTask {
	aborted := outcome == bstmAborted || outcome == bstmAbortedDelayed
	if aborted {
		// While tx is still aborting, so neither its release nor its next incarnation's commit can
		// overtake the transactions above it that read the incarnation just aborted.
		s.decreaseValidation(tx + 1)
	}
	if outcome == bstmAbortedDelayed {
		// Held only now, after its writes became estimates, so a release cannot overtake them.
		st := &s.txs[tx]
		st.mu.Lock()
		st.status = bstmDelayed
		st.mu.Unlock()
		s.delayedCount.Add(1)
		s.wakeIfBehind()
		s.tryCommit()
		s.active.Add(-1)
		return bstmTask{}
	}
	if aborted {
		s.setReady(tx)
		s.wakeIfBehind()
		if s.execIdx.Load() > int64(tx) {
			if inc, ok := s.tryIncarnate(tx); ok {
				return bstmTask{kind: bstmExecute, tx: tx, inc: inc}
			}
			return bstmTask{}
		}
	}
	s.active.Add(-1)
	return bstmTask{}
}

func (s *bstmScheduler) decreaseExecution(tx int) {
	lowerStop(&s.execIdx, int64(tx))
	s.decreases.Add(1)
}

func (s *bstmScheduler) decreaseValidation(tx int) {
	s.bumpWave(tx)
	s.lowerValidation(tx)
}

// bumpWave starts a validation wave that every transaction from tx on must be validated in again before
// it commits.
func (s *bstmScheduler) bumpWave(tx int) {
	wave := s.valWave.Add(1)
	for at := &s.waveAt[tx]; ; {
		old := at.Load()
		if old >= wave || at.CompareAndSwap(old, wave) {
			return
		}
	}
}

// lowerValidation moves the validation counter back to tx.
func (s *bstmScheduler) lowerValidation(tx int) {
	lowerStop(&s.valIdx, int64(tx))
	s.decreases.Add(1)
}

// checkDone ends the block once both counters are past the last transaction, no task is active, and no
// counter moved back meanwhile.
func (s *bstmScheduler) checkDone() {
	observed := s.decreases.Load()
	if min(s.execIdx.Load(), s.valIdx.Load()) >= int64(s.n) && s.active.Load() == 0 && observed == s.decreases.Load() {
		// A delayed transaction is still to run once the transactions below it commit; waiting for the
		// lock lets a worker already committing release it first.
		s.commitMu.Lock()
		s.advanceCommitted()
		s.commitMu.Unlock()
		if observed != s.decreases.Load() {
			return
		}
		s.halt()
	}
}

// halt ends the block and releases every parked worker.
func (s *bstmScheduler) halt() {
	s.done.Store(true)
	s.parkMu.Lock()
	s.wake.Broadcast()
	s.retired.Broadcast()
	s.parkMu.Unlock()
}

// hasWork reports whether either counter has transactions left to look at.
func (s *bstmScheduler) hasWork() bool {
	return s.execIdx.Load() < int64(s.n) || s.valIdx.Load() < int64(s.n)
}

// park waits, after a short spin, until either counter has transactions left to look at or the block
// is done, and reports false once it is done.
func (s *bstmScheduler) park() bool {
	for range bstmSpins {
		if s.done.Load() {
			return false
		}
		if s.hasWork() {
			return true
		}
	}
	s.parkMu.Lock()
	s.parked.Add(1)
	for !s.done.Load() && !s.hasWork() {
		if s.parked.Load() == s.workers {
			s.settleIdleBlock()
			if s.done.Load() || s.hasWork() {
				break
			}
		}
		s.wake.Wait()
	}
	s.parked.Add(-1)
	s.parkMu.Unlock()
	s.wakeIfBehind()
	return !s.done.Load()
}

// retire holds a worker, counted as parked, until the block is done. Its wait is apart from park's, so a
// wake-up meant for a worker that takes tasks never reaches it.
func (s *bstmScheduler) retire() {
	s.parkMu.Lock()
	s.parked.Add(1)
	for !s.done.Load() {
		if s.parked.Load() == s.workers {
			s.settleIdleBlock()
			if s.done.Load() {
				break
			}
			if s.hasWork() {
				// The settle released a delayed transaction, which a worker that takes tasks must run.
				s.wake.Signal()
			}
		}
		s.retired.Wait()
	}
	s.parked.Add(-1)
	s.parkMu.Unlock()
}

// settleIdleBlock runs checkDone for the last worker to park, which holds parkMu on entry and on return.
// checkDone passes only when no task is active, and a claim counts itself active while it looks, so
// workers that run out of work together can each fail it for the other. With every other worker
// parked none is claiming, so this check ends the block or releases a delayed transaction.
func (s *bstmScheduler) settleIdleBlock() {
	s.parkMu.Unlock()
	s.checkDone()
	s.parkMu.Lock()
}

// wakeIfBehind wakes one parked worker when the ready executions, and the validation backlog counted
// per worker, outnumber the workers awake to take them. The worker that made the work takes one task
// itself, so a chain whose every execution releases the next wakes nobody.
func (s *bstmScheduler) wakeIfBehind() {
	parked := s.parked.Load()
	if parked == 0 {
		return
	}
	awake := s.workers - parked
	backlog := int64(s.executed.count(int(min(s.valIdx.Load(), int64(s.n))), s.n))
	if s.readyCount.Load()+backlog/bstmValidationsPerWorker <= awake {
		return
	}
	s.parkMu.Lock()
	s.wake.Signal()
	s.parkMu.Unlock()
}

// bstmBits is a set of transaction indexes safe for concurrent use.
type bstmBits []atomic.Uint64

func newBlockSTMBits(n int, all bool) bstmBits {
	b := make(bstmBits, (n+63)/64)
	if all {
		for i := range n {
			b.set(i)
		}
	}
	return b
}

func (b bstmBits) set(i int) { b[i/64].Or(1 << (uint(i) % 64)) }

func (b bstmBits) clear(i int) { b[i/64].And(^(uint64(1) << (uint(i) % 64))) }

// next returns the lowest index in [from, to) in the set, or to.
func (b bstmBits) next(from int, to int) int {
	for word := from / 64; word*64 < to; word++ {
		w := b[word].Load()
		if word == from/64 {
			w &= ^uint64(0) << (uint(from) % 64)
		}
		if w != 0 {
			return min(word*64+bits.TrailingZeros64(w), to)
		}
	}
	return to
}

// count returns how many indexes in [from, to) are in the set.
func (b bstmBits) count(from int, to int) int {
	n := 0
	for word := from / 64; word*64 < to; word++ {
		w := b[word].Load()
		if word == from/64 {
			w &= ^uint64(0) << (uint(from) % 64)
		}
		if end := to - word*64; end < 64 {
			w &= uint64(1)<<uint(end) - 1
		}
		n += bits.OnesCount64(w)
	}
	return n
}

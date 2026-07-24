package evmonly

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
)

type occTxExecution struct {
	txResult                 TxResult
	receipt                  *ethtypes.Receipt
	changeSet                StateChangeSet
	readSet                  map[stateAccessKey]struct{}
	writeSet                 map[stateAccessKey]struct{}
	gasUsed                  uint64
	gasLimit                 uint64
	commutativeBalanceDeltas map[common.Address]*big.Int
	err                      error
}

type occSpeculativeRunner struct {
	executor      *Executor
	req           PreparedBlock
	chainConfig   *params.ChainConfig
	blockCtx      vm.BlockContext
	baseFee       *big.Int
	blockGasLimit uint64
}

func newOCCSpeculativeRunner(e *Executor, req PreparedBlock) occSpeculativeRunner {
	gasLimit := req.Context.GasLimit
	if gasLimit == 0 {
		gasLimit = math.MaxUint64
	}
	return occSpeculativeRunner{
		executor:      e,
		req:           req,
		chainConfig:   e.chainConfig(req.Context),
		blockCtx:      buildBlockContext(req.Context),
		baseFee:       cloneOptionalBig(req.Context.BaseFee),
		blockGasLimit: gasLimit,
	}
}

func (e *Executor) executeBlockOCC(ctx context.Context, req PreparedBlock) (*BlockResult, error) {
	runner := newOCCSpeculativeRunner(e, req)
	workers := e.cfg.OCCWorkers
	if workers > len(req.Txs) {
		workers = len(req.Txs)
	}
	executionPool := e.occPool
	if executionPool == nil {
		executionPool = newOCCWorkerPool(workers)
		defer executionPool.Close()
	}
	validationPool := newOCCWorkerPool(workers)
	defer validationPool.Close()

	results, changeSet, validation, err := e.validateBlockSTM(ctx, runner, executionPool, validationPool)
	if errors.Is(err, errOCCMaxIncarnation) {
		result, seqErr := e.executeBlockSequential(ctx, req)
		if seqErr != nil {
			return nil, seqErr
		}
		result.OCCStats = validation.stats(true)
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	result, err := e.mergeOCCResults(ctx, results, changeSet)
	if err != nil {
		return nil, err
	}
	result.OCCStats = validation.stats(false)
	return result, nil
}

func (r occSpeculativeRunner) executeTx(
	ctx context.Context,
	source StateReader,
	txIndex int,
	txIndexUint uint,
	gasLimit uint64,
) (occTxExecution, error) {
	return r.executor.executeTxSpeculative(
		ctx,
		source,
		r.req,
		txIndex,
		txIndexUint,
		r.chainConfig,
		r.blockCtx,
		r.baseFee,
		gasLimit,
	)
}

func (e *Executor) executeTxSpeculative(
	ctx context.Context,
	source StateReader,
	req PreparedBlock,
	txIndex int,
	txIndexUint uint,
	chainConfig *params.ChainConfig,
	blockCtx vm.BlockContext,
	baseFee *big.Int,
	gasLimit uint64,
) (occTxExecution, error) {
	if err := ctx.Err(); err != nil {
		return occTxExecution{}, err
	}
	p := req.Txs[txIndex]
	stateDB := e.acquireStateDB(source)
	defer e.releaseStateDB(stateDB)
	stateDB.enableAccessTracking()
	evm := vm.NewEVM(blockCtx, stateDB, chainConfig, vm.Config{}, nil)
	stateDB.SetEVM(evm)
	gasPool := new(core.GasPool).AddGas(gasLimit)
	txResult, receipt, err := e.executeTx(
		evm,
		stateDB,
		gasPool,
		req.Context,
		p,
		txIndex,
		txIndexUint,
		baseFee,
	)
	if err != nil {
		return occTxExecution{}, fmt.Errorf("execute tx %d %s: %w", txIndex, p.Tx.Hash(), err)
	}
	readSet, writeSet := stateDB.accessSets()
	var changeSet StateChangeSet
	stateDB.ChangeSetInto(&changeSet)
	return occTxExecution{
		txResult:                 txResult,
		receipt:                  receipt,
		changeSet:                changeSet,
		readSet:                  readSet,
		writeSet:                 writeSet,
		gasUsed:                  txResult.GasUsed,
		gasLimit:                 p.Tx.Gas(),
		commutativeBalanceDeltas: stateDB.commutativeBalanceDeltasBig(),
	}, nil
}

const occMaxTxIncarnations = 10

var errOCCMaxIncarnation = errors.New("occ max incarnation reached")

type occExecutionTask struct {
	txIndex      int
	txIndexUint  uint
	incarnation  int
	sourcePrefix int
	source       StateReader
	gasLimit     uint64
}

type occValidationTask struct {
	execution occExecutionTask
	result    occTxExecution
}

type occValidationAction struct {
	executionTasks []occExecutionTask
	complete       bool
}

// occBlockSTMScheduler coordinates execution and validation workers. Execution
// tasks carry the accepted-prefix version they read from; validation only
// compares them against writes accepted after that prefix.
type occBlockSTMScheduler struct {
	runner         occSpeculativeRunner
	executionPool  *occWorkerPool
	validationPool *occWorkerPool

	executionQueue  chan occExecutionTask
	validationQueue chan occValidationTask
	done            chan struct{}
	cancel          context.CancelFunc
	finishOnce      sync.Once
	finishErr       error

	mu                sync.Mutex
	prefix            *blockSTMState
	writes            *stateAccessIndex
	results           []occTxExecution
	ready             []occValidationTask
	readySet          []bool
	latestScheduled   []int
	nextToValidate    int
	cumulativeGasUsed uint64
	validation        occValidationResult
}

func (e *Executor) validateBlockSTM(
	ctx context.Context,
	runner occSpeculativeRunner,
	executionPool *occWorkerPool,
	validationPool *occWorkerPool,
) ([]occTxExecution, StateChangeSet, occValidationResult, error) {
	scheduler := newOCCBlockSTMScheduler(e.state, runner, executionPool, validationPool)
	return scheduler.run(ctx)
}

func newOCCBlockSTMScheduler(
	source StateReader,
	runner occSpeculativeRunner,
	executionPool *occWorkerPool,
	validationPool *occWorkerPool,
) *occBlockSTMScheduler {
	txCount := len(runner.req.Txs)
	baseSource := parallelSafeStateReader(source)
	return &occBlockSTMScheduler{
		runner:          runner,
		executionPool:   executionPool,
		validationPool:  validationPool,
		executionQueue:  make(chan occExecutionTask, txCount),
		validationQueue: make(chan occValidationTask, txCount),
		done:            make(chan struct{}),
		prefix:          newBlockSTMState(baseSource),
		writes:          newStateAccessIndex(),
		results:         make([]occTxExecution, txCount),
		ready:           make([]occValidationTask, txCount),
		readySet:        make([]bool, txCount),
		latestScheduled: make([]int, txCount),
		validation:      occValidationResult{valid: true},
	}
}

func (s *occBlockSTMScheduler) run(ctx context.Context) ([]occTxExecution, StateChangeSet, occValidationResult, error) {
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	defer cancel()

	waitCh := make(chan error, 2)
	go func() {
		waitCh <- s.executionPool.Run(runCtx, func(workerCtx context.Context, _ int) error {
			return s.executionWorker(workerCtx)
		})
	}()
	go func() {
		waitCh <- s.validationPool.Run(runCtx, func(workerCtx context.Context, _ int) error {
			return s.validationWorker(workerCtx)
		})
	}()

	for txIndex, txIndexUint := 0, uint(0); txIndex < len(s.results); txIndex, txIndexUint = txIndex+1, txIndexUint+1 {
		if err := s.enqueueExecution(runCtx, occExecutionTask{
			txIndex:      txIndex,
			txIndexUint:  txIndexUint,
			incarnation:  0,
			sourcePrefix: 0,
			source:       s.prefix.source,
			gasLimit:     s.runner.blockGasLimit,
		}); err != nil {
			s.finish(err)
			break
		}
	}

	completedPools := 0
	var poolErr error
	for {
		select {
		case <-s.done:
			cancel()
			for completedPools < 2 {
				if err := <-waitCh; err != nil && poolErr == nil && !errors.Is(err, context.Canceled) {
					poolErr = err
				}
				completedPools++
			}
			results, changeSet, validation, finishErr := s.result()
			if finishErr != nil {
				return nil, StateChangeSet{}, validation, finishErr
			}
			if poolErr != nil {
				return nil, StateChangeSet{}, validation, poolErr
			}
			return results, changeSet, validation, nil
		case err := <-waitCh:
			completedPools++
			if err != nil && poolErr == nil && !errors.Is(err, context.Canceled) {
				poolErr = err
				s.finish(err)
			}
			if completedPools == 2 {
				results, changeSet, validation, finishErr := s.result()
				if finishErr != nil {
					return nil, StateChangeSet{}, validation, finishErr
				}
				if poolErr != nil {
					return nil, StateChangeSet{}, validation, poolErr
				}
				return results, changeSet, validation, nil
			}
		case <-ctx.Done():
			s.finish(ctx.Err())
		}
	}
}

func (s *occBlockSTMScheduler) executionWorker(ctx context.Context) error {
	for {
		select {
		case <-s.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case task := <-s.executionQueue:
			result, err := s.runner.executeTx(ctx, task.source, task.txIndex, task.txIndexUint, task.gasLimit)
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return ctxErr
				}
				result.err = err
				result.gasLimit = s.runner.req.Txs[task.txIndex].Tx.Gas()
			}
			if err := s.enqueueValidation(ctx, occValidationTask{execution: task, result: result}); err != nil {
				return err
			}
		}
	}
}

func (s *occBlockSTMScheduler) validationWorker(ctx context.Context) error {
	for {
		select {
		case <-s.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case task := <-s.validationQueue:
			action, err := s.handleValidation(task)
			if err != nil {
				s.finish(err)
				return nil
			}
			if action.complete {
				s.finish(nil)
				return nil
			}
			for _, executionTask := range action.executionTasks {
				if err := s.enqueueExecution(ctx, executionTask); err != nil {
					return err
				}
			}
		}
	}
}

func (s *occBlockSTMScheduler) enqueueExecution(ctx context.Context, task occExecutionTask) error {
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case s.executionQueue <- task:
		return nil
	}
}

func (s *occBlockSTMScheduler) enqueueValidation(ctx context.Context, task occValidationTask) error {
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case s.validationQueue <- task:
		return nil
	}
}

func (s *occBlockSTMScheduler) handleValidation(task occValidationTask) (occValidationAction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	txIndex := task.execution.txIndex
	if txIndex < s.nextToValidate || task.execution.incarnation < s.latestScheduled[txIndex] {
		return occValidationAction{}, nil
	}
	s.ready[txIndex] = task
	s.readySet[txIndex] = true
	return s.drainReadyLocked()
}

func (s *occBlockSTMScheduler) drainReadyLocked() (occValidationAction, error) {
	var action occValidationAction
	for {
		accepted := false
		for s.nextToValidate < len(s.results) && s.readySet[s.nextToValidate] {
			txIndex := s.nextToValidate
			task := s.ready[txIndex]
			if task.execution.incarnation < s.latestScheduled[txIndex] {
				s.readySet[txIndex] = false
				continue
			}
			needsRerun, err := s.needsRerunLocked(task)
			if err != nil {
				return action, err
			}
			if needsRerun {
				rerun, err := s.newRerunTaskLocked(task)
				if err != nil {
					return action, err
				}
				action.executionTasks = append(action.executionTasks, rerun)
				break
			}
			if err := s.acceptReadyLocked(txIndex, task.result); err != nil {
				return action, err
			}
			accepted = true
		}
		futureScheduled, err := s.scheduleFutureRerunsLocked(&action)
		if err != nil {
			return action, err
		}
		if !accepted && !futureScheduled {
			break
		}
	}
	action.complete = s.nextToValidate == len(s.results)
	return action, nil
}

func (s *occBlockSTMScheduler) scheduleFutureRerunsLocked(action *occValidationAction) (bool, error) {
	scheduled := false
	for txIndex := s.nextToValidate + 1; txIndex < len(s.results); txIndex++ {
		if !s.readySet[txIndex] {
			continue
		}
		task := s.ready[txIndex]
		if task.execution.incarnation < s.latestScheduled[txIndex] {
			s.readySet[txIndex] = false
			continue
		}
		needsRerun, err := s.needsRerunLocked(task)
		if err != nil {
			return scheduled, err
		}
		if !needsRerun {
			continue
		}
		rerun, err := s.newRerunTaskLocked(task)
		if err != nil {
			return scheduled, err
		}
		action.executionTasks = append(action.executionTasks, rerun)
		scheduled = true
	}
	return scheduled, nil
}

func (s *occBlockSTMScheduler) needsRerunLocked(task occValidationTask) (bool, error) {
	txIndex := task.execution.txIndex
	if task.result.err != nil {
		if txIndex == s.nextToValidate && task.execution.sourcePrefix >= txIndex {
			return false, task.result.err
		}
		return s.nextToValidate > task.execution.sourcePrefix, nil
	}
	accepted := validateSTMResultAgainstPrefix(
		&s.validation,
		s.writes,
		task.result,
		s.cumulativeGasUsed,
		s.runner.blockGasLimit,
		task.execution.sourcePrefix,
	)
	if accepted {
		return false, nil
	}
	if txIndex == s.nextToValidate || s.nextToValidate > task.execution.sourcePrefix {
		return true, nil
	}
	return false, nil
}

func (s *occBlockSTMScheduler) newRerunTaskLocked(task occValidationTask) (occExecutionTask, error) {
	nextIncarnation := task.execution.incarnation + 1
	if nextIncarnation >= occMaxTxIncarnations {
		s.validation.valid = false
		s.validation.fallbackReason = occFallbackReasonMaxIncarnation
		return occExecutionTask{}, errOCCMaxIncarnation
	}
	s.validation.rerunCount++
	txIndex := task.execution.txIndex
	sourcePrefix := s.nextToValidate
	availableGas := uint64(0)
	if s.cumulativeGasUsed < s.runner.blockGasLimit {
		availableGas = s.runner.blockGasLimit - s.cumulativeGasUsed
	}
	s.readySet[txIndex] = false
	s.latestScheduled[txIndex] = nextIncarnation
	return occExecutionTask{
		txIndex:      txIndex,
		txIndexUint:  task.execution.txIndexUint,
		incarnation:  nextIncarnation,
		sourcePrefix: sourcePrefix,
		source:       s.prefix.clone(),
		gasLimit:     availableGas,
	}, nil
}

func (s *occBlockSTMScheduler) acceptReadyLocked(txIndex int, result occTxExecution) error {
	if result.gasUsed > math.MaxUint64-s.cumulativeGasUsed {
		s.validation.valid = false
		s.validation.fallbackReason = occFallbackReasonGasOverflow
		return errors.New(occFallbackReasonGasOverflow)
	}
	s.cumulativeGasUsed += result.gasUsed
	s.results[txIndex] = result
	s.prefix.apply(result)
	s.writes.addAllAt(txIndex, result.writeSet)
	s.writes.addCommutativeBalanceDeltasAt(txIndex, result.commutativeBalanceDeltas)
	s.readySet[txIndex] = false
	s.nextToValidate++
	return nil
}

func (s *occBlockSTMScheduler) finish(err error) {
	s.finishOnce.Do(func() {
		s.finishErr = err
		close(s.done)
		if s.cancel != nil {
			s.cancel()
		}
	})
}

func (s *occBlockSTMScheduler) result() ([]occTxExecution, StateChangeSet, occValidationResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	validation := s.validation
	if s.finishErr != nil {
		return nil, StateChangeSet{}, validation, s.finishErr
	}
	results := append([]occTxExecution(nil), s.results...)
	return results, s.prefix.ChangeSet(), validation, nil
}

type occValidationResult struct {
	valid          bool
	fallbackReason string
	rerunCount     uint64
	conflictCount  uint64
	conflicts      map[occConflictAggregationKey]uint64
}

type occConflictAggregationKey struct {
	access  string
	kind    stateAccessKind
	address common.Address
	slot    common.Hash
}

const (
	occFallbackReasonConflict       = "conflict"
	occFallbackReasonGasLimit       = "gas_limit"
	occFallbackReasonGasOverflow    = "gas_overflow"
	occFallbackReasonMaxIncarnation = "max_incarnation"
)

func validateSTMResultAgainstPrefix(
	validation *occValidationResult,
	writes *stateAccessIndex,
	result occTxExecution,
	cumulativeGasUsed uint64,
	gasLimit uint64,
	sourcePrefix int,
) bool {
	if result.gasUsed > math.MaxUint64-cumulativeGasUsed {
		validation.valid = false
		validation.fallbackReason = occFallbackReasonGasOverflow
		return false
	}
	if cumulativeGasUsed > gasLimit || result.gasLimit > gasLimit-cumulativeGasUsed {
		validation.valid = false
		validation.fallbackReason = occFallbackReasonGasLimit
		return false
	}
	conflictsBefore := validation.conflictCount
	validation.addConflicts("read", writes, result.readSet, sourcePrefix)
	validation.addConflicts("write", writes, result.writeSet, sourcePrefix)
	if validation.conflictCount == conflictsBefore {
		return true
	}
	validation.valid = false
	validation.fallbackReason = occFallbackReasonConflict
	return false
}

func (r *occValidationResult) addConflicts(access string, writes *stateAccessIndex, set map[stateAccessKey]struct{}, sourcePrefix int) {
	for key := range set {
		if !writes.conflictsWithAfter(key, sourcePrefix) {
			continue
		}
		if r.conflicts == nil {
			r.conflicts = map[occConflictAggregationKey]uint64{}
		}
		r.conflictCount++
		r.conflicts[occConflictAggregationKey{
			access:  access,
			kind:    key.kind,
			address: key.address,
			slot:    key.slot,
		}]++
	}
}

func (r occValidationResult) stats(fallback bool) OCCStats {
	stats := OCCStats{
		Attempted:      true,
		Fallback:       fallback,
		FallbackReason: r.fallbackReason,
		RerunCount:     r.rerunCount,
		ConflictCount:  r.conflictCount,
	}
	if len(r.conflicts) == 0 {
		return stats
	}
	keys := make([]occConflictAggregationKey, 0, len(r.conflicts))
	for key := range r.conflicts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left, right := keys[i], keys[j]
		if left.access != right.access {
			return left.access < right.access
		}
		if left.kind != right.kind {
			return left.kind < right.kind
		}
		if cmp := bytes.Compare(left.address[:], right.address[:]); cmp != 0 {
			return cmp < 0
		}
		return bytes.Compare(left.slot[:], right.slot[:]) < 0
	})
	for _, key := range keys {
		stats.ConflictSamples = append(stats.ConflictSamples, OCCConflictCount{
			Access:  key.access,
			Kind:    key.kind.String(),
			Address: key.address,
			Slot:    key.slot,
			Count:   r.conflicts[key],
		})
	}
	return stats
}

func (k stateAccessKind) String() string {
	switch k {
	case stateAccessAccount:
		return "account"
	case stateAccessBalance:
		return "balance"
	case stateAccessNonce:
		return "nonce"
	case stateAccessCode:
		return "code"
	case stateAccessStorage:
		return "storage"
	default:
		return "unknown"
	}
}

func (e *Executor) mergeOCCResults(ctx context.Context, results []occTxExecution, changeSet StateChangeSet) (*BlockResult, error) {
	blockResult, err := e.acquireBlockResult(ctx, len(results))
	if err != nil {
		return nil, err
	}
	blockResult.prepareIndexedResults(len(results))
	var logIndex uint
	for i, result := range results {
		blockResult.GasUsed += result.gasUsed
		result.txResult.CumulativeGasUsed = blockResult.GasUsed
		result.receipt.CumulativeGasUsed = blockResult.GasUsed
		for _, log := range result.receipt.Logs {
			log.Index = logIndex
			logIndex++
		}
		blockResult.Txs[i] = result.txResult
		blockResult.Receipts[i] = result.receipt
	}
	changeSet.cloneInto(&blockResult.ChangeSet)
	return blockResult, nil
}

type blockSTMState struct {
	source        StateReader
	balances      map[common.Address]*big.Int
	nonces        map[common.Address]uint64
	code          map[common.Address][]byte
	storageClears map[common.Address]struct{}
	storage       map[storageChangeKey]common.Hash
}

func newBlockSTMState(source StateReader) *blockSTMState {
	if source == nil {
		source = NewMemoryState()
	}
	return &blockSTMState{
		source:        source,
		balances:      map[common.Address]*big.Int{},
		nonces:        map[common.Address]uint64{},
		code:          map[common.Address][]byte{},
		storageClears: map[common.Address]struct{}{},
		storage:       map[storageChangeKey]common.Hash{},
	}
}

func (s *blockSTMState) clone() *blockSTMState {
	copied := &blockSTMState{
		source:        s.source,
		balances:      make(map[common.Address]*big.Int, len(s.balances)),
		nonces:        make(map[common.Address]uint64, len(s.nonces)),
		code:          make(map[common.Address][]byte, len(s.code)),
		storageClears: make(map[common.Address]struct{}, len(s.storageClears)),
		storage:       make(map[storageChangeKey]common.Hash, len(s.storage)),
	}
	for addr, balance := range s.balances {
		copied.balances[addr] = cloneBig(balance)
	}
	for addr, nonce := range s.nonces {
		copied.nonces[addr] = nonce
	}
	for addr, code := range s.code {
		copied.code[addr] = cloneBytes(code)
	}
	for addr := range s.storageClears {
		copied.storageClears[addr] = struct{}{}
	}
	for key, value := range s.storage {
		copied.storage[key] = value
	}
	return copied
}

func (s *blockSTMState) GetBalance(addr common.Address) *big.Int {
	if balance, ok := s.balances[addr]; ok {
		return cloneBig(balance)
	}
	return s.source.GetBalance(addr)
}

func (s *blockSTMState) GetNonce(addr common.Address) uint64 {
	if nonce, ok := s.nonces[addr]; ok {
		return nonce
	}
	return s.source.GetNonce(addr)
}

func (s *blockSTMState) GetCode(addr common.Address) []byte {
	if code, ok := s.code[addr]; ok {
		return cloneBytes(code)
	}
	return s.source.GetCode(addr)
}

func (s *blockSTMState) GetState(addr common.Address, key common.Hash) common.Hash {
	if value, ok := s.storage[storageChangeKey{address: addr, key: key}]; ok {
		return value
	}
	if _, ok := s.storageClears[addr]; ok {
		return common.Hash{}
	}
	return s.source.GetState(addr, key)
}

func (s *blockSTMState) apply(result occTxExecution) {
	for _, change := range result.changeSet.Balances {
		delta := result.commutativeBalanceDeltas[change.Address]
		_, normalWrite := result.writeSet[stateAccessKey{kind: stateAccessBalance, address: change.Address}]
		if delta != nil && !normalWrite {
			balance := s.GetBalance(change.Address)
			balance.Add(balance, delta)
			s.balances[change.Address] = balance
			continue
		}
		s.balances[change.Address] = cloneBig(change.Balance)
	}
	for _, change := range result.changeSet.Nonces {
		s.nonces[change.Address] = change.Nonce
	}
	for _, change := range result.changeSet.Code {
		if change.Delete {
			s.code[change.Address] = nil
		} else {
			s.code[change.Address] = cloneBytes(change.Code)
		}
	}
	for _, addr := range result.changeSet.StorageClears {
		s.storageClears[addr] = struct{}{}
		for key := range s.storage {
			if key.address == addr {
				delete(s.storage, key)
			}
		}
	}
	for _, change := range result.changeSet.Storage {
		s.storage[storageChangeKey{address: change.Address, key: change.Key}] = change.Value
	}
}

func (s *blockSTMState) ChangeSet() StateChangeSet {
	var changes StateChangeSet
	balanceAddrs := sortedAddressesFromBigMap(s.balances)
	for _, addr := range balanceAddrs {
		balance := cloneBig(s.balances[addr])
		if balance.Cmp(s.source.GetBalance(addr)) == 0 {
			continue
		}
		changes.Balances = append(changes.Balances, BalanceChange{Address: addr, Balance: balance})
	}
	nonceAddrs := sortedAddressesFromUint64Map(s.nonces)
	for _, addr := range nonceAddrs {
		if s.nonces[addr] == s.source.GetNonce(addr) {
			continue
		}
		changes.Nonces = append(changes.Nonces, NonceChange{Address: addr, Nonce: s.nonces[addr]})
	}
	codeAddrs := sortedAddressesFromBytesMap(s.code)
	for _, addr := range codeAddrs {
		code := cloneBytes(s.code[addr])
		if bytes.Equal(code, s.source.GetCode(addr)) {
			continue
		}
		changes.Code = append(changes.Code, CodeChange{Address: addr, Code: code, Delete: len(code) == 0})
	}
	storageClearAddrs := sortedAddressesFromSet(s.storageClears)
	changes.StorageClears = append(changes.StorageClears, storageClearAddrs...)

	storageKeys := make([]storageChangeKey, 0, len(s.storage))
	for key := range s.storage {
		storageKeys = append(storageKeys, key)
	}
	sort.Slice(storageKeys, func(i, j int) bool {
		if cmp := bytes.Compare(storageKeys[i].address[:], storageKeys[j].address[:]); cmp != 0 {
			return cmp < 0
		}
		return bytes.Compare(storageKeys[i].key[:], storageKeys[j].key[:]) < 0
	})
	for _, key := range storageKeys {
		value := s.storage[key]
		baseValue := s.source.GetState(key.address, key.key)
		if _, cleared := s.storageClears[key.address]; cleared {
			baseValue = common.Hash{}
		}
		if value == baseValue {
			continue
		}
		changes.Storage = append(changes.Storage, StorageChange{
			Address: key.address,
			Key:     key.key,
			Value:   value,
			Delete:  value == (common.Hash{}),
		})
	}
	return changes
}

type stateAccessIndex struct {
	exact              map[stateAccessKey]int
	account            map[common.Address]int
	touched            map[common.Address]int
	commutativeBalance map[common.Address]int
}

func newStateAccessIndex() *stateAccessIndex {
	return &stateAccessIndex{
		exact:              map[stateAccessKey]int{},
		account:            map[common.Address]int{},
		touched:            map[common.Address]int{},
		commutativeBalance: map[common.Address]int{},
	}
}

func (i *stateAccessIndex) conflictsWith(key stateAccessKey) bool {
	return i.conflictsWithAfter(key, 0)
}

func (i *stateAccessIndex) conflictsWithAfter(key stateAccessKey, sourcePrefix int) bool {
	if i.hasWriteAtOrAfter(i.exact, key, sourcePrefix) {
		return true
	}
	if i.hasAddressWriteAtOrAfter(i.account, key.address, sourcePrefix) {
		return true
	}
	if key.kind == stateAccessAccount {
		if i.hasAddressWriteAtOrAfter(i.touched, key.address, sourcePrefix) {
			return true
		}
	}
	if key.kind == stateAccessStorage {
		return false
	}
	if key.kind != stateAccessAccount && key.kind != stateAccessBalance {
		return false
	}
	return i.hasAddressWriteAtOrAfter(i.commutativeBalance, key.address, sourcePrefix)
}

func (i *stateAccessIndex) addAll(set map[stateAccessKey]struct{}) {
	i.addAllAt(math.MaxInt, set)
}

func (i *stateAccessIndex) addAllAt(txIndex int, set map[stateAccessKey]struct{}) {
	for key := range set {
		i.recordWrite(i.exact, key, txIndex)
		// Exist/Empty account reads depend on account metadata, not storage slots.
		if key.kind != stateAccessStorage {
			i.recordAddressWrite(i.touched, key.address, txIndex)
		}
		if key.kind == stateAccessAccount {
			i.recordAddressWrite(i.account, key.address, txIndex)
		}
	}
}

func (i *stateAccessIndex) addCommutativeBalanceDeltas(deltas map[common.Address]*big.Int) {
	i.addCommutativeBalanceDeltasAt(math.MaxInt, deltas)
}

func (i *stateAccessIndex) addCommutativeBalanceDeltasAt(txIndex int, deltas map[common.Address]*big.Int) {
	for addr, delta := range deltas {
		if delta == nil || delta.Sign() == 0 {
			continue
		}
		i.recordAddressWrite(i.commutativeBalance, addr, txIndex)
	}
}

func (i *stateAccessIndex) hasWriteAtOrAfter(writes map[stateAccessKey]int, key stateAccessKey, sourcePrefix int) bool {
	txIndex, ok := writes[key]
	return ok && txIndex >= sourcePrefix
}

func (i *stateAccessIndex) hasAddressWriteAtOrAfter(writes map[common.Address]int, addr common.Address, sourcePrefix int) bool {
	txIndex, ok := writes[addr]
	return ok && txIndex >= sourcePrefix
}

func (i *stateAccessIndex) recordWrite(writes map[stateAccessKey]int, key stateAccessKey, txIndex int) {
	if existing, ok := writes[key]; ok && existing >= txIndex {
		return
	}
	writes[key] = txIndex
}

func (i *stateAccessIndex) recordAddressWrite(writes map[common.Address]int, addr common.Address, txIndex int) {
	if existing, ok := writes[addr]; ok && existing >= txIndex {
		return
	}
	writes[addr] = txIndex
}

type storageChangeKey struct {
	address common.Address
	key     common.Hash
}

func (cs StateChangeSet) cloneInto(dst *StateChangeSet) {
	dst.resetForReuse()
	for _, change := range cs.Balances {
		dst.Balances = append(dst.Balances, BalanceChange{
			Address: change.Address,
			Balance: cloneBig(change.Balance),
		})
	}
	dst.Nonces = append(dst.Nonces, cs.Nonces...)
	for _, change := range cs.Code {
		dst.Code = append(dst.Code, CodeChange{
			Address: change.Address,
			Code:    cloneBytes(change.Code),
			Delete:  change.Delete,
		})
	}
	dst.StorageClears = append(dst.StorageClears, cs.StorageClears...)
	dst.Storage = append(dst.Storage, cs.Storage...)
}

func sortedAddressesFromBigMap(values map[common.Address]*big.Int) []common.Address {
	addrs := make([]common.Address, 0, len(values))
	for addr := range values {
		addrs = append(addrs, addr)
	}
	sort.Slice(addrs, func(i, j int) bool {
		return bytes.Compare(addrs[i][:], addrs[j][:]) < 0
	})
	return addrs
}

func sortedAddressesFromUint64Map(values map[common.Address]uint64) []common.Address {
	addrs := make([]common.Address, 0, len(values))
	for addr := range values {
		addrs = append(addrs, addr)
	}
	sort.Slice(addrs, func(i, j int) bool {
		return bytes.Compare(addrs[i][:], addrs[j][:]) < 0
	})
	return addrs
}

func sortedAddressesFromBytesMap(values map[common.Address][]byte) []common.Address {
	addrs := make([]common.Address, 0, len(values))
	for addr := range values {
		addrs = append(addrs, addr)
	}
	sort.Slice(addrs, func(i, j int) bool {
		return bytes.Compare(addrs[i][:], addrs[j][:]) < 0
	})
	return addrs
}

func sortedAddressesFromSet(values map[common.Address]struct{}) []common.Address {
	addrs := make([]common.Address, 0, len(values))
	for addr := range values {
		addrs = append(addrs, addr)
	}
	sort.Slice(addrs, func(i, j int) bool {
		return bytes.Compare(addrs[i][:], addrs[j][:]) < 0
	})
	return addrs
}

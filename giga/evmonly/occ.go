package evmonly

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
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
	incarnation              int
	sourcePrefix             int
	err                      error
}

type occTxRange struct {
	start     int
	end       int
	startUint uint
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
	return occSpeculativeRunner{
		executor:      e,
		req:           req,
		chainConfig:   e.chainConfig(req.Context),
		blockCtx:      buildBlockContext(req.Context),
		baseFee:       cloneOptionalBig(req.Context.BaseFee),
		blockGasLimit: req.Context.GasLimit,
	}
}

func (e *Executor) executeBlockOCC(ctx context.Context, req PreparedBlock, source StateReader) (*BlockResult, error) {
	runner := newOCCSpeculativeRunner(e, req)
	workers := min(e.cfg.OCCWorkers, len(req.Txs))
	executionPool := e.occPool

	results := make([]occTxExecution, len(req.Txs))
	chunkSize := occChunkSize(len(req.Txs), workers)
	e.blockPhases.SetPhase("occ_speculate")
	if err := runner.runRanges(ctx, executionPool, occRanges(len(req.Txs), chunkSize), source, runner.blockGasLimit, results); err != nil {
		if errors.Is(err, errOCCWorkerPoolClosed) {
			return e.executeBlockOCCSequentialFallback(ctx, req, source, occValidationResult{}, occFallbackReasonWorkerPoolClosed)
		}
		return nil, err
	}

	e.blockPhases.SetPhase("occ_validate")
	results, finalState, validation, err := e.validateBlockSTM(ctx, runner, executionPool, source, results)
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
	result.OCCStats = validation.stats(false)
	return result, nil
}

func (e *Executor) executeBlockOCCSequentialFallback(ctx context.Context, req PreparedBlock, source StateReader, validation occValidationResult, reason string) (*BlockResult, error) {
	if reason != "" {
		validation.fallbackReason = reason
	}
	result, err := e.executeBlockSequential(ctx, req, source)
	if err != nil {
		return nil, err
	}
	result.OCCStats = validation.stats(true)
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

func (r occSpeculativeRunner) runRanges(
	ctx context.Context,
	pool *occWorkerPool,
	ranges []occTxRange,
	source StateReader,
	gasLimit uint64,
	results []occTxExecution,
) error {
	if len(ranges) == 0 {
		return ctx.Err()
	}
	return pool.Run(ctx, len(ranges), func(workerCtx context.Context, workerID int, workers int) error {
		for rangeIndex := workerID; rangeIndex < len(ranges); rangeIndex += workers {
			txRange := ranges[rangeIndex]
			for idx, idxUint := txRange.start, txRange.startUint; idx < txRange.end; idx, idxUint = idx+1, idxUint+1 {
				if err := workerCtx.Err(); err != nil {
					return err
				}
				task := occExecutionTask{
					txIndex:      idx,
					txIndexUint:  idxUint,
					incarnation:  0,
					sourcePrefix: 0,
					source:       source,
					gasLimit:     gasLimit,
				}
				if err := r.executeTaskInto(workerCtx, task, results); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (r occSpeculativeRunner) runTasks(
	ctx context.Context,
	pool *occWorkerPool,
	tasks []occExecutionTask,
	results []occTxExecution,
) error {
	if len(tasks) == 0 {
		return ctx.Err()
	}
	return pool.Run(ctx, len(tasks), func(workerCtx context.Context, workerID int, workers int) error {
		for taskIndex := workerID; taskIndex < len(tasks); taskIndex += workers {
			if err := workerCtx.Err(); err != nil {
				return err
			}
			if err := r.executeTaskInto(workerCtx, tasks[taskIndex], results); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r occSpeculativeRunner) executeTaskInto(ctx context.Context, task occExecutionTask, results []occTxExecution) error {
	result, err := r.executeTx(ctx, task.source, task.txIndex, task.txIndexUint, task.gasLimit)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		result.err = err
	}
	result.incarnation = task.incarnation
	result.sourcePrefix = task.sourcePrefix
	results[task.txIndex] = result
	return nil
}

func occRanges(txCount int, chunkSize int) []occTxRange {
	chunkSize = max(chunkSize, 1)
	ranges := make([]occTxRange, 0, (txCount+chunkSize-1)/chunkSize)
	startUint := uint(0)
	for start := 0; start < txCount; {
		end := min(start+chunkSize, txCount)
		ranges = append(ranges, occTxRange{start: start, end: end, startUint: startUint})
		width := end - start
		start = end
		startUint += uint(width) //nolint:gosec // width is non-negative and bounded by txCount.
	}
	return ranges
}

func occChunkSize(txCount int, workers int) int {
	if txCount <= 0 || workers <= 0 {
		return 1
	}
	targetChunks := workers * 8
	chunkSize := (txCount + targetChunks - 1) / targetChunks
	return min(max(chunkSize, 1), 256)
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
	blockGasLimit uint64,
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
	gasPool := new(core.GasPool).AddGas(blockGasLimit)
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
	readSet, writeSet := stateDB.accessSets()
	gasLimit := p.Tx.Gas()
	if txResult.Rejected {
		// A rejected transaction occupies no block gas, so validation must not charge its declared limit.
		gasLimit = 0
	}
	result := occTxExecution{
		txResult:                 txResult,
		receipt:                  receipt,
		readSet:                  readSet,
		writeSet:                 writeSet,
		gasUsed:                  txResult.GasUsed,
		gasLimit:                 gasLimit,
		commutativeBalanceDeltas: stateDB.commutativeBalanceDeltasBig(),
	}
	if err != nil {
		return result, fmt.Errorf("execute tx %d %s: %w", txIndex, p.Tx.Hash(), err)
	}
	stateDB.ChangeSetInto(&result.changeSet)
	return result, nil
}

type blockSTMValidationState struct {
	prefix            *blockSTMState
	writes            *stateAccessIndex
	cumulativeGasUsed uint64
	nextToValidate    int
}

func newBlockSTMValidationState(source StateReader) *blockSTMValidationState {
	return &blockSTMValidationState{
		prefix: newBlockSTMState(source),
		writes: newStateAccessIndex(),
	}
}

func (e *Executor) validateBlockSTM(
	ctx context.Context,
	runner occSpeculativeRunner,
	pool *occWorkerPool,
	source StateReader,
	results []occTxExecution,
) ([]occTxExecution, *blockSTMState, occValidationResult, error) {
	state := newBlockSTMValidationState(source)
	validation := occValidationResult{}
	if err := state.writes.indexResults(ctx, pool, results); err != nil {
		return nil, nil, validation, err
	}
	// The frontier alternates between a parallel pass, which accepts every result up to the first
	// one that needs attention, and the serial frontier, which handles that one. A block whose
	// transactions depend on each other one after another would make each parallel pass accept
	// nothing, so after such a pass the serial frontier keeps going for a stretch that doubles each
	// time it happens again.
	serialUntil := 0
	serialStretch := occMinParallelValidation
	for state.nextToValidate < len(results) {
		if state.nextToValidate >= serialUntil {
			accepted, err := e.acceptValidatedPrefix(ctx, runner, pool, results, state, &validation)
			if err != nil {
				return nil, nil, validation, err
			}
			if state.nextToValidate == len(results) {
				break
			}
			if accepted < occMinParallelValidation {
				serialUntil = state.nextToValidate + serialStretch
				serialStretch *= 2
			} else {
				serialStretch = occMinParallelValidation
			}
		}
		end := min(len(results), max(serialUntil, state.nextToValidate+1))
		rerun, err := validateBlockSTMFrontier(ctx, runner, results, state, &validation, end)
		if err != nil {
			return nil, nil, validation, err
		}
		if rerun == nil {
			continue
		}
		if err := runner.runTasks(ctx, pool, []occExecutionTask{*rerun}, results); err != nil {
			return nil, nil, validation, err
		}
		// The rerun's writes join the index; the previous incarnation's stay, which can only cost a
		// later transaction a rerun it did not need, never miss a conflict.
		state.writes.addAllAt(rerun.txIndex, results[rerun.txIndex].writeSet)
		state.writes.addCommutativeBalanceDeltasAt(rerun.txIndex, results[rerun.txIndex].commutativeBalanceDeltas)
	}
	return results, state.prefix, validation, nil
}

// validateBlockSTMFrontier accepts results in block order on the calling goroutine until it reaches
// end or a result that has to be rerun, which it returns as a task.
func validateBlockSTMFrontier(
	ctx context.Context,
	runner occSpeculativeRunner,
	results []occTxExecution,
	state *blockSTMValidationState,
	validation *occValidationResult,
	end int,
) (*occExecutionTask, error) {
	for state.nextToValidate < end {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		txIndex := state.nextToValidate
		txIndexUint := uint(txIndex) //nolint:gosec // transaction count is bounded by available memory.
		result := results[txIndex]
		validation.validationCount++
		needsRerun, err := needsSTMRerun(
			validation,
			state.writes,
			result,
			state.cumulativeGasUsed,
			runner.blockGasLimit,
			result.sourcePrefix,
			txIndex,
			txIndex,
		)
		if err != nil {
			return nil, err
		}
		if needsRerun {
			rerun, err := newSTMRerunTask(
				validation,
				result,
				txIndex,
				txIndexUint,
				state.prefix,
				availableGas(runner.blockGasLimit, state.cumulativeGasUsed),
			)
			if err != nil {
				return nil, err
			}
			return &rerun, nil
		}
		if result.gasUsed > math.MaxUint64-state.cumulativeGasUsed {
			validation.fallbackReason = occFallbackReasonGasOverflow
			return nil, errors.New(occFallbackReasonGasOverflow)
		}
		state.cumulativeGasUsed += result.gasUsed
		state.prefix.apply(result)
		state.nextToValidate++
	}
	return nil, nil
}

func needsSTMRerun(
	validation *occValidationResult,
	writes *stateAccessIndex,
	result occTxExecution,
	cumulativeGasUsed uint64,
	gasLimit uint64,
	sourcePrefix int,
	txIndex int,
	nextToValidate int,
) (bool, error) {
	if result.err != nil {
		if txIndex == nextToValidate && sourcePrefix >= txIndex {
			return false, result.err
		}
		return nextToValidate > sourcePrefix, nil
	}
	if err := stmGasValidationError(validation, result, cumulativeGasUsed, gasLimit); err != nil {
		if txIndex == nextToValidate && sourcePrefix >= txIndex {
			return false, err
		}
		return nextToValidate > sourcePrefix, nil
	}
	return !validateSTMResultAgainstPrefix(validation, writes, result, cumulativeGasUsed, gasLimit, sourcePrefix, txIndex), nil
}

func newSTMRerunTask(
	validation *occValidationResult,
	result occTxExecution,
	txIndex int,
	txIndexUint uint,
	source StateReader,
	gasLimit uint64,
) (occExecutionTask, error) {
	nextIncarnation := result.incarnation + 1
	if nextIncarnation >= occMaxTxIncarnations {
		validation.fallbackReason = occFallbackReasonMaxIncarnation
		return occExecutionTask{}, errOCCMaxIncarnation
	}
	validation.rerunCount++
	validation.maxIncarnation = max(validation.maxIncarnation, utils.Clamp[uint64](nextIncarnation))
	return occExecutionTask{
		txIndex:      txIndex,
		txIndexUint:  txIndexUint,
		incarnation:  nextIncarnation,
		sourcePrefix: txIndex,
		source:       source,
		gasLimit:     gasLimit,
	}, nil
}

func availableGas(gasLimit uint64, cumulativeGasUsed uint64) uint64 {
	if cumulativeGasUsed >= gasLimit {
		return 0
	}
	return gasLimit - cumulativeGasUsed
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

type occValidationResult struct {
	fallbackReason  string
	rerunCount      uint64
	maxIncarnation  uint64
	conflictCount   uint64
	validationCount uint64
	conflicts       map[occConflictAggregationKey]uint64
}

type occConflictAggregationKey struct {
	access  string
	kind    stateAccessKind
	address common.Address
	slot    common.Hash
}

const (
	occFallbackReasonConflict         = "conflict"
	occFallbackReasonGasLimit         = "gas_limit"
	occFallbackReasonGasOverflow      = "gas_overflow"
	occFallbackReasonMaxIncarnation   = "max_incarnation"
	occFallbackReasonWorkerPoolClosed = "worker_pool_closed"
)

// validateSTMResultAgainstPrefix reports whether the result at txIndex, executed against the prefix
// [0, sourcePrefix), still holds once the writes at [sourcePrefix, txIndex) are accepted, recording
// every conflict it finds.
func validateSTMResultAgainstPrefix(
	validation *occValidationResult,
	writes *stateAccessIndex,
	result occTxExecution,
	cumulativeGasUsed uint64,
	gasLimit uint64,
	sourcePrefix int,
	txIndex int,
) bool {
	if err := stmGasValidationError(validation, result, cumulativeGasUsed, gasLimit); err != nil {
		return false
	}
	conflictsBefore := validation.conflictCount
	validation.addConflicts("read", writes, result.readSet, sourcePrefix, txIndex)
	validation.addConflicts("write", writes, result.writeSet, sourcePrefix, txIndex)
	if validation.conflictCount == conflictsBefore {
		return true
	}
	validation.fallbackReason = occFallbackReasonConflict
	return false
}

func stmGasValidationError(validation *occValidationResult, result occTxExecution, cumulativeGasUsed uint64, gasLimit uint64) error {
	reason, err := stmGasFailure(result, cumulativeGasUsed, gasLimit)
	if err != nil {
		validation.fallbackReason = reason
	}
	return err
}

// stmGasFailure returns the fallback reason and error for a result that does not fit the block gas
// accounting after cumulativeGasUsed, or an empty reason and nil error when it does.
func stmGasFailure(result occTxExecution, cumulativeGasUsed uint64, gasLimit uint64) (string, error) {
	if result.gasUsed > math.MaxUint64-cumulativeGasUsed {
		return occFallbackReasonGasOverflow, errors.New(occFallbackReasonGasOverflow)
	}
	if cumulativeGasUsed > gasLimit || result.gasLimit > gasLimit-cumulativeGasUsed {
		return occFallbackReasonGasLimit, core.ErrGasLimitReached
	}
	return "", nil
}

func (r *occValidationResult) addConflicts(access string, writes *stateAccessIndex, set map[stateAccessKey]struct{}, sourcePrefix int, txIndex int) {
	for key := range set {
		if !writes.conflictsWithin(key, sourcePrefix, txIndex) {
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
		Attempted:       true,
		Fallback:        fallback,
		RerunCount:      r.rerunCount,
		MaxIncarnation:  r.maxIncarnation,
		ConflictCount:   r.conflictCount,
		ValidationCount: r.validationCount,
	}
	if fallback {
		stats.FallbackReason = r.fallbackReason
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

func (e *Executor) mergeOCCResults(ctx context.Context, results []occTxExecution, finalState *blockSTMState) (*BlockResult, error) {
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
	if err := finalState.changeSetIntoParallel(ctx, e.occPool, &blockResult.ChangeSet); err != nil {
		blockResult.Release()
		return nil, err
	}
	return blockResult, nil
}

// blockSTMState is the accepted prefix of a block's state, split into address shards so that
// applying results and emitting the changeset can proceed shard by shard on different workers.
type blockSTMState struct {
	source StateReader
	shards [occStateShards]blockSTMShard
}

// blockSTMShard holds the accepted writes for the addresses of one shard.
type blockSTMShard struct {
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
	state := &blockSTMState{source: source}
	for i := range state.shards {
		state.shards[i] = blockSTMShard{
			balances:      map[common.Address]*big.Int{},
			nonces:        map[common.Address]uint64{},
			code:          map[common.Address][]byte{},
			storageClears: map[common.Address]struct{}{},
			storage:       map[storageChangeKey]common.Hash{},
		}
	}
	return state
}

func (s *blockSTMState) shard(addr common.Address) *blockSTMShard {
	return &s.shards[occShardOf(addr)]
}

func (s *blockSTMState) GetBalance(addr common.Address) *big.Int {
	if balance, ok := s.shard(addr).balances[addr]; ok {
		return cloneBig(balance)
	}
	return s.source.GetBalance(addr)
}

func (s *blockSTMState) GetNonce(addr common.Address) uint64 {
	if nonce, ok := s.shard(addr).nonces[addr]; ok {
		return nonce
	}
	return s.source.GetNonce(addr)
}

func (s *blockSTMState) GetCode(addr common.Address) []byte {
	if code, ok := s.shard(addr).code[addr]; ok {
		return cloneBytes(code)
	}
	return s.source.GetCode(addr)
}

func (s *blockSTMState) GetState(addr common.Address, key common.Hash) common.Hash {
	shard := s.shard(addr)
	if value, ok := shard.storage[storageChangeKey{address: addr, key: key}]; ok {
		return value
	}
	if _, ok := shard.storageClears[addr]; ok {
		return common.Hash{}
	}
	return s.source.GetState(addr, key)
}

// apply folds one accepted result into the prefix.
func (s *blockSTMState) apply(result occTxExecution) {
	s.applyOwned(result, occAllShards)
}

// applyOwned folds the parts of one accepted result whose addresses fall in the shards owns
// reports true for. Two callers with disjoint ownership can run concurrently.
func (s *blockSTMState) applyOwned(result occTxExecution, owns occShardOwnership) {
	for _, change := range result.changeSet.Balances {
		if !owns(occShardOf(change.Address)) {
			continue
		}
		shard := s.shard(change.Address)
		delta := result.commutativeBalanceDeltas[change.Address]
		_, normalWrite := result.writeSet[stateAccessKey{kind: stateAccessBalance, address: change.Address}]
		if delta != nil && !normalWrite {
			balance := cloneBig(s.GetBalance(change.Address))
			balance.Add(balance, delta)
			shard.balances[change.Address] = balance
			continue
		}
		shard.balances[change.Address] = cloneBig(change.Balance)
	}
	for _, change := range result.changeSet.Nonces {
		if owns(occShardOf(change.Address)) {
			s.shard(change.Address).nonces[change.Address] = change.Nonce
		}
	}
	for _, change := range result.changeSet.Code {
		if !owns(occShardOf(change.Address)) {
			continue
		}
		if change.Delete {
			s.shard(change.Address).code[change.Address] = nil
		} else {
			s.shard(change.Address).code[change.Address] = cloneBytes(change.Code)
		}
	}
	for _, addr := range result.changeSet.StorageClears {
		if !owns(occShardOf(addr)) {
			continue
		}
		shard := s.shard(addr)
		shard.storageClears[addr] = struct{}{}
		for key := range shard.storage {
			if key.address == addr {
				delete(shard.storage, key)
			}
		}
	}
	for _, change := range result.changeSet.Storage {
		if owns(occShardOf(change.Address)) {
			s.shard(change.Address).storage[storageChangeKey{address: change.Address, key: change.Key}] = change.Value
		}
	}
}

func (s *blockSTMState) ChangeSet() StateChangeSet {
	var changes StateChangeSet
	s.ChangeSetInto(&changes)
	return changes
}

// baseAccounts serves an account's pre-block fields, reading the row once however many fields a
// caller asks for. It is scoped to one shard of one merge and is not safe for concurrent use.
type baseAccounts struct {
	source StateReader
	reader accountSnapshotReader
	seen   map[common.Address]accountSnapshot
}

func newBaseAccounts(source StateReader) *baseAccounts {
	b := &baseAccounts{source: source, seen: map[common.Address]accountSnapshot{}}
	b.reader, _ = source.(accountSnapshotReader)
	return b
}

func (b *baseAccounts) get(addr common.Address) accountSnapshot {
	if snapshot, ok := b.seen[addr]; ok {
		return snapshot
	}
	var snapshot accountSnapshot
	if b.reader != nil {
		if read, served := b.reader.ReadAccount(addr); served {
			snapshot = read
			b.seen[addr] = snapshot
			return snapshot
		}
	}
	snapshot = accountSnapshot{
		Balance: b.source.GetBalance(addr),
		Nonce:   b.source.GetNonce(addr),
		Code:    b.source.GetCode(addr),
	}
	b.seen[addr] = snapshot
	return snapshot
}

func (b *baseAccounts) balance(addr common.Address) *big.Int {
	if balance := b.get(addr).Balance; balance != nil {
		return balance
	}
	return new(big.Int)
}

func (b *baseAccounts) nonce(addr common.Address) uint64 { return b.get(addr).Nonce }
func (b *baseAccounts) code(addr common.Address) []byte  { return b.get(addr).Code }

// ChangeSetInto writes the block's net state changes, in canonical order, on the calling goroutine.
func (s *blockSTMState) ChangeSetInto(changes *StateChangeSet) {
	changes.resetForReuse()
	for i := range s.shards {
		s.shards[i].changeSetInto(s.source, changes)
	}
}

// changeSetInto appends the shard's net changes, in canonical order, to changes. Shards partition
// the address space in canonical order, so appending shard by shard yields a canonically ordered
// changeset.
func (h *blockSTMShard) changeSetInto(source StateReader, changes *StateChangeSet) {
	// The three loops below each compare against the same accounts, and balance, nonce and code hash
	// share one row. Reading per field would resolve that row three times per address.
	base := newBaseAccounts(source)
	balanceAddrs := sortedAddressesFromBigMap(h.balances)
	for _, addr := range balanceAddrs {
		balance := cloneBig(h.balances[addr])
		if balance.Cmp(base.balance(addr)) == 0 {
			continue
		}
		changes.Balances = append(changes.Balances, BalanceChange{Address: addr, Balance: balance})
	}
	nonceAddrs := sortedAddressesFromUint64Map(h.nonces)
	for _, addr := range nonceAddrs {
		if h.nonces[addr] == base.nonce(addr) {
			continue
		}
		changes.Nonces = append(changes.Nonces, NonceChange{Address: addr, Nonce: h.nonces[addr]})
	}
	codeAddrs := sortedAddressesFromBytesMap(h.code)
	for _, addr := range codeAddrs {
		code := cloneBytes(h.code[addr])
		if bytes.Equal(code, base.code(addr)) {
			continue
		}
		changes.Code = append(changes.Code, CodeChange{Address: addr, Code: code, Delete: len(code) == 0})
	}
	storageClearAddrs := sortedAddressesFromSet(h.storageClears)
	changes.StorageClears = append(changes.StorageClears, storageClearAddrs...)

	storageKeys := make([]storageChangeKey, 0, len(h.storage))
	for key := range h.storage {
		storageKeys = append(storageKeys, key)
	}
	sort.Slice(storageKeys, func(i, j int) bool {
		if cmp := bytes.Compare(storageKeys[i].address[:], storageKeys[j].address[:]); cmp != 0 {
			return cmp < 0
		}
		return bytes.Compare(storageKeys[i].key[:], storageKeys[j].key[:]) < 0
	})
	for _, key := range storageKeys {
		value := h.storage[key]
		baseValue := source.GetState(key.address, key.key)
		if _, cleared := h.storageClears[key.address]; cleared {
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
}

// stateAccessIndex records, per state key and per address, the range of transaction indexes that
// wrote it. It is split into address shards so that disjoint shards can be filled concurrently.
type stateAccessIndex struct {
	shards [occStateShards]stateAccessShard
}

// stateAccessShard indexes the writes to the addresses of one shard.
type stateAccessShard struct {
	exact              map[stateAccessKey]txIndexSpan
	account            map[common.Address]txIndexSpan
	touched            map[common.Address]txIndexSpan
	commutativeBalance map[common.Address]txIndexSpan
}

// txIndexSpan is the lowest and highest transaction index recorded for one key.
type txIndexSpan struct {
	first int
	last  int
}

func newStateAccessIndex() *stateAccessIndex {
	index := &stateAccessIndex{}
	for i := range index.shards {
		index.shards[i] = stateAccessShard{
			exact:              map[stateAccessKey]txIndexSpan{},
			account:            map[common.Address]txIndexSpan{},
			touched:            map[common.Address]txIndexSpan{},
			commutativeBalance: map[common.Address]txIndexSpan{},
		}
	}
	return index
}

func (i *stateAccessIndex) shard(addr common.Address) *stateAccessShard {
	return &i.shards[occShardOf(addr)]
}

// conflictsWithin reports whether a write recorded for key would invalidate a read or write of it
// by a transaction that executed against the prefix [0, lo) and sits at index hi, i.e. whether the
// key was written at an index in [lo, hi). Only the first and last write of a key are recorded, so
// the answer can be a false positive when both fall outside the range with a gap across it; it is
// never a false negative, and a false positive costs one rerun, not correctness.
func (i *stateAccessIndex) conflictsWithin(key stateAccessKey, lo int, hi int) bool {
	shard := i.shard(key.address)
	if writtenWithin(shard.exact, key, lo, hi) {
		return true
	}
	if writtenWithin(shard.account, key.address, lo, hi) {
		return true
	}
	if key.kind == stateAccessAccount {
		if writtenWithin(shard.touched, key.address, lo, hi) {
			return true
		}
	}
	if key.kind == stateAccessStorage {
		return false
	}
	if key.kind != stateAccessAccount && key.kind != stateAccessBalance {
		return false
	}
	return writtenWithin(shard.commutativeBalance, key.address, lo, hi)
}

// writtenWithin reports whether the span recorded for key may contain an index in [lo, hi).
func writtenWithin[K comparable](writes map[K]txIndexSpan, key K, lo int, hi int) bool {
	span, ok := writes[key]
	return ok && lo < hi && span.last >= lo && span.first < hi
}

// addAll records the set as written by transactions at every index.
func (i *stateAccessIndex) addAll(set map[stateAccessKey]struct{}) {
	i.addSpan(txIndexSpan{first: 0, last: math.MaxInt}, set, occAllShards)
}

// addAllAt records the set as written by the transaction at txIndex.
func (i *stateAccessIndex) addAllAt(txIndex int, set map[stateAccessKey]struct{}) {
	i.addSpan(txIndexSpan{first: txIndex, last: txIndex}, set, occAllShards)
}

func (i *stateAccessIndex) addSpan(span txIndexSpan, set map[stateAccessKey]struct{}, owns occShardOwnership) {
	for key := range set {
		if !owns(occShardOf(key.address)) {
			continue
		}
		shard := i.shard(key.address)
		recordSpan(shard.exact, key, span)
		// Exist/Empty account reads depend on account metadata, not storage slots.
		if key.kind != stateAccessStorage {
			recordSpan(shard.touched, key.address, span)
		}
		if key.kind == stateAccessAccount {
			recordSpan(shard.account, key.address, span)
		}
	}
}

// addCommutativeBalanceDeltasAt records the non-zero deltas as balance credits by the transaction
// at txIndex.
func (i *stateAccessIndex) addCommutativeBalanceDeltasAt(txIndex int, deltas map[common.Address]*big.Int) {
	i.addCommutativeBalanceDeltas(txIndex, deltas, occAllShards)
}

func (i *stateAccessIndex) addCommutativeBalanceDeltas(txIndex int, deltas map[common.Address]*big.Int, owns occShardOwnership) {
	for addr, delta := range deltas {
		if delta == nil || delta.Sign() == 0 || !owns(occShardOf(addr)) {
			continue
		}
		recordSpan(i.shard(addr).commutativeBalance, addr, txIndexSpan{first: txIndex, last: txIndex})
	}
}

// recordSpan widens the span recorded for key to include span.
func recordSpan[K comparable](writes map[K]txIndexSpan, key K, span txIndexSpan) {
	if existing, ok := writes[key]; ok {
		span.first = min(span.first, existing.first)
		span.last = max(span.last, existing.last)
	}
	writes[key] = span
}

type storageChangeKey struct {
	address common.Address
	key     common.Hash
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

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

func (e *Executor) executeBlockOCC(ctx context.Context, req PreparedBlock) (*BlockResult, error) {
	runner := newOCCSpeculativeRunner(e, req)
	workers := min(e.cfg.OCCWorkers, len(req.Txs))
	executionPool := e.occPool

	results := make([]occTxExecution, len(req.Txs))
	chunkSize := occChunkSize(len(req.Txs), workers)
	if err := runner.runRanges(ctx, executionPool, occRanges(len(req.Txs), chunkSize), e.state, runner.blockGasLimit, results); err != nil {
		if errors.Is(err, errOCCWorkerPoolClosed) {
			return e.executeBlockOCCSequentialFallback(ctx, req, occValidationResult{}, occFallbackReasonWorkerPoolClosed)
		}
		return nil, err
	}

	results, finalState, validation, err := e.validateBlockSTM(ctx, runner, executionPool, results)
	if errors.Is(err, errOCCMaxIncarnation) || errors.Is(err, errOCCWorkerPoolClosed) {
		reason := validation.fallbackReason
		switch {
		case errors.Is(err, errOCCWorkerPoolClosed):
			reason = occFallbackReasonWorkerPoolClosed
		case errors.Is(err, errOCCMaxIncarnation) && reason == "":
			reason = occFallbackReasonMaxIncarnation
		}
		return e.executeBlockOCCSequentialFallback(ctx, req, validation, reason)
	}
	if err != nil {
		return nil, err
	}
	result, err := e.mergeOCCResults(ctx, results, finalState)
	if err != nil {
		return nil, err
	}
	result.OCCStats = validation.stats(false)
	return result, nil
}

func (e *Executor) executeBlockOCCSequentialFallback(ctx context.Context, req PreparedBlock, validation occValidationResult, reason string) (*BlockResult, error) {
	if reason != "" {
		validation.fallbackReason = reason
	}
	result, err := e.executeBlockSequential(ctx, req)
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
	readSet, writeSet := stateDB.accessSets()
	result := occTxExecution{
		txResult:                 txResult,
		receipt:                  receipt,
		readSet:                  readSet,
		writeSet:                 writeSet,
		gasUsed:                  txResult.GasUsed,
		gasLimit:                 p.Tx.Gas(),
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
	results []occTxExecution,
) ([]occTxExecution, *blockSTMState, occValidationResult, error) {
	state := newBlockSTMValidationState(e.state)
	validation := occValidationResult{}
	for state.nextToValidate < len(results) {
		rerun, err := validateBlockSTMFrontier(ctx, runner, results, state, &validation)
		if err != nil {
			return nil, nil, validation, err
		}
		if rerun == nil {
			continue
		}
		if err := runner.runTasks(ctx, pool, []occExecutionTask{*rerun}, results); err != nil {
			return nil, nil, validation, err
		}
	}
	return results, state.prefix, validation, nil
}

func validateBlockSTMFrontier(
	ctx context.Context,
	runner occSpeculativeRunner,
	results []occTxExecution,
	state *blockSTMValidationState,
	validation *occValidationResult,
) (*occExecutionTask, error) {
	for state.nextToValidate < len(results) {
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
		state.writes.addAllAt(txIndex, result.writeSet)
		state.writes.addCommutativeBalanceDeltasAt(txIndex, result.commutativeBalanceDeltas)
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
	return !validateSTMResultAgainstPrefix(validation, writes, result, cumulativeGasUsed, gasLimit, sourcePrefix), nil
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

func validateSTMResultAgainstPrefix(
	validation *occValidationResult,
	writes *stateAccessIndex,
	result occTxExecution,
	cumulativeGasUsed uint64,
	gasLimit uint64,
	sourcePrefix int,
) bool {
	if err := stmGasValidationError(validation, result, cumulativeGasUsed, gasLimit); err != nil {
		return false
	}
	conflictsBefore := validation.conflictCount
	validation.addConflicts("read", writes, result.readSet, sourcePrefix)
	validation.addConflicts("write", writes, result.writeSet, sourcePrefix)
	if validation.conflictCount == conflictsBefore {
		return true
	}
	validation.fallbackReason = occFallbackReasonConflict
	return false
}

func stmGasValidationError(validation *occValidationResult, result occTxExecution, cumulativeGasUsed uint64, gasLimit uint64) error {
	if result.gasUsed > math.MaxUint64-cumulativeGasUsed {
		validation.fallbackReason = occFallbackReasonGasOverflow
		return errors.New(occFallbackReasonGasOverflow)
	}
	if cumulativeGasUsed > gasLimit || result.gasLimit > gasLimit-cumulativeGasUsed {
		validation.fallbackReason = occFallbackReasonGasLimit
		return core.ErrGasLimitReached
	}
	return nil
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
		Attempted:       true,
		Fallback:        fallback,
		RerunCount:      r.rerunCount,
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
	finalState.ChangeSetInto(&blockResult.ChangeSet)
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
			balance := cloneBig(s.GetBalance(change.Address))
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
	s.ChangeSetInto(&changes)
	return changes
}

func (s *blockSTMState) ChangeSetInto(changes *StateChangeSet) {
	changes.resetForReuse()
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

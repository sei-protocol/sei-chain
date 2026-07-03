package evmonly

import (
	"bytes"
	"context"
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
}

type occTxRange struct {
	start int
	end   int
}

func (e *Executor) executeBlockOCC(ctx context.Context, req PreparedBlock) (*BlockResult, error) {
	chainConfig := e.chainConfig(req.Context)
	blockCtx := buildBlockContext(req.Context)
	baseFee := cloneOptionalBig(req.Context.BaseFee)
	gasLimit := req.Context.GasLimit
	if gasLimit == 0 {
		gasLimit = math.MaxUint64
	}

	workers := e.cfg.OCCWorkers
	txCount := len(req.Txs)
	if workers > txCount {
		workers = txCount
	}
	results := make([]occTxExecution, txCount)
	chunkSize := occChunkSize(txCount, workers)
	pool := e.occPool
	if pool == nil {
		pool = newOCCWorkerPool(workers)
		defer pool.Close()
	}
	if err := pool.Run(ctx, occRanges(txCount, chunkSize), func(workerCtx context.Context, txRange occTxRange) error {
		for idx := txRange.start; idx < txRange.end; idx++ {
			result, err := e.executeTxSpeculative(workerCtx, req, idx, chainConfig, blockCtx, baseFee, gasLimit)
			if err != nil {
				return err
			}
			results[idx] = result
		}
		return nil
	}); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		result, seqErr := e.executeBlockSequential(ctx, req)
		if seqErr != nil {
			return nil, seqErr
		}
		result.OCCStats = OCCStats{
			Attempted:      true,
			Fallback:       true,
			FallbackReason: occFallbackReasonSpeculativeError,
		}
		return result, nil
	}
	validation := validateOCCResults(results, gasLimit)
	if !validation.valid {
		result, err := e.executeBlockSequential(ctx, req)
		if err != nil {
			return nil, err
		}
		result.OCCStats = validation.stats(true)
		return result, nil
	}
	result, err := e.mergeOCCResults(ctx, results)
	if err != nil {
		return nil, err
	}
	result.OCCStats = validation.stats(false)
	return result, nil
}

func occRanges(txCount int, chunkSize int) []occTxRange {
	if chunkSize <= 0 {
		chunkSize = 1
	}
	ranges := make([]occTxRange, 0, (txCount+chunkSize-1)/chunkSize)
	for start := 0; start < txCount; start += chunkSize {
		end := start + chunkSize
		if end > txCount {
			end = txCount
		}
		ranges = append(ranges, occTxRange{start: start, end: end})
	}
	return ranges
}

func occChunkSize(txCount int, workers int) int {
	if txCount <= 0 || workers <= 0 {
		return 1
	}
	targetChunks := workers * 8
	chunkSize := (txCount + targetChunks - 1) / targetChunks
	if chunkSize < 16 {
		return 16
	}
	if chunkSize > 256 {
		return 256
	}
	return chunkSize
}

func (e *Executor) executeTxSpeculative(
	ctx context.Context,
	req PreparedBlock,
	txIndex int,
	chainConfig *params.ChainConfig,
	blockCtx vm.BlockContext,
	baseFee *big.Int,
	gasLimit uint64,
) (occTxExecution, error) {
	p := req.Txs[txIndex]
	stateDB := newNativeStateDB(e.state)
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
		uint(txIndex),
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

type occValidationResult struct {
	valid          bool
	fallbackReason string
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
	occFallbackReasonConflict         = "conflict"
	occFallbackReasonGasLimit         = "gas_limit"
	occFallbackReasonGasOverflow      = "gas_overflow"
	occFallbackReasonSpeculativeError = "speculative_error"
)

func validateOCCResults(results []occTxExecution, gasLimit uint64) occValidationResult {
	writes := newStateAccessIndex()
	var totalGasLimit uint64
	var totalGasUsed uint64
	validation := occValidationResult{valid: true}
	for _, result := range results {
		if len(result.changeSet.StorageClears) > 0 {
			validation.valid = false
			validation.fallbackReason = occFallbackReasonConflict
			return validation
		}
		if result.gasLimit > math.MaxUint64-totalGasLimit || result.gasUsed > math.MaxUint64-totalGasUsed {
			validation.valid = false
			validation.fallbackReason = occFallbackReasonGasOverflow
			return validation
		}
		totalGasLimit += result.gasLimit
		totalGasUsed += result.gasUsed
		if totalGasLimit > gasLimit {
			validation.valid = false
			validation.fallbackReason = occFallbackReasonGasLimit
			return validation
		}
		validation.addConflicts("read", writes, result.readSet)
		validation.addConflicts("write", writes, result.writeSet)
		writes.addAll(result.writeSet)
		writes.addCommutativeBalanceDeltas(result.commutativeBalanceDeltas)
	}
	if validation.conflictCount > 0 {
		validation.valid = false
		validation.fallbackReason = occFallbackReasonConflict
	}
	return validation
}

func (r *occValidationResult) addConflicts(access string, writes *stateAccessIndex, set map[stateAccessKey]struct{}) {
	for key := range set {
		if !writes.conflictsWith(key) {
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

func (e *Executor) mergeOCCResults(ctx context.Context, results []occTxExecution) (*BlockResult, error) {
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
	mergeChangeSetsInto(results, &blockResult.ChangeSet)
	return blockResult, nil
}

type stateAccessIndex struct {
	exact              map[stateAccessKey]struct{}
	account            map[common.Address]struct{}
	touched            map[common.Address]struct{}
	commutativeBalance map[common.Address]struct{}
}

func newStateAccessIndex() *stateAccessIndex {
	return &stateAccessIndex{
		exact:              map[stateAccessKey]struct{}{},
		account:            map[common.Address]struct{}{},
		touched:            map[common.Address]struct{}{},
		commutativeBalance: map[common.Address]struct{}{},
	}
}

func (i *stateAccessIndex) conflictsWith(key stateAccessKey) bool {
	if _, ok := i.exact[key]; ok {
		return true
	}
	if _, ok := i.account[key.address]; ok {
		return true
	}
	if key.kind == stateAccessAccount {
		if _, ok := i.touched[key.address]; ok {
			return true
		}
	}
	if key.kind == stateAccessStorage {
		return false
	}
	_, ok := i.commutativeBalance[key.address]
	return ok
}

func (i *stateAccessIndex) addAll(set map[stateAccessKey]struct{}) {
	for key := range set {
		i.exact[key] = struct{}{}
		// Exist/Empty account reads depend on account metadata, not storage slots.
		if key.kind != stateAccessStorage {
			i.touched[key.address] = struct{}{}
		}
		if key.kind == stateAccessAccount {
			i.account[key.address] = struct{}{}
		}
	}
}

func (i *stateAccessIndex) addCommutativeBalanceDeltas(deltas map[common.Address]*big.Int) {
	for addr, delta := range deltas {
		if delta == nil || delta.Sign() == 0 {
			continue
		}
		i.commutativeBalance[addr] = struct{}{}
	}
}

type storageChangeKey struct {
	address common.Address
	key     common.Hash
}

func mergeChangeSetsInto(results []occTxExecution, merged *StateChangeSet) {
	merged.resetForReuse()
	balances := map[common.Address]*big.Int{}
	balanceBases := map[common.Address]*big.Int{}
	balanceDeltas := map[common.Address]*big.Int{}
	nonces := map[common.Address]uint64{}
	code := map[common.Address]CodeChange{}
	storageClears := map[common.Address]struct{}{}
	storage := map[storageChangeKey]StorageChange{}

	for _, result := range results {
		for _, change := range result.changeSet.Balances {
			delta := result.commutativeBalanceDeltas[change.Address]
			if delta == nil {
				balances[change.Address] = cloneBig(change.Balance)
				continue
			}
			base := new(big.Int).Sub(cloneBig(change.Balance), delta)
			balanceBases[change.Address] = base
			if _, normalWrite := result.writeSet[stateAccessKey{kind: stateAccessBalance, address: change.Address}]; normalWrite {
				balances[change.Address] = cloneBig(base)
			}
		}
		for addr, delta := range result.commutativeBalanceDeltas {
			if balanceDeltas[addr] == nil {
				balanceDeltas[addr] = new(big.Int)
			}
			balanceDeltas[addr].Add(balanceDeltas[addr], delta)
		}
		for _, change := range result.changeSet.Nonces {
			nonces[change.Address] = change.Nonce
		}
		for _, change := range result.changeSet.Code {
			code[change.Address] = CodeChange{
				Address: change.Address,
				Code:    cloneBytes(change.Code),
				Delete:  change.Delete,
			}
		}
		for _, addr := range result.changeSet.StorageClears {
			storageClears[addr] = struct{}{}
			for key := range storage {
				if key.address == addr {
					delete(storage, key)
				}
			}
		}
		for _, change := range result.changeSet.Storage {
			storage[storageChangeKey{address: change.Address, key: change.Key}] = change
		}
	}
	for addr, delta := range balanceDeltas {
		base := balances[addr]
		if base == nil {
			base = balanceBases[addr]
		}
		if base == nil {
			base = new(big.Int)
		} else {
			base = cloneBig(base)
		}
		balances[addr] = base.Add(base, delta)
	}

	balanceAddrs := sortedAddressesFromBigMap(balances)
	for _, addr := range balanceAddrs {
		merged.Balances = append(merged.Balances, BalanceChange{Address: addr, Balance: cloneBig(balances[addr])})
	}
	nonceAddrs := sortedAddressesFromUint64Map(nonces)
	for _, addr := range nonceAddrs {
		merged.Nonces = append(merged.Nonces, NonceChange{Address: addr, Nonce: nonces[addr]})
	}
	codeAddrs := sortedAddressesFromCodeMap(code)
	for _, addr := range codeAddrs {
		change := code[addr]
		change.Code = cloneBytes(change.Code)
		merged.Code = append(merged.Code, change)
	}
	storageClearAddrs := sortedAddressesFromSet(storageClears)
	merged.StorageClears = append(merged.StorageClears, storageClearAddrs...)
	storageKeys := make([]storageChangeKey, 0, len(storage))
	for key := range storage {
		storageKeys = append(storageKeys, key)
	}
	sort.Slice(storageKeys, func(i, j int) bool {
		if cmp := bytes.Compare(storageKeys[i].address[:], storageKeys[j].address[:]); cmp != 0 {
			return cmp < 0
		}
		return bytes.Compare(storageKeys[i].key[:], storageKeys[j].key[:]) < 0
	})
	for _, key := range storageKeys {
		merged.Storage = append(merged.Storage, storage[key])
	}
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

func sortedAddressesFromCodeMap(values map[common.Address]CodeChange) []common.Address {
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

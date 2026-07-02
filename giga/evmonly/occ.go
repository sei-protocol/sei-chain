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
	"golang.org/x/sync/errgroup"
)

type occTxExecution struct {
	txResult  TxResult
	receipt   *ethtypes.Receipt
	changeSet StateChangeSet
	readSet   map[stateAccessKey]struct{}
	writeSet  map[stateAccessKey]struct{}
	gasUsed   uint64
}

type occTxRange struct {
	start int
	end   int
}

func (e *Executor) executeBlockOCC(ctx context.Context, req BlockRequest) (*BlockResult, error) {
	chainConfig := e.chainConfig(req.Context)
	signer := ethtypes.MakeSigner(chainConfig, new(big.Int).SetUint64(req.Context.Number), req.Context.Time)
	blockCtx := buildBlockContext(req.Context)
	baseFee := cloneBig(req.Context.BaseFee)
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
	jobs := make(chan occTxRange)
	group, groupCtx := errgroup.WithContext(ctx)

	group.Go(func() error {
		defer close(jobs)
		for start := 0; start < txCount; start += chunkSize {
			end := start + chunkSize
			if end > txCount {
				end = txCount
			}
			select {
			case jobs <- occTxRange{start: start, end: end}:
			case <-groupCtx.Done():
				return groupCtx.Err()
			}
		}
		return nil
	})
	for range workers {
		group.Go(func() error {
			for {
				select {
				case <-groupCtx.Done():
					return groupCtx.Err()
				case txRange, ok := <-jobs:
					if !ok {
						return nil
					}
					for idx := txRange.start; idx < txRange.end; idx++ {
						result, err := e.executeTxSpeculative(groupCtx, req, idx, signer, chainConfig, blockCtx, baseFee, gasLimit)
						if err != nil {
							return err
						}
						results[idx] = result
					}
				}
			}
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	if !validateOCCResults(results, gasLimit) {
		return e.executeBlockSequential(ctx, req)
	}
	return mergeOCCResults(results), nil
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
	req BlockRequest,
	txIndex int,
	signer ethtypes.Signer,
	chainConfig *params.ChainConfig,
	blockCtx vm.BlockContext,
	baseFee *big.Int,
	gasLimit uint64,
) (occTxExecution, error) {
	tx, sender, err := parseTx(req.Txs[txIndex], signer)
	if err != nil {
		return occTxExecution{}, fmt.Errorf("parse tx %d: %w", txIndex, err)
	}
	p := parsedTx{tx: tx, sender: sender}
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
		signer,
	)
	if err != nil {
		return occTxExecution{}, fmt.Errorf("execute tx %d %s: %w", txIndex, p.tx.Hash(), err)
	}
	readSet, writeSet := stateDB.accessSets()
	return occTxExecution{
		txResult:  txResult,
		receipt:   receipt,
		changeSet: stateDB.ChangeSet(),
		readSet:   readSet,
		writeSet:  writeSet,
		gasUsed:   txResult.GasUsed,
	}, nil
}

func validateOCCResults(results []occTxExecution, gasLimit uint64) bool {
	writes := newStateAccessIndex()
	var totalGas uint64
	for _, result := range results {
		if result.gasUsed > math.MaxUint64-totalGas {
			return false
		}
		totalGas += result.gasUsed
		if totalGas > gasLimit {
			return false
		}
		if writes.conflictsWithAny(result.readSet) || writes.conflictsWithAny(result.writeSet) {
			return false
		}
		writes.addAll(result.writeSet)
	}
	return true
}

func mergeOCCResults(results []occTxExecution) *BlockResult {
	blockResult := &BlockResult{
		Txs:      make([]TxResult, len(results)),
		Receipts: make(ethtypes.Receipts, len(results)),
	}
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
	blockResult.ChangeSet = mergeChangeSets(results)
	return blockResult
}

type stateAccessIndex struct {
	exact   map[stateAccessKey]struct{}
	account map[common.Address]struct{}
	touched map[common.Address]struct{}
}

func newStateAccessIndex() *stateAccessIndex {
	return &stateAccessIndex{
		exact:   map[stateAccessKey]struct{}{},
		account: map[common.Address]struct{}{},
		touched: map[common.Address]struct{}{},
	}
}

func (i *stateAccessIndex) conflictsWithAny(set map[stateAccessKey]struct{}) bool {
	for key := range set {
		if i.conflictsWith(key) {
			return true
		}
	}
	return false
}

func (i *stateAccessIndex) conflictsWith(key stateAccessKey) bool {
	if _, ok := i.exact[key]; ok {
		return true
	}
	if _, ok := i.account[key.address]; ok {
		return true
	}
	if key.kind == stateAccessAccount {
		_, ok := i.touched[key.address]
		return ok
	}
	return false
}

func (i *stateAccessIndex) addAll(set map[stateAccessKey]struct{}) {
	for key := range set {
		i.exact[key] = struct{}{}
		i.touched[key.address] = struct{}{}
		if key.kind == stateAccessAccount {
			i.account[key.address] = struct{}{}
		}
	}
}

type storageChangeKey struct {
	address common.Address
	key     common.Hash
}

func mergeChangeSets(results []occTxExecution) StateChangeSet {
	balances := map[common.Address]*big.Int{}
	nonces := map[common.Address]uint64{}
	code := map[common.Address]CodeChange{}
	storage := map[storageChangeKey]StorageChange{}

	for _, result := range results {
		for _, change := range result.changeSet.Balances {
			balances[change.Address] = cloneBig(change.Balance)
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
		for _, change := range result.changeSet.Storage {
			storage[storageChangeKey{address: change.Address, key: change.Key}] = change
		}
	}

	var merged StateChangeSet
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
	return merged
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

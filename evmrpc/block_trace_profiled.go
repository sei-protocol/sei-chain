package evmrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	gethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	gethstate "github.com/ethereum/go-ethereum/core/state"
	gethtracing "github.com/ethereum/go-ethereum/core/tracing"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth/tracers"
	traceLogger "github.com/ethereum/go-ethereum/eth/tracers/logger"
	"github.com/ethereum/go-ethereum/eth/tracers/tracersutils"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/evmrpc/traceprofile"
)

const profiledDefaultTraceTimeout = 5 * time.Second
const profiledDefaultTraceReexec = uint64(128)
const maxProfiledTraceWorkers = 16

func shouldUseProfiledBlockTrace(config *tracers.TraceConfig) bool {
	return config == nil || config.Tracer == nil || *config.Tracer == ""
}

func (api *DebugAPI) profiledTraceBlockByNumber(ctx context.Context, number rpc.BlockNumber, config *tracers.TraceConfig) (interface{}, error) {
	block, metadata, err := api.backend.BlockByNumber(ctx, number)
	if err != nil {
		return nil, err
	}
	if block == nil {
		return nil, fmt.Errorf("block #%d not found", number)
	}
	return api.profiledTraceBlock(ctx, block, metadata, config)
}

func (api *DebugAPI) profiledTraceBlockByHash(ctx context.Context, hash gethcommon.Hash, config *tracers.TraceConfig) (interface{}, error) {
	block, metadata, err := api.backend.BlockByHash(ctx, hash)
	if err != nil {
		return nil, err
	}
	if block == nil {
		return nil, fmt.Errorf("block %s not found", hash.Hex())
	}
	return api.profiledTraceBlock(ctx, block, metadata, config)
}

func (api *DebugAPI) profiledTraceBlock(ctx context.Context, block *gethtypes.Block, metadata []tracersutils.TraceBlockMetadata, config *tracers.TraceConfig) ([]*tracers.TxTraceResult, error) {
	if block.NumberU64() == 0 {
		return nil, errors.New("genesis is not traceable")
	}

	parent, _, err := api.backend.BlockByNumber(ctx, rpc.BlockNumber(block.NumberU64()-1))
	if err != nil {
		return nil, err
	}
	if parent == nil || parent.Hash() != block.ParentHash() {
		parent, _, err = api.backend.BlockByHash(ctx, block.ParentHash())
		if err != nil {
			return nil, err
		}
		if parent == nil {
			return nil, fmt.Errorf("parent block %s not found", block.ParentHash().Hex())
		}
	}

	reexec := profiledDefaultTraceReexec
	if config != nil && config.Reexec != nil {
		reexec = *config.Reexec
	}
	statedb, release, err := api.backend.StateAtBlock(ctx, parent, reexec, nil, true, false)
	if err != nil {
		return nil, err
	}
	defer release()

	blockCtx, err := api.backend.GetBlockContext(ctx, block, statedb, api.backend)
	if err != nil {
		return nil, fmt.Errorf("cannot get block context: %w", err)
	}
	txs := block.Transactions()
	blockHash := block.Hash()
	signer := gethtypes.MakeSigner(api.backend.ChainConfig(), block.Number(), block.Time())
	results := make([]*tracers.TxTraceResult, len(txs))
	tracedCount := len(txs)
	if len(metadata) > 0 {
		tracedCount = 0
		for _, md := range metadata {
			if md.ShouldIncludeInTraceResult {
				tracedCount++
			}
		}
	}
	threads := min(runtime.NumCPU(), tracedCount)
	threads = min(threads, maxProfiledTraceWorkers)
	if threads <= 1 {
		return api.profiledTraceBlockSequential(ctx, block, metadata, config, statedb, blockCtx, signer, blockHash, results)
	}
	if recorder := traceprofile.FromContext(ctx); recorder != nil {
		recorder.AddCount("trace_block_parallel_workers", threads)
	}
	return api.profiledTraceBlockParallel(ctx, block, metadata, config, statedb, signer, blockHash, results, threads)
}

func (api *DebugAPI) profiledTraceBlockSequential(
	ctx context.Context,
	block *gethtypes.Block,
	metadata []tracersutils.TraceBlockMetadata,
	config *tracers.TraceConfig,
	statedb vm.StateDB,
	blockCtx vm.BlockContext,
	signer gethtypes.Signer,
	blockHash gethcommon.Hash,
	results []*tracers.TxTraceResult,
) ([]*tracers.TxTraceResult, error) {
	txs := block.Transactions()
	traceOne := func(i int, tx *gethtypes.Transaction) {
		msg, _ := core.TransactionToMessage(tx, signer, block.BaseFee())
		txctx := &tracers.Context{
			BlockHash:   blockHash,
			BlockNumber: block.Number(),
			TxIndex:     i,
			TxHash:      tx.Hash(),
		}
		res, err := api.profiledTraceTx(ctx, tx, msg, txctx, blockCtx, statedb, config, nil)
		if err != nil {
			results[i] = &tracers.TxTraceResult{TxHash: tx.Hash(), Error: err.Error()}
		} else {
			results[i] = &tracers.TxTraceResult{TxHash: tx.Hash(), Result: res}
		}
	}
	if len(metadata) == 0 {
		for i, tx := range txs {
			traceOne(i, tx)
		}
		return results, nil
	}
	for _, md := range metadata {
		if md.ShouldIncludeInTraceResult {
			i := md.IdxInEthBlock
			traceOne(i, txs[i])
			if results[i] != nil && results[i].Error != "" {
				statedb.RevertToSnapshot(0)
			}
			continue
		}
		if recorder := traceprofile.FromContext(ctx); recorder != nil {
			start := time.Now()
			recorder.AddCount("trace_block_runnable_count", 1)
			md.TraceRunnable(statedb)
			recorder.AddDuration("trace_block_runnable_total", time.Since(start))
		} else {
			md.TraceRunnable(statedb)
		}
	}
	return results, nil
}

type profiledTxTraceTask struct {
	index   int
	statedb vm.StateDB
}

func (api *DebugAPI) profiledTraceBlockParallel(
	ctx context.Context,
	block *gethtypes.Block,
	metadata []tracersutils.TraceBlockMetadata,
	config *tracers.TraceConfig,
	statedb vm.StateDB,
	signer gethtypes.Signer,
	blockHash gethcommon.Hash,
	results []*tracers.TxTraceResult,
	threads int,
) ([]*tracers.TxTraceResult, error) {
	txs := block.Transactions()
	jobs := make(chan *profiledTxTraceTask, threads)
	var pend sync.WaitGroup

	for th := 0; th < threads; th++ {
		pend.Add(1)
		go func() {
			defer pend.Done()
			for task := range jobs {
				tx := txs[task.index]
				msg, _ := core.TransactionToMessage(tx, signer, block.BaseFee())
				txctx := &tracers.Context{
					BlockHash:   blockHash,
					BlockNumber: block.Number(),
					TxIndex:     task.index,
					TxHash:      tx.Hash(),
				}
				blockCtx, err := api.backend.GetBlockContext(ctx, block, task.statedb, api.backend)
				if err != nil {
					results[task.index] = &tracers.TxTraceResult{TxHash: tx.Hash(), Error: err.Error()}
					continue
				}
				res, err := api.profiledTraceTx(ctx, tx, msg, txctx, blockCtx, task.statedb, config, nil)
				if err != nil {
					results[task.index] = &tracers.TxTraceResult{TxHash: tx.Hash(), Error: err.Error()}
				} else {
					results[task.index] = &tracers.TxTraceResult{TxHash: tx.Hash(), Result: res}
				}
			}
		}()
	}

	mainBlockCtx, err := api.backend.GetBlockContext(ctx, block, statedb, api.backend)
	if err != nil {
		close(jobs)
		pend.Wait()
		return nil, err
	}
	var failed error

	advanceState := func(i int, tx *gethtypes.Transaction) error {
		msg, _ := core.TransactionToMessage(tx, signer, block.BaseFee())
		txctx := &tracers.Context{
			BlockHash:   blockHash,
			BlockNumber: block.Number(),
			TxIndex:     i,
			TxHash:      tx.Hash(),
		}
		recorder := traceprofile.FromContext(ctx)
		if recorder != nil {
			start := time.Now()
			recorder.AddCount("trace_tx_advance_count", 1)
			err := api.profiledAdvanceTx(ctx, tx, msg, txctx, mainBlockCtx, statedb, nil)
			recorder.AddDuration("trace_tx_advance_total", time.Since(start))
			return err
		}
		return api.profiledAdvanceTx(ctx, tx, msg, txctx, mainBlockCtx, statedb, nil)
	}

	feedTraceTask := func(i int) error {
		task := &profiledTxTraceTask{statedb: statedb.Copy(), index: i}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case jobs <- task:
			return nil
		}
	}

	if len(metadata) == 0 {
		for i, tx := range txs {
			if err := feedTraceTask(i); err != nil {
				failed = err
				break
			}
			if err := advanceState(i, tx); err != nil {
				failed = err
				break
			}
		}
	} else {
		for _, md := range metadata {
			if md.ShouldIncludeInTraceResult {
				i := md.IdxInEthBlock
				if err := feedTraceTask(i); err != nil {
					failed = err
					break
				}
				if err := advanceState(i, txs[i]); err != nil {
					failed = err
					break
				}
				continue
			}
			if recorder := traceprofile.FromContext(ctx); recorder != nil {
				start := time.Now()
				recorder.AddCount("trace_block_runnable_count", 1)
				md.TraceRunnable(statedb)
				recorder.AddDuration("trace_block_runnable_total", time.Since(start))
			} else {
				md.TraceRunnable(statedb)
			}
		}
	}

	close(jobs)
	pend.Wait()
	if failed != nil {
		return nil, failed
	}
	return results, nil
}

func (api *DebugAPI) profiledAdvanceTx(
	ctx context.Context,
	tx *gethtypes.Transaction,
	message *core.Message,
	txctx *tracers.Context,
	vmctx vm.BlockContext,
	statedb vm.StateDB,
	precompiles vm.PrecompiledContracts,
) (returnErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}

	startingNonce := statedb.GetNonce(message.From)
	defer func() {
		if recover() != nil {
			returnErr = nil
		}
		nonce := statedb.GetNonce(message.From)
		if nonce == startingNonce {
			statedb.SetNonce(message.From, nonce+1, gethtracing.NonceChangeUnspecified)
		}
	}()

	txContext := core.NewEVMTxContext(message)
	evm := vm.NewEVM(vmctx, statedb, api.backend.ChainConfigAtHeight(vmctx.BlockNumber.Int64()), vm.Config{NoBaseFee: true}, api.backend.GetCustomPrecompiles(vmctx.BlockNumber.Int64()))
	if precompiles != nil {
		evm.SetPrecompiles(precompiles)
	}
	evm.SetTxContext(txContext)

	statedb.SetTxContext(txctx.TxHash, txctx.TxIndex)
	if err := api.backend.PrepareTx(statedb, tx); err != nil {
		return nil
	}

	var usedGas uint64
	_, _ = core.ApplyTransactionWithEVM(message, new(core.GasPool).AddGas(message.GasLimit), statedb, vmctx.BlockNumber, txctx.BlockHash, tx, &usedGas, evm)
	return nil
}

func (api *DebugAPI) profiledTraceTx(ctx context.Context, tx *gethtypes.Transaction, message *core.Message, txctx *tracers.Context, vmctx vm.BlockContext, statedb vm.StateDB, config *tracers.TraceConfig, precompiles vm.PrecompiledContracts) (value interface{}, returnErr error) {
	var (
		tracer    *tracers.Tracer
		tracerMtx *sync.Mutex
		err       error
		timeout   = profiledDefaultTraceTimeout
		usedGas   uint64
	)
	recorder := traceprofile.FromContext(ctx)
	if recorder != nil {
		recorder.AddCount("trace_tx_count", 1)
	}
	txStart := time.Now()

	startingNonce := statedb.GetNonce(message.From)
	defer func() {
		if r := recover(); r != nil {
			value, returnErr = profiledErrorTrace(fmt.Errorf("%s", r), tx, message, txctx, vmctx, config)
		}
		nonce := statedb.GetNonce(message.From)
		if nonce == startingNonce {
			statedb.SetNonce(message.From, nonce+1, gethtracing.NonceChangeUnspecified)
		}
		if recorder != nil {
			recorder.AddDuration("trace_tx_total", time.Since(txStart))
		}
	}()

	if config == nil {
		config = &tracers.TraceConfig{}
	}
	if config.Tracer == nil {
		start := time.Now()
		logger := traceLogger.NewStructLogger(config.Config)
		tracer = &tracers.Tracer{
			Hooks:     logger.Hooks(),
			GetResult: logger.GetResult,
			Stop:      logger.Stop,
		}
		if recorder != nil {
			recorder.AddDuration("trace_tx_tracer_init_total", time.Since(start))
		}
	} else {
		start := time.Now()
		tracer, err = tracers.DefaultDirectory.New(*config.Tracer, txctx, config.TracerConfig, api.backend.ChainConfigAtHeight(vmctx.BlockNumber.Int64()))
		if recorder != nil {
			recorder.AddDuration("trace_tx_tracer_init_total", time.Since(start))
		}
		if err != nil {
			return nil, err
		}
	}
	tracingStateDB := gethstate.NewHookedState(statedb, tracer.Hooks)
	tracerMtx = &sync.Mutex{}
	txContext := core.NewEVMTxContext(message)
	evm := vm.NewEVM(vmctx, tracingStateDB, api.backend.ChainConfigAtHeight(vmctx.BlockNumber.Int64()), vm.Config{Tracer: tracer.Hooks, NoBaseFee: true}, api.backend.GetCustomPrecompiles(vmctx.BlockNumber.Int64()))
	if precompiles != nil {
		evm.SetPrecompiles(precompiles)
	}
	evm.SetTxContext(txContext)

	if config.Timeout != nil {
		if timeout, err = time.ParseDuration(*config.Timeout); err != nil {
			return nil, err
		}
	}
	deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
	go func() {
		<-deadlineCtx.Done()
		if errors.Is(deadlineCtx.Err(), context.DeadlineExceeded) {
			tracerMtx.Lock()
			tracer.Stop(errors.New("execution timeout"))
			tracerMtx.Unlock()
			evm.Cancel()
		}
	}()
	defer cancel()

	statedb.SetTxContext(txctx.TxHash, txctx.TxIndex)
	if err := api.backend.PrepareTx(statedb, tx); err != nil {
		return profiledErrorTrace(err, tx, message, txctx, vmctx, config)
	}
	applyStart := time.Now()
	_, err = core.ApplyTransactionWithEVM(message, new(core.GasPool).AddGas(message.GasLimit), statedb, vmctx.BlockNumber, txctx.BlockHash, tx, &usedGas, evm)
	if recorder != nil {
		recorder.AddDuration("trace_tx_apply_total", time.Since(applyStart))
	}
	if err != nil {
		return profiledErrorTrace(err, tx, message, txctx, vmctx, config)
	}
	resultStart := time.Now()
	tracerMtx.Lock()
	res, err := tracer.GetResult()
	tracerMtx.Unlock()
	if recorder != nil {
		recorder.AddDuration("trace_tx_get_result_total", time.Since(resultStart))
	}
	if err == nil && errors.Is(deadlineCtx.Err(), context.DeadlineExceeded) {
		err = errors.New("execution timeout")
	}
	return res, err
}

func profiledErrorTrace(err error, tx *gethtypes.Transaction, message *core.Message, txctx *tracers.Context, vmctx vm.BlockContext, config *tracers.TraceConfig) (value interface{}, returnErr error) {
	if config != nil && config.Tracer != nil {
		switch *config.Tracer {
		case "callTracer":
			errTrace := map[string]interface{}{
				"from":    message.From.Hex(),
				"gas":     hexutil.Uint64(message.GasLimit),
				"gasUsed": "0x0",
				"input":   "0x",
				"error":   err.Error(),
				"type":    "CALL",
			}
			if message.Value != nil {
				errTrace["value"] = hexutil.Big(*message.Value)
			}
			if message.To != nil {
				errTrace["to"] = message.To.Hex()
			} else {
				errTrace["type"] = "CREATE"
			}
			if message.Data != nil {
				errTrace["input"] = hexutil.Encode(message.Data)
			}
			bz, marshalErr := json.Marshal(errTrace)
			if marshalErr != nil {
				return nil, fmt.Errorf("tracing failed: %w", marshalErr)
			}
			return json.RawMessage(bz), nil
		case "flatCallTracer":
			action := map[string]interface{}{
				"callType": "call",
				"from":     message.From.Hex(),
				"gas":      hexutil.Uint64(message.GasLimit),
				"input":    "0x",
			}
			if message.Value != nil {
				action["value"] = hexutil.Big(*message.Value)
			}
			if message.To != nil {
				action["to"] = message.To.Hex()
			}
			if message.Data != nil {
				action["input"] = hexutil.Encode(message.Data)
			}
			errTrace := map[string]interface{}{
				"action":      action,
				"blockHash":   txctx.BlockHash,
				"blockNumber": txctx.BlockNumber,
				"result": map[string]interface{}{
					"gasUsed": "0x0",
					"output":  "0x",
				},
				"subtraces":           0,
				"traceAddress":        []string{},
				"transactionHash":     tx.Hash(),
				"transactionPosition": txctx.TxIndex,
				"error":               err.Error(),
			}
			bz, marshalErr := json.Marshal([]map[string]interface{}{errTrace})
			if marshalErr != nil {
				return nil, fmt.Errorf("tracing failed: %w", marshalErr)
			}
			return json.RawMessage(bz), nil
		}
	}
	if strings.Contains(err.Error(), core.ErrInsufficientFunds.Error()) {
		return json.RawMessage(`{}`), nil
	}
	return nil, fmt.Errorf("tracing failed: %w", err)
}

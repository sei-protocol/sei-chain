package pipeline

import (
	"context"
	"sync"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/utils"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	pipelinetypes "github.com/sei-protocol/sei-chain/app/pipeline/types"
)

// ExecutorComponent is a long-running component that executes blocks sequentially
type ExecutorComponent struct {
	mu      sync.RWMutex
	in      <-chan *pipelinetypes.PreprocessedBlockWithContext
	out     chan<- *pipelinetypes.ExecutedBlock
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	// Dependencies
	helper                   pipelinetypes.ExecutionHelper
	beginBlock               func(ctx sdk.Context, req abci.RequestBeginBlock) abci.ResponseBeginBlock
	midBlock                 func(ctx sdk.Context, height int64) []abci.Event
	endBlock                 func(ctx sdk.Context, req abci.RequestEndBlock) abci.ResponseEndBlock
	writeState               func()
	getWorkingHash           func() []byte
	executeEVMTransaction    func(ctx sdk.Context, preprocessed *pipelinetypes.PreprocessedTx, helper pipelinetypes.ExecutionHelper) (*pipelinetypes.TransactionResult, error)
	executeCosmosTransaction func(ctx sdk.Context, tx sdk.Tx, txBytes []byte, helper pipelinetypes.ExecutionHelper) (*pipelinetypes.TransactionResult, error)
}

// NewExecutorComponent creates a new ExecutorComponent
func NewExecutorComponent(
	in <-chan *pipelinetypes.PreprocessedBlockWithContext,
	out chan<- *pipelinetypes.ExecutedBlock,
	helper pipelinetypes.ExecutionHelper,
	beginBlock func(ctx sdk.Context, req abci.RequestBeginBlock) abci.ResponseBeginBlock,
	midBlock func(ctx sdk.Context, height int64) []abci.Event,
	endBlock func(ctx sdk.Context, req abci.RequestEndBlock) abci.ResponseEndBlock,
	writeState func(),
	getWorkingHash func() []byte,
	executeEVMTransaction func(ctx sdk.Context, preprocessed *pipelinetypes.PreprocessedTx, helper pipelinetypes.ExecutionHelper) (*pipelinetypes.TransactionResult, error),
	executeCosmosTransaction func(ctx sdk.Context, tx sdk.Tx, txBytes []byte, helper pipelinetypes.ExecutionHelper) (*pipelinetypes.TransactionResult, error),
) *ExecutorComponent {
	return &ExecutorComponent{
		in:                       in,
		out:                      out,
		helper:                   helper,
		beginBlock:               beginBlock,
		midBlock:                 midBlock,
		endBlock:                 endBlock,
		writeState:               writeState,
		getWorkingHash:           getWorkingHash,
		executeEVMTransaction:    executeEVMTransaction,
		executeCosmosTransaction: executeCosmosTransaction,
	}
}

// Start begins executing blocks
func (e *ExecutorComponent) Start(ctx context.Context) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.started {
		return
	}

	e.ctx, e.cancel = context.WithCancel(ctx)
	e.started = true
	e.wg.Add(1)

	go func() {
		defer e.wg.Done()
		for {
			select {
			case <-e.ctx.Done():
				return
			case blockWithCtx, ok := <-e.in:
				if !ok {
					return
				}
				executed, err := e.executeBlock(blockWithCtx)
				if err != nil {
					// Log execution errors and continue (preprocessing errors are handled in executeBlock)
					blockWithCtx.Ctx.Logger().Error("execution failed", "error", err, "height", blockWithCtx.Block.Height)
					continue
				}
				select {
				case <-e.ctx.Done():
					return
				case e.out <- executed:
				}
			}
		}
	}()
}

// executeBlock executes a preprocessed block
func (e *ExecutorComponent) executeBlock(blockWithCtx *pipelinetypes.PreprocessedBlockWithContext) (*pipelinetypes.ExecutedBlock, error) {
	preprocessed := blockWithCtx.Block
	ctx := blockWithCtx.Ctx

	// Check if preprocessing failed - if so, return error ExecutedBlock immediately
	if preprocessed.PreprocessError != nil {
		return &pipelinetypes.ExecutedBlock{
			PreprocessedBlock: preprocessed,
			TxResults:         nil,
			Events:            nil,
			AppHash:           nil,
			EndBlockResp:      abci.ResponseEndBlock{},
			Ctx:               ctx,
		}, nil
	}

	// BeginBlock
	beginBlockReq := abci.RequestBeginBlock{
		Hash: preprocessed.Hash,
		ByzantineValidators: utils.Map(preprocessed.ByzantineValidators, func(mis abci.Misbehavior) abci.Evidence {
			return abci.Evidence(mis)
		}),
		LastCommitInfo: abci.LastCommitInfo{
			Round: preprocessed.LastCommit.Round,
			Votes: utils.Map(preprocessed.LastCommit.Votes, func(vote abci.VoteInfo) abci.VoteInfo {
				return abci.VoteInfo{
					Validator:       vote.Validator,
					SignedLastBlock: vote.SignedLastBlock,
				}
			}),
		},
		Header: tmproto.Header{
			ChainID:         ctx.ChainID(),
			Height:          preprocessed.Height,
			Time:            preprocessed.Time,
			ProposerAddress: ctx.BlockHeader().ProposerAddress,
		},
		Simulate: false,
	}
	beginBlockResp := e.beginBlock(ctx, beginBlockReq)
	events := beginBlockResp.Events

	// Reuse decoded transactions from preprocessing to avoid re-decoding
	typedTxs := preprocessed.TypedTxs
	if typedTxs == nil || len(typedTxs) != len(blockWithCtx.Txs) {
		// Fallback: decode if not available (shouldn't happen in normal flow)
		typedTxs = e.helper.DecodeTransactionsConcurrently(ctx, blockWithCtx.Txs)
	}
	
	// Build absoluteTxIndices for ALL transactions (not just preprocessed ones)
	// Transactions skipped during preprocessing will still be executed via DeliverTxBatch
	// and will fail with appropriate error codes from ante handlers
	absoluteTxIndices := make([]int, len(blockWithCtx.Txs))
	for i := range blockWithCtx.Txs {
		absoluteTxIndices[i] = i // Use original index for all transactions
	}

	// Execute transactions using OCC (batches all transactions together for concurrent execution)
	execResults, ctx := e.helper.ProcessTXsWithOCC(ctx, blockWithCtx.Txs, typedTxs, absoluteTxIndices)

	// Extract EVM messages for SetMsgs (needed for GetAllEVMTxDeferredInfo in EndBlock)
	evmMsgs := make([]*evmtypes.MsgEVMTransaction, len(typedTxs))
	for i, tx := range typedTxs {
		evmMsgs[i] = e.getEVMMessage(tx)
	}

	// Set txResults and msgs on keeper (needed for GetAllEVMTxDeferredInfo in EndBlock)
	// This is critical for EndBlock to process surplus correctly
	e.helper.GetKeeper().SetTxResults(execResults)
	e.helper.GetKeeper().SetMsgs(evmMsgs)

	// Convert ExecTxResult to TransactionResult
	txResults := make([]*pipelinetypes.TransactionResult, 0, len(execResults))
	for _, execResult := range execResults {
		// Extract surplus from EVM transactions (it's stored in deferred info during execution)
		surplus := sdk.ZeroInt()
		if execResult.EvmTxInfo != nil {
			// Surplus is already handled during execution via AppendToEvmTxDeferredInfo
			// We don't need to extract it here since it's already stored
		}

		// Extract logs from events (for EVM transactions)
		var logs []*types.Log
		vmError := ""
		if execResult.EvmTxInfo != nil {
			vmError = execResult.EvmTxInfo.VmError
			// Logs are extracted from events by the EVM module
			// They're already included in the receipt written during execution
		}

		txResult := &pipelinetypes.TransactionResult{
			GasUsed:    execResult.GasUsed,
			ReturnData: execResult.Data,
			Logs:       logs,
			VmError:    vmError,
			Events:     execResult.Events,
			Code:       execResult.Code,
			Codespace:  execResult.Codespace,
			Log:        execResult.Log,
			Surplus:    surplus,
		}
		txResults = append(txResults, txResult)
		events = append(events, execResult.Events...)
	}

	// Calculate total EVM gas used for EndBlock
	evmTotalGasUsed := int64(0)
	for _, execResult := range execResults {
		if execResult.EvmTxInfo != nil {
			evmTotalGasUsed += execResult.GasUsed
		}
	}

	// MidBlock
	midBlockEvents := e.midBlock(ctx, preprocessed.Height)
	events = append(events, midBlockEvents...)

	// EndBlock
	endBlockResp := e.endBlock(ctx, abci.RequestEndBlock{
		Height:       preprocessed.Height,
		BlockGasUsed: evmTotalGasUsed,
	})
	events = append(events, endBlockResp.Events...)

	// Note: WriteState() and GetWorkingHash() are NOT called here because
	// they need to be called on the main thread after SetDeliverStateToCommit()
	// The AppHash will be set to nil here and filled in FinalizeBlocker
	appHash := []byte(nil)

	return &pipelinetypes.ExecutedBlock{
		PreprocessedBlock: preprocessed,
		TxResults:         txResults,
		Events:            events,
		AppHash:           appHash,
		EndBlockResp:      endBlockResp,
		Ctx:               ctx,
	}, nil
}

// Stop gracefully stops the component
func (e *ExecutorComponent) Stop() {
	e.mu.Lock()
	if !e.started {
		e.mu.Unlock()
		return
	}
	e.mu.Unlock()

	if e.cancel != nil {
		e.cancel()
	}
	e.wg.Wait()

	e.mu.Lock()
	e.started = false
	e.mu.Unlock()
}

// IsRunning returns true if the component is currently running
func (e *ExecutorComponent) IsRunning() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.started
}

// getEVMMessage extracts EVM message from a transaction, similar to App.GetEVMMsg
func (e *ExecutorComponent) getEVMMessage(tx sdk.Tx) *evmtypes.MsgEVMTransaction {
	defer func() {
		if err := recover(); err != nil {
			// Return nil on panic
		}
	}()
	if tx == nil {
		return nil
	} else if emsg := evmtypes.GetEVMTransactionMessage(tx); emsg != nil && !emsg.IsAssociateTx() {
		return emsg
	}
	return nil
}

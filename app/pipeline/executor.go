package pipeline

import (
	"context"
	"sync"

	sdk "github.com/cosmos/cosmos-sdk/types"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/utils"

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
	helper pipelinetypes.ExecutionHelper
	beginBlock func(ctx sdk.Context, req abci.RequestBeginBlock) abci.ResponseBeginBlock
	midBlock   func(ctx sdk.Context, height int64) []abci.Event
	endBlock   func(ctx sdk.Context, req abci.RequestEndBlock) abci.ResponseEndBlock
	writeState func()
	getWorkingHash func() []byte
	executeEVMTransaction func(ctx sdk.Context, preprocessed *pipelinetypes.PreprocessedTx, helper pipelinetypes.ExecutionHelper) (*pipelinetypes.TransactionResult, error)
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
		in:                      in,
		out:                     out,
		helper:                  helper,
		beginBlock:              beginBlock,
		midBlock:                midBlock,
		endBlock:                endBlock,
		writeState:              writeState,
		getWorkingHash:           getWorkingHash,
		executeEVMTransaction:   executeEVMTransaction,
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
					// Log error and continue - component should handle errors
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
	
	// BeginBlock
	beginBlockReq := abci.RequestBeginBlock{
		Hash:              preprocessed.Hash,
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

	// Execute transactions
	txResults := make([]*pipelinetypes.TransactionResult, 0, len(preprocessed.PreprocessedTxs))
	for i, preprocessedTx := range preprocessed.PreprocessedTxs {
		var result *pipelinetypes.TransactionResult
		var err error

		if preprocessedTx.Type == pipelinetypes.TransactionTypeEVM {
			result, err = e.executeEVMTransaction(ctx, preprocessedTx, e.helper)
		} else {
			// COSMOS transaction
			result, err = e.executeCosmosTransaction(ctx, preprocessedTx.CosmosTx, blockWithCtx.Txs[i], e.helper)
		}

		if err != nil {
			ctx.Logger().Error("transaction execution failed", "error", err, "txIndex", i)
			// Create error result
			result = &pipelinetypes.TransactionResult{
				Code:      uint32(1),
				Codespace: "app",
				Log:       err.Error(),
			}
		}

		txResults = append(txResults, result)
		events = append(events, result.Events...)
	}

	// MidBlock
	midBlockEvents := e.midBlock(ctx, preprocessed.Height)
	events = append(events, midBlockEvents...)

	// EndBlock
	endBlockResp := e.endBlock(ctx, abci.RequestEndBlock{
		Height:       preprocessed.Height,
		BlockGasUsed: 0, // TODO: Calculate from txResults
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


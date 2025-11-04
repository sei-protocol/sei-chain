package app

import (
	"context"
	"sync"

	sdk "github.com/cosmos/cosmos-sdk/types"
	abci "github.com/tendermint/tendermint/abci/types"

	"github.com/sei-protocol/sei-chain/app/pipeline"
	pipelinetypes "github.com/sei-protocol/sei-chain/app/pipeline/types"
)

// BlockPipeline orchestrates all pipeline components
// Moved to app package to avoid import cycles
type BlockPipeline struct {
	mu sync.RWMutex

	// Components
	preprocessor *pipeline.PreprocessorComponent
	ordering     *pipeline.OrderingComponent
	executor     *pipeline.ExecutorComponent
	finalizer    *pipeline.FinalizerComponent

	// Channels
	input  chan *pipelinetypes.BlockRequest
	output chan *pipelinetypes.ProcessedBlock

	// Internal channels between components
	preprocessedOut chan *pipelinetypes.PreprocessedBlock
	orderedOut      chan *pipelinetypes.PreprocessedBlock
	executorIn      chan *pipelinetypes.PreprocessedBlockWithContext
	executedOut     chan *pipelinetypes.ExecutedBlock

	// Configuration
	preprocessorWorkers int
	bufferSize          int
	startSequence       int64

	// State
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewBlockPipeline creates a new BlockPipeline
func NewBlockPipeline(
	ctx context.Context,
	preprocessorHelper pipelinetypes.PreprocessorHelper,
	executionHelper pipelinetypes.ExecutionHelper,
	finalizerHelper pipelinetypes.FinalizerHelper,
	txDecoder func([]byte) (sdk.Tx, error),
	beginBlock func(ctx sdk.Context, req abci.RequestBeginBlock) abci.ResponseBeginBlock,
	midBlock func(ctx sdk.Context, height int64) []abci.Event,
	endBlock func(ctx sdk.Context, req abci.RequestEndBlock) abci.ResponseEndBlock,
	writeState func(),
	getWorkingHash func() []byte,
	preprocessBlock func(ctx sdk.Context, req pipelinetypes.BlockProcessRequest, lastCommit abci.LastCommitInfo, txs [][]byte, txDecoder func([]byte) (sdk.Tx, error), helper pipelinetypes.PreprocessorHelper) (*pipelinetypes.PreprocessedBlock, error),
	executeEVMTransaction func(ctx sdk.Context, preprocessed *pipelinetypes.PreprocessedTx, helper pipelinetypes.ExecutionHelper) (*pipelinetypes.TransactionResult, error),
	executeCosmosTransaction func(ctx sdk.Context, tx sdk.Tx, txBytes []byte, helper pipelinetypes.ExecutionHelper) (*pipelinetypes.TransactionResult, error),
	preprocessorWorkers int,
	bufferSize int,
	startSequence int64,
) *BlockPipeline {
	if preprocessorWorkers < 1 {
		preprocessorWorkers = 10
	}
	if bufferSize < 1 {
		bufferSize = 1000
	}
	if startSequence < 1 {
		startSequence = 1
	}

	// Create channels
	input := make(chan *pipelinetypes.BlockRequest, bufferSize)
	preprocessedOut := make(chan *pipelinetypes.PreprocessedBlock, bufferSize)
	orderedOut := make(chan *pipelinetypes.PreprocessedBlock, bufferSize)
	executorIn := make(chan *pipelinetypes.PreprocessedBlockWithContext, bufferSize)
	executedOut := make(chan *pipelinetypes.ExecutedBlock, bufferSize)
	output := make(chan *pipelinetypes.ProcessedBlock, bufferSize)

	// Create components
	preprocessor := pipeline.NewPreprocessorComponent(input, preprocessedOut, preprocessorWorkers, txDecoder, preprocessorHelper, preprocessBlock)
	ordering := pipeline.NewOrderingComponent(preprocessedOut, orderedOut, bufferSize, startSequence)
	executor := pipeline.NewExecutorComponent(executorIn, executedOut, executionHelper, beginBlock, midBlock, endBlock, writeState, getWorkingHash, executeEVMTransaction, executeCosmosTransaction)
	finalizer := pipeline.NewFinalizerComponent(executedOut, output, finalizerHelper)

	return &BlockPipeline{
		preprocessor:        preprocessor,
		ordering:            ordering,
		executor:            executor,
		finalizer:           finalizer,
		input:               input,
		output:              output,
		preprocessedOut:     preprocessedOut,
		orderedOut:          orderedOut,
		executorIn:          executorIn,
		executedOut:         executedOut,
		preprocessorWorkers: preprocessorWorkers,
		bufferSize:          bufferSize,
		startSequence:       startSequence,
	}
}

// Start starts all pipeline components
func (bp *BlockPipeline) Start(ctx context.Context) {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	if bp.started {
		return
	}

	bp.ctx, bp.cancel = context.WithCancel(ctx)
	bp.started = true

	// Start adapter goroutine to convert PreprocessedBlock to PreprocessedBlockWithContext
	bp.wg.Add(1)
	go func() {
		defer bp.wg.Done()
		for {
			select {
			case <-bp.ctx.Done():
				close(bp.executorIn)
				return
			case block, ok := <-bp.orderedOut:
				if !ok {
					close(bp.executorIn)
					return
				}
				// Convert PreprocessedBlock to PreprocessedBlockWithContext
				blockWithCtx := &pipelinetypes.PreprocessedBlockWithContext{
					Block: block,
					Ctx:   block.Ctx,
					Txs:   block.Txs,
				}
				select {
				case <-bp.ctx.Done():
					return
				case bp.executorIn <- blockWithCtx:
				}
			}
		}
	}()

	// Start all components
	bp.preprocessor.Start(bp.ctx)
	bp.ordering.Start(bp.ctx)
	bp.executor.Start(bp.ctx)
	bp.finalizer.Start(bp.ctx)
}

// Stop stops all pipeline components
func (bp *BlockPipeline) Stop() {
	bp.mu.Lock()
	if !bp.started {
		bp.mu.Unlock()
		return
	}
	bp.mu.Unlock()

	if bp.cancel != nil {
		bp.cancel()
	}

	// Stop components in reverse order
	bp.finalizer.Stop()
	bp.executor.Stop()
	bp.ordering.Stop()
	bp.preprocessor.Stop()

	// Close channels
	close(bp.input)
	close(bp.output)

	bp.wg.Wait()

	bp.mu.Lock()
	bp.started = false
	bp.mu.Unlock()
}

// Input returns the input channel for blocks
func (bp *BlockPipeline) Input() chan<- *pipelinetypes.BlockRequest {
	return bp.input
}

// Output returns the output channel for processed blocks
func (bp *BlockPipeline) Output() <-chan *pipelinetypes.ProcessedBlock {
	return bp.output
}

// IsRunning returns true if the pipeline is currently running
func (bp *BlockPipeline) IsRunning() bool {
	bp.mu.RLock()
	defer bp.mu.RUnlock()
	return bp.started
}

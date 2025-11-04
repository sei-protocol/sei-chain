package pipeline

import (
	"context"
	"sync"

	sdk "github.com/cosmos/cosmos-sdk/types"
	abci "github.com/tendermint/tendermint/abci/types"

	pipelinetypes "github.com/sei-protocol/sei-chain/app/pipeline/types"
)

// PreprocessorComponent is a long-running component that preprocesses blocks in parallel
type PreprocessorComponent struct {
	mu      sync.RWMutex
	in      <-chan *pipelinetypes.BlockRequest
	out     chan<- *pipelinetypes.PreprocessedBlock
	workers int
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	// Dependencies
	txDecoder       func([]byte) (sdk.Tx, error)
	helper          pipelinetypes.PreprocessorHelper
	preprocessBlock func(ctx sdk.Context, req pipelinetypes.BlockProcessRequest, lastCommit abci.LastCommitInfo, txs [][]byte, txDecoder func([]byte) (sdk.Tx, error), helper pipelinetypes.PreprocessorHelper) (*pipelinetypes.PreprocessedBlock, error)
}

// NewPreprocessorComponent creates a new PreprocessorComponent
func NewPreprocessorComponent(
	in <-chan *pipelinetypes.BlockRequest,
	out chan<- *pipelinetypes.PreprocessedBlock,
	workers int,
	txDecoder func([]byte) (sdk.Tx, error),
	helper pipelinetypes.PreprocessorHelper,
	preprocessBlock func(ctx sdk.Context, req pipelinetypes.BlockProcessRequest, lastCommit abci.LastCommitInfo, txs [][]byte, txDecoder func([]byte) (sdk.Tx, error), helper pipelinetypes.PreprocessorHelper) (*pipelinetypes.PreprocessedBlock, error),
) *PreprocessorComponent {
	if workers < 1 {
		workers = 1
	}
	return &PreprocessorComponent{
		in:              in,
		out:             out,
		workers:         workers,
		txDecoder:       txDecoder,
		helper:          helper,
		preprocessBlock: preprocessBlock,
	}
}

// Start begins processing blocks
func (p *PreprocessorComponent) Start(ctx context.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.started {
		return
	}

	p.ctx, p.cancel = context.WithCancel(ctx)
	p.started = true
	p.wg.Add(p.workers)

	// Start worker goroutines
	for i := 0; i < p.workers; i++ {
		go func() {
			defer p.wg.Done()
			for {
				select {
				case <-p.ctx.Done():
					return
				case blockReq, ok := <-p.in:
					if !ok {
						return
					}
					preprocessed, err := p.preprocessBlock(
						blockReq.Ctx,
						blockReq.Req,
						abci.LastCommitInfo(blockReq.LastCommit),
						blockReq.Txs,
						p.txDecoder,
						p.helper,
					)
					if err != nil {
						// Log error and continue - component should handle errors
						blockReq.Ctx.Logger().Error("preprocessing failed", "error", err)
						continue
					}
					select {
					case <-p.ctx.Done():
						return
					case p.out <- preprocessed:
					}
				}
			}
		}()
	}
}

// Stop gracefully stops the component
func (p *PreprocessorComponent) Stop() {
	p.mu.Lock()
	if !p.started {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()

	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()

	p.mu.Lock()
	p.started = false
	p.mu.Unlock()
}

// IsRunning returns true if the component is currently running
func (p *PreprocessorComponent) IsRunning() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.started
}

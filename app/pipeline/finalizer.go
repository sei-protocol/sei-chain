package pipeline

import (
	"context"
	"sync"

	pipelinetypes "github.com/sei-protocol/sei-chain/app/pipeline/types"
)

// FinalizerComponent is a long-running component that handles non-critical post-processing
type FinalizerComponent struct {
	mu      sync.RWMutex
	in      <-chan *pipelinetypes.ExecutedBlock
	out     chan<- *pipelinetypes.ProcessedBlock
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	// Dependencies
	helper pipelinetypes.FinalizerHelper
}

// NewFinalizerComponent creates a new FinalizerComponent
func NewFinalizerComponent(
	in <-chan *pipelinetypes.ExecutedBlock,
	out chan<- *pipelinetypes.ProcessedBlock,
	helper pipelinetypes.FinalizerHelper,
) *FinalizerComponent {
	return &FinalizerComponent{
		in:     in,
		out:    out,
		helper: helper,
	}
}

// Start begins finalizing blocks
func (f *FinalizerComponent) Start(ctx context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.started {
		return
	}

	f.ctx, f.cancel = context.WithCancel(ctx)
	f.started = true
	f.wg.Add(1)

	go func() {
		defer f.wg.Done()
		for {
			select {
			case <-f.ctx.Done():
				return
			case executed, ok := <-f.in:
				if !ok {
					return
				}
				processed, err := f.finalizeBlock(executed)
				if err != nil {
					// Log error and continue - component should handle errors
					// Note: we don't have direct access to logger here, so we'll need to handle this differently
					// For now, we'll just continue
					continue
				}
				select {
				case <-f.ctx.Done():
					return
				case f.out <- processed:
				}
			}
		}
	}()
}

// finalizeBlock performs non-critical post-processing on an executed block
func (f *FinalizerComponent) finalizeBlock(executed *pipelinetypes.ExecutedBlock) (*pipelinetypes.ProcessedBlock, error) {
	ctx := executed.Ctx
	preprocessed := executed.PreprocessedBlock

	// If preprocessing failed, skip processing and just forward the error block
	if preprocessed.PreprocessError != nil {
		return &pipelinetypes.ProcessedBlock{
			ExecutedBlock: executed,
		}, nil
	}

	// Process each transaction result for gas meter updates
	for i, txResult := range executed.TxResults {
		if i >= len(preprocessed.PreprocessedTxs) {
			continue
		}

		preprocessedTx := preprocessed.PreprocessedTxs[i]

		// Only process EVM transactions for gas meter updates
		if preprocessedTx.Type == pipelinetypes.TransactionTypeEVM {
			// Update gas meter (convert EVM gas to Sei gas)
			normalizer := f.helper.GetPriorityNormalizer(ctx)
			adjustedGasUsed := normalizer.MulInt64(txResult.GasUsed).TruncateInt().Uint64()
			ctx.GasMeter().ConsumeGas(adjustedGasUsed, "evm transaction")
		}
	}

	// Flush transient receipts (happens at block level)
	if err := f.helper.FlushTransientReceipts(ctx); err != nil {
		ctx.Logger().Error("failed to flush transient receipts", "error", err)
		// Continue anyway - this is non-critical
	}

	// Create processed block
	return &pipelinetypes.ProcessedBlock{
		ExecutedBlock: executed,
	}, nil
}

// Stop gracefully stops the component
func (f *FinalizerComponent) Stop() {
	f.mu.Lock()
	if !f.started {
		f.mu.Unlock()
		return
	}
	f.mu.Unlock()

	if f.cancel != nil {
		f.cancel()
	}
	f.wg.Wait()

	f.mu.Lock()
	f.started = false
	f.mu.Unlock()
}

// IsRunning returns true if the component is currently running
func (f *FinalizerComponent) IsRunning() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.started
}

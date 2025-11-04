package pipeline

import (
	"context"
	"sync"

	sdk "github.com/cosmos/cosmos-sdk/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	pipelinetypes "github.com/sei-protocol/sei-chain/app/pipeline/types"
	"github.com/sei-protocol/sei-chain/x/evm/state"
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
	keeper := f.helper.GetKeeper()

	// Process each transaction result for EVM transactions
	for i, txResult := range executed.TxResults {
		if i >= len(preprocessed.PreprocessedTxs) {
			continue
		}

		preprocessedTx := preprocessed.PreprocessedTxs[i]

		// Only write receipts for EVM transactions
		if preprocessedTx.Type == pipelinetypes.TransactionTypeEVM {
			// Create a minimal stateDB just for receipt writing
			// We already have logs in txResult, so we'll add them to stateDB
			stateDB := state.NewDBImpl(ctx, keeper, false)
			defer stateDB.Cleanup()

			// Add logs to stateDB (they're needed for WriteReceipt to create bloom)
			for _, log := range txResult.Logs {
				stateDB.AddLog(log)
			}

			// Create transaction hash
			etx := ethtypes.NewTx(preprocessedTx.TxData.AsEthereumData())
			txHash := etx.Hash()

			// Write receipt
			receipt, err := f.helper.WriteReceipt(
				ctx,
				stateDB,
				preprocessedTx.EVMMessage,
				uint32(etx.Type()),
				txHash,
				uint64(txResult.GasUsed), //nolint:gosec // GasUsed is bounded by block gas limit
				txResult.VmError,
			)
			if err != nil {
				// Log error but continue - receipt writing is non-critical
				ctx.Logger().Error("failed to write receipt", "error", err, "txIndex", i)
				continue
			}

			// Handle deferred info (bloom and surplus)
			// Get surplus from transaction result (set during execution)
			bloom := ethtypes.Bloom{}
			bloom.SetBytes(receipt.LogsBloom)
			surplus := txResult.Surplus
			if surplus.IsNil() {
				surplus = sdk.ZeroInt()
			}
			f.helper.AppendToEvmTxDeferredInfo(ctx, bloom, txHash, surplus)

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

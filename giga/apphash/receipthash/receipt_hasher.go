package receipthash

import (
	"context"
	"errors"
	"fmt"
	"sync"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/seilog"

	"github.com/sei-protocol/sei-chain/giga/apphash"
)

var logger = seilog.NewLogger("giga", "apphash", "receipthash")

// ReceiptHasher computes the receipt hash of each block submitted to it on a background goroutine, and reports the
// hashes in submission order.
type ReceiptHasher struct {

	// Called with each block's receipt hash. An error stops the hasher.
	report func(ctx context.Context, blockHeight uint64, receiptHash apphash.ReceiptHash) error

	// The blocks waiting to be hashed, in submission order, followed by the request Close() sends.
	requests chan receiptHashRequest

	// Cancelled by Close(), or by the first block that fails. Its cause is the error every later call returns.
	ctx context.Context

	// Cancels ctx with a cause.
	cancel context.CancelCauseFunc

	// Tracks the hashing goroutine, which Close() waits for.
	wg sync.WaitGroup

	// The error from the block that stopped the hashing goroutine, or nil if none did. Set by the hashing goroutine
	// as it exits, and read by Close() once it has exited.
	failure error
}

// A request for the hashing goroutine: a block to hash, or the request Close() sends.
type receiptHashRequest struct {

	// The height of the block whose receipts these are.
	blockHeight uint64

	// The block's receipts, as passed to ComputeReceiptHash().
	receipts []*ethtypes.Receipt

	// Set only on the request Close() sends, and closed once every block submitted before it has been reported.
	drained chan struct{}
}

// NewReceiptHasher starts a hasher that passes each block's receipt hash to report. Errors if config is invalid.
func NewReceiptHasher(
	// Cancelling it stops the hasher.
	ctx context.Context,
	// Configures the hasher.
	config ReceiptHasherConfig,
	// Called with each block's receipt hash, in submission order. An error stops the hasher. Its signature matches
	// AppHashBuilder.ReportReceiptHash().
	report func(ctx context.Context, blockHeight uint64, receiptHash apphash.ReceiptHash) error,
) (*ReceiptHasher, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid receipt hasher config: %w", err)
	}
	ctx, cancel := context.WithCancelCause(ctx)
	h := &ReceiptHasher{
		report:   report,
		requests: make(chan receiptHashRequest, config.BufferSize),
		ctx:      ctx,
		cancel:   cancel,
	}
	h.wg.Go(h.run)
	return h, nil
}

// Submit queues a block's receipts to be hashed, blocking while the queue is full. Receipts that
// ComputeReceiptHash() rejects stop the hasher once they are hashed rather than failing this call. Errors if the
// hasher has stopped, or if ctx is done first.
func (h *ReceiptHasher) Submit(
	ctx context.Context,
	// The height of the block whose receipts these are.
	blockHeight uint64,
	// The block's receipts, as passed to ComputeReceiptHash(). Must not be mutated after the call.
	receipts []*ethtypes.Receipt,
) error {
	request := receiptHashRequest{blockHeight: blockHeight, receipts: receipts}
	if err := h.enqueue(ctx, request); err != nil {
		return fmt.Errorf("failed to submit the receipts of block %d: %w", blockHeight, err)
	}
	return nil
}

// Close reports the receipt hash of every block submitted before the call, then stops the hasher and returns once
// its goroutine has exited. A block submitted concurrently with the call may or may not be reported, as may every
// block still queued if the hasher's context is cancelled. Errors if a block failed to be hashed or reported.
func (h *ReceiptHasher) Close() error {
	drained := make(chan struct{})
	// A hasher already stopped refuses the request, and has nothing left to report.
	if h.enqueue(context.Background(), receiptHashRequest{drained: drained}) == nil {
		select {
		case <-drained:
		case <-h.ctx.Done():
		}
	}
	h.cancel(errors.New("receipt hasher is closed"))
	h.wg.Wait()
	if h.failure != nil {
		return fmt.Errorf("failed to close receipt hasher: %w", h.failure)
	}
	return nil
}

// Hands request to the hashing goroutine.
func (h *ReceiptHasher) enqueue(ctx context.Context, request receiptHashRequest) error {
	// A stopped hasher must refuse the request rather than race it into the buffer, where nothing would run it.
	if h.ctx.Err() != nil {
		return h.stoppedError()
	}
	select {
	case h.requests <- request:
		return nil
	case <-h.ctx.Done():
		return h.stoppedError()
	case <-ctx.Done():
		return fmt.Errorf("receipt hasher call abandoned: %w", ctx.Err())
	}
}

// Returns the error every call returns once the hasher is stopped.
func (h *ReceiptHasher) stoppedError() error {
	return fmt.Errorf("receipt hasher stopped: %w", context.Cause(h.ctx))
}

// The hashing goroutine. Hashes and reports blocks until the request Close() sends, a cancelled context, or a block
// that fails.
func (h *ReceiptHasher) run() {
	for {
		select {
		case <-h.ctx.Done():
			return
		case request := <-h.requests:
			if request.drained != nil {
				close(request.drained)
				return
			}
			if err := h.hashAndReport(request); err != nil {
				logger.Error("receipt hasher stopped", "err", err)
				h.failure = err
				h.cancel(err)
				return
			}
		}
	}
}

// Computes the receipt hash of one block and reports it.
func (h *ReceiptHasher) hashAndReport(request receiptHashRequest) error {
	receiptHash, err := ComputeReceiptHash(request.receipts)
	if err != nil {
		return fmt.Errorf("failed to hash the receipts of block %d: %w", request.blockHeight, err)
	}
	if err := h.report(h.ctx, request.blockHeight, receiptHash); err != nil {
		return fmt.Errorf("failed to report the receipt hash of block %d: %w", request.blockHeight, err)
	}
	return nil
}

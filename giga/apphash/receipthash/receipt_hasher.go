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

	// The messages waiting to be handled, in submission order: each a receiptHashRequest, receiptFlushRequest or
	// receiptCloseRequest.
	requests chan any

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

// A block to hash.
type receiptHashRequest struct {

	// The height of the block whose receipts these are.
	blockHeight uint64

	// The block's receipts, as passed to ComputeReceiptHash().
	receipts []*ethtypes.Receipt
}

// Sent by Flush().
type receiptFlushRequest struct {
	// Closed once every block submitted before the request has been reported.
	flushed chan struct{}
}

// Sent by Close(). The hashing goroutine exits once it has handled it.
type receiptCloseRequest struct {
	// Closed once every block submitted before the request has been reported.
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
		requests: make(chan any, config.BufferSize),
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

// Flush returns once the receipt hash of every block submitted before the call has been reported.
func (h *ReceiptHasher) Flush(ctx context.Context) error {
	flushed := make(chan struct{})
	if err := h.enqueue(ctx, receiptFlushRequest{flushed: flushed}); err != nil {
		return fmt.Errorf("failed to flush the receipt hasher: %w", err)
	}
	select {
	case <-flushed:
		return nil
	case <-h.ctx.Done():
		return h.stoppedError()
	case <-ctx.Done():
		return fmt.Errorf("receipt hasher flush abandoned: %w", ctx.Err())
	}
}

// Close reports the receipt hash of every block submitted before the call, then stops the hasher and returns once
// its goroutine has exited. A block submitted concurrently with the call may or may not be reported, as may every
// block still queued if the hasher's context is cancelled. Errors if a block failed to be hashed or reported.
func (h *ReceiptHasher) Close() error {
	drained := make(chan struct{})
	// A hasher already stopped refuses the request, and has nothing left to report.
	if h.enqueue(context.Background(), receiptCloseRequest{drained: drained}) == nil {
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
func (h *ReceiptHasher) enqueue(ctx context.Context, request any) error {
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

// The hashing goroutine. Hashes and reports blocks until a receiptCloseRequest, a cancelled context, or a block that
// fails.
func (h *ReceiptHasher) run() {
	for {
		var request any
		select {
		case <-h.ctx.Done():
			return
		case request = <-h.requests:
		}
		switch r := request.(type) {
		case receiptHashRequest:
			if err := h.hashAndReport(r); err != nil {
				h.fail(err)
				return
			}
		case receiptFlushRequest:
			close(r.flushed)
		case receiptCloseRequest:
			close(r.drained)
			return
		default:
			h.fail(fmt.Errorf("unknown receipt hasher request %T", request))
			return
		}
	}
}

// Stops the hasher with err, which Close() and every later call report.
func (h *ReceiptHasher) fail(err error) {
	logger.Error("receipt hasher stopped", "err", err)
	h.failure = err
	h.cancel(err)
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

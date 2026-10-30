package bud

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/sei-protocol/seilog"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

var logger = seilog.NewLogger("giga", "apphash", "bud")

// budTombstoneHorizon is the tombstone horizon, as the BUD spec defines it, of every block: the not-modified-since
// height of a budlet whose key had no entry in the state before its block.
//
// FlatKV MUST start storing tombstones before Autobahn. The true tombstone horizon is the first block at which FlatKV
// stores tombstones, but it is simpler to set it to the giga upgrade height. It must become that global chain
// parameter before the mainnet app hash uses the BUD hash.
const budTombstoneHorizon uint64 = 0

var _ BUDHasher = (*StandardBUDHasher)(nil)

// StandardBUDHasher is a BUDHasher that sorts each block on one goroutine and hashes it on a second, so the two
// stages run concurrently on consecutive blocks.
type StandardBUDHasher struct {
	// Called with each block's BUD hash. An error stops the hasher.
	report func(ctx context.Context, blockHeight uint64, budHash [32]byte) error

	// The blocks waiting to be sorted, in submission order, followed by the request Close() sends.
	sortRequests chan budHashRequest

	// The sorted blocks waiting to be hashed, in submission order, followed by the request Close() sends.
	hashRequests chan budHashRequest

	// Cancelled by Close(), or by the first block that fails. Its cause is the error every later call returns.
	ctx context.Context

	// Cancels ctx with a cause.
	cancel context.CancelCauseFunc

	// Tracks the sorting and hashing goroutines, which Close() waits for.
	wg sync.WaitGroup

	// The error from the block that stopped the hashing goroutine, or nil if none did. Set by the hashing goroutine
	// as it exits, and read by Close() once it has exited.
	failure error
}

// A request for the sorting and hashing goroutines: a block to hash, or the request Close() sends.
type budHashRequest struct {
	// The height of the block the mutations belong to.
	blockHeight uint64

	// The block's mutations: as submitted on the way to the sorting goroutine, and sorted by key on the way to the
	// hashing goroutine.
	mutations []mutation.Mutation

	// Set only on the request Close() sends, and closed once every block submitted before it has been reported.
	drained chan struct{}
}

// NewStandardBUDHasher starts a hasher that passes each block's BUD hash to report. Errors if config is invalid.
func NewStandardBUDHasher(
	// Cancelling it stops the hasher.
	ctx context.Context,
	// Configures the hasher.
	config BUDHasherConfig,
	// Called with each block's BUD hash, in submission order. An error stops the hasher. Its signature matches
	// AppHashBuilder.ReportBUDHash().
	report func(ctx context.Context, blockHeight uint64, budHash [32]byte) error,
) (*StandardBUDHasher, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid BUD hasher config: %w", err)
	}
	ctx, cancel := context.WithCancelCause(ctx)
	h := &StandardBUDHasher{
		report:       report,
		sortRequests: make(chan budHashRequest, config.BufferSize),
		hashRequests: make(chan budHashRequest, config.BufferSize),
		ctx:          ctx,
		cancel:       cancel,
	}
	h.wg.Go(h.sortBlocks)
	h.wg.Go(h.hashBlocks)
	return h, nil
}

func (h *StandardBUDHasher) Submit(ctx context.Context, blockHeight uint64, mutations []mutation.Mutation) error {
	request := budHashRequest{blockHeight: blockHeight, mutations: mutations}
	if err := h.enqueue(ctx, request); err != nil {
		return fmt.Errorf("failed to submit the mutations of block %d: %w", blockHeight, err)
	}
	return nil
}

func (h *StandardBUDHasher) Close() error {
	drained := make(chan struct{})
	// A hasher already stopped refuses the request, and has nothing left to report.
	if h.enqueue(context.Background(), budHashRequest{drained: drained}) == nil {
		select {
		case <-drained:
		case <-h.ctx.Done():
		}
	}
	h.cancel(errors.New("BUD hasher is closed"))
	h.wg.Wait()
	if h.failure != nil {
		return fmt.Errorf("failed to close BUD hasher: %w", h.failure)
	}
	return nil
}

// Hands request to the sorting goroutine.
func (h *StandardBUDHasher) enqueue(ctx context.Context, request budHashRequest) error {
	// A stopped hasher must refuse the request rather than race it into the buffer, where nothing would run it.
	if h.ctx.Err() != nil {
		return h.stoppedError()
	}
	select {
	case h.sortRequests <- request:
		return nil
	case <-h.ctx.Done():
		return h.stoppedError()
	case <-ctx.Done():
		return fmt.Errorf("BUD hasher call abandoned: %w", ctx.Err())
	}
}

// Returns the error every call returns once the hasher is stopped.
func (h *StandardBUDHasher) stoppedError() error {
	return fmt.Errorf("BUD hasher stopped: %w", context.Cause(h.ctx))
}

// The sorting goroutine. Sorts each block and hands it to the hashing goroutine, until it has handed over the
// request Close() sends, or the context is cancelled.
func (h *StandardBUDHasher) sortBlocks() {
	for {
		var request budHashRequest
		select {
		case <-h.ctx.Done():
			return
		case request = <-h.sortRequests:
		}
		if request.drained == nil {
			request.mutations = sortedByKey(request.mutations)
		}
		select {
		case <-h.ctx.Done():
			return
		case h.hashRequests <- request:
		}
		if request.drained != nil {
			return
		}
	}
}

// The hashing goroutine. Hashes and reports sorted blocks until the request Close() sends, a cancelled context, or a
// block that fails.
func (h *StandardBUDHasher) hashBlocks() {
	for {
		select {
		case <-h.ctx.Done():
			return
		case request := <-h.hashRequests:
			if request.drained != nil {
				close(request.drained)
				return
			}
			if err := h.hashAndReport(request); err != nil {
				logger.Error("BUD hasher stopped", "err", err)
				h.failure = err
				h.cancel(err)
				return
			}
		}
	}
}

// Computes the BUD hash of one sorted block and reports it.
func (h *StandardBUDHasher) hashAndReport(request budHashRequest) error {
	budHash, err := budHashFromMutations(request.mutations)
	if err != nil {
		return fmt.Errorf("failed to hash block %d: %w", request.blockHeight, err)
	}
	if err := h.report(h.ctx, request.blockHeight, [32]byte(budHash)); err != nil {
		return fmt.Errorf("failed to report the BUD hash of block %d: %w", request.blockHeight, err)
	}
	return nil
}

// sortedByKey returns a copy of mutations sorted by key, leaving mutations in its original order.
func sortedByKey(mutations []mutation.Mutation) []mutation.Mutation {
	sorted := slices.Clone(mutations)
	slices.SortFunc(sorted, func(a mutation.Mutation, b mutation.Mutation) int {
		return bytes.Compare(a.Key, b.Key)
	})
	return sorted
}

// budHashFromMutations returns the BUD hash of one block's mutations. Each mutation must meet the requirements of
// budletFromMutation().
func budHashFromMutations(
	// The block's mutations, strictly sorted by key.
	mutations []mutation.Mutation,
) (apphash.BUDHash, error) {
	budlets := make([]*Budlet, len(mutations))
	for i := range mutations {
		budlet, err := budletFromMutation(&mutations[i])
		if err != nil {
			return apphash.BUDHash{}, fmt.Errorf("failed to convert mutation %d: %w", i, err)
		}
		budlets[i] = budlet
	}
	treeHash, err := ComputeBUDTreeHash(budlets)
	if err != nil {
		return apphash.BUDHash{}, fmt.Errorf("failed to compute BUD tree hash: %w", err)
	}
	return ComputeBUDHash(uint64(len(budlets)), treeHash), nil
}

// budletFromMutation returns the budlet for one key a block modified.
//
// The mutation's key must not be empty, and each of its values must be nil or a serialized FlatKV VType. A nil value
// converts to the absent payload, the empty byte string, which budPayload() never returns.
func budletFromMutation(
	// The mutation to convert. The budlet retains its key.
	m *mutation.Mutation,
) (*Budlet, error) {
	var value []byte
	if m.NewValue != nil {
		var err error
		value, _, err = budPayload(m.NewValue)
		if err != nil {
			return nil, fmt.Errorf("invalid new value of key %x: %w", m.Key, err)
		}
	}

	var previousValue []byte
	notModifiedSince := budTombstoneHorizon
	if m.PreviousValue != nil {
		var err error
		previousValue, notModifiedSince, err = budPayload(m.PreviousValue)
		if err != nil {
			return nil, fmt.Errorf("invalid previous value of key %x: %w", m.Key, err)
		}
	}

	budlet, err := NewBudlet(m.Key, value, previousValue, notModifiedSince)
	if err != nil {
		return nil, fmt.Errorf("invalid mutation of key %x: %w", m.Key, err)
	}
	return budlet, nil
}

// budPayload returns the BUD payload of a serialized FlatKV VType, its serialization version followed by its data,
// and the height of the block that wrote it.
func budPayload(
	// The serialized VType.
	value []byte,
) ([]byte, uint64, error) {
	version, blockHeight, data, err := vtype.SplitValue(value)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to split value: %w", err)
	}
	payload := make([]byte, 0, 1+len(data))
	payload = append(payload, version)
	payload = append(payload, data...)
	return payload, blockHeight, nil
}

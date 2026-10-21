package statewal

import (
	"context"
	"fmt"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
)

// stateWALHasher computes the hash of each block written to a state WAL and delivers it to the listeners, in write
// order, on its own goroutine.
type stateWALHasher struct {
	// Cancelled when the WAL stops. The goroutine exits on it, and it is the ctx passed to hash listeners.
	ctx context.Context

	// Cancels ctx with the error that bricks the WAL.
	cancel context.CancelCauseFunc

	// Carries hash jobs, flush requests, and register requests to the goroutine, in order.
	requestChan chan any

	// Tracks the goroutine so the WAL can wait for it to exit.
	wg sync.WaitGroup

	// The listeners each block's hash is delivered to, in registration order.
	listeners []HashListener

	// Whether lastBlockNumber and lastHash are set.
	hasLast bool

	// The block of the most recent hash delivered, or of the last stored block when the WAL was opened.
	lastBlockNumber uint64

	// The hash of lastBlockNumber.
	lastHash [32]byte
}

// A written block whose hash the hasher computes and delivers to the listeners.
type hashJob struct {
	// The block number.
	blockNumber uint64

	// The block's mutations.
	mutations []mutation.Mutation
}

// Asks the hasher to close done once every hash job sent before it has been delivered.
type flushRequest struct {
	// Closed by the hasher's goroutine when it reaches this request.
	done chan struct{}
}

// Asks the hasher to add a listener and report the most recent hash delivered before it was added.
type registerRequest struct {
	// The listener to add, or nil to only read the most recent hash.
	listener HashListener

	// Closed by the hasher's goroutine once the fields below are set.
	done chan struct{}

	// Whether lastBlockNumber and lastHash are set.
	hasLast bool

	// The block of the most recent hash delivered before listener was added, or of the last stored block when none
	// has been.
	lastBlockNumber uint64

	// The hash of lastBlockNumber.
	lastHash [32]byte
}

// newStateWALHasher returns a hasher with no listeners whose goroutine is running.
func newStateWALHasher(
	// Cancelled when the WAL stops.
	ctx context.Context,
	// Cancels ctx; the hasher calls it with the error that bricks the WAL.
	cancel context.CancelCauseFunc,
	// The capacity of the channel carrying requests to the goroutine.
	bufferSize uint,
	// Whether the WAL already stores a block, whose hash is reported to listeners registered before any delivery.
	hasLast bool,
	// The last stored block. Ignored unless hasLast.
	lastBlockNumber uint64,
	// The hash of lastBlockNumber. Ignored unless hasLast.
	lastHash [32]byte,
) *stateWALHasher {
	h := &stateWALHasher{
		ctx:             ctx,
		cancel:          cancel,
		requestChan:     make(chan any, bufferSize),
		hasLast:         hasLast,
		lastBlockNumber: lastBlockNumber,
		lastHash:        lastHash,
	}
	h.wg.Add(1)
	go h.run()
	return h
}

// ScheduleHash queues a written block to have its hash computed and delivered to the listeners, blocking while the
// queue is full. It returns false once the WAL stops.
func (h *stateWALHasher) ScheduleHash(
	// The block number.
	blockNumber uint64,
	// The block's mutations.
	mutations []mutation.Mutation,
) bool {
	return h.send(hashJob{blockNumber: blockNumber, mutations: mutations})
}

// Flush blocks until every block scheduled before the call has had its hash delivered to the listeners. It returns
// false once the WAL stops.
func (h *stateWALHasher) Flush() bool {
	request := flushRequest{done: make(chan struct{})}
	return h.send(request) && h.wait(request.done)
}

// RegisterListener adds listener, unless it is nil, after every block scheduled before the call has had its hash
// delivered. It reports the most recent hash delivered before listener was added, or the seeded hash when none has
// been.
func (h *stateWALHasher) RegisterListener(listener HashListener) (
	// False when there is neither a delivered nor a seeded hash.
	hasLast bool,
	// The block of the reported hash.
	lastBlockNumber uint64,
	// The reported hash.
	lastHash [32]byte,
	// False once the WAL stops, in which case listener was not added and the other results are undefined.
	accepted bool,
) {
	request := &registerRequest{listener: listener, done: make(chan struct{})}
	if !h.send(request) || !h.wait(request.done) {
		return false, 0, [32]byte{}, false
	}
	return request.hasLast, request.lastBlockNumber, request.lastHash, true
}

// AwaitExit blocks until the goroutine has exited, which it does once ctx is cancelled.
func (h *stateWALHasher) AwaitExit() {
	h.wg.Wait()
}

// run serves requests in the order they were sent: it computes and delivers each hash job's hash, answers flush
// requests, and adds listeners. A failure bricks the WAL.
func (h *stateWALHasher) run() {
	defer h.wg.Done()
	for {
		var request any
		select {
		case <-h.ctx.Done():
			return
		case request = <-h.requestChan:
		}

		switch r := request.(type) {
		case hashJob:
			if err := h.deliverHash(r); err != nil {
				h.cancel(err)
				return
			}
		case flushRequest:
			close(r.done)
		case *registerRequest:
			h.register(r)
		default:
			h.cancel(fmt.Errorf("unknown hasher request %T", request))
			return
		}
	}
}

// deliverHash computes the hash of job's block and delivers it to every listener, returning the first listener
// error. It rejects a block that does not immediately follow the last one hashed.
func (h *stateWALHasher) deliverHash(job hashJob) error {
	if h.hasLast && job.blockNumber != h.lastBlockNumber+1 {
		return fmt.Errorf("hash job for block %d does not follow the last hashed block %d",
			job.blockNumber, h.lastBlockNumber)
	}
	// STOPGAP: the block's second serialization, after the one for its WAL record. See computePlaceholderHash().
	hash := computePlaceholderHash(job.mutations)
	for _, listener := range h.listeners {
		if err := listener(h.ctx, job.blockNumber, hash); err != nil {
			return fmt.Errorf("a hash listener refused block %d: %w", job.blockNumber, err)
		}
	}
	h.hasLast = true
	h.lastBlockNumber = job.blockNumber
	h.lastHash = hash
	return nil
}

// register adds request's listener, unless it is nil, and answers it with the most recent hash.
func (h *stateWALHasher) register(request *registerRequest) {
	if request.listener != nil {
		h.listeners = append(h.listeners, request.listener)
	}
	request.hasLast = h.hasLast
	request.lastBlockNumber = h.lastBlockNumber
	request.lastHash = h.lastHash
	close(request.done)
}

// send hands request to the goroutine, blocking while its channel is full. It returns false once the WAL stops.
func (h *stateWALHasher) send(request any) bool {
	select {
	case h.requestChan <- request:
		return true
	case <-h.ctx.Done():
		return false
	}
}

// wait blocks until done is closed. It returns false once the WAL stops.
func (h *stateWALHasher) wait(done chan struct{}) bool {
	select {
	case <-done:
		return true
	case <-h.ctx.Done():
		return false
	}
}

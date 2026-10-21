package statewal

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
)

// stateWALSorter sorts each written block's mutations by key, then hands them to the underlying WAL and to the
// hasher, on its own goroutine and in write order. Every other request that must stay ordered with writes passes
// through it on the way to the underlying WAL or the hasher.
type stateWALSorter struct {
	// Cancelled when the WAL stops. The goroutine exits on it.
	ctx context.Context

	// Cancels ctx with the error that bricks the WAL.
	cancel context.CancelCauseFunc

	// Carries requests to the goroutine, in order.
	requestChan chan any

	// Tracks the goroutine so the WAL can wait for it to exit.
	wg sync.WaitGroup

	// The WAL each block is appended to.
	wal seiwal.WAL[stateWALRecord]

	// Computes each block's hash and delivers it to the listeners.
	hasher *stateWALHasher
}

// A written block to sort and hand on.
type sortJob struct {
	// The block number.
	blockNumber uint64

	// The block's mutations, in any order.
	mutations []mutation.Mutation
}

// Asks the sorter to flush the underlying WAL and then the hasher.
type sorterFlushRequest struct {
	// Receives the result.
	reply chan error
}

// Asks the sorter for the range of blocks stored in the underlying WAL.
type sorterBoundsRequest struct {
	// Receives the result.
	reply chan sorterBoundsResult
}

// The result of a sorterBoundsRequest.
type sorterBoundsResult struct {
	// Whether the WAL holds any block.
	ok bool

	// The lowest block stored.
	first uint64

	// The highest block stored.
	last uint64

	// Any error reading the range.
	err error
}

// Asks the sorter to prune the underlying WAL.
type sorterPruneRequest struct {
	// The lowest block to keep.
	lowestBlockNumberToKeep uint64

	// Receives the result.
	reply chan error
}

// Asks the sorter for an iterator over the underlying WAL.
type sorterIteratorRequest struct {
	// The first block to yield.
	startingBlockNumber uint64

	// The last block to yield.
	endingBlockNumber uint64

	// Receives the result.
	reply chan sorterIteratorResult
}

// The result of a sorterIteratorRequest.
type sorterIteratorResult struct {
	// The iterator, when err is nil.
	iterator seiwal.Iterator[stateWALRecord]

	// Any error creating the iterator.
	err error
}

// Asks the sorter to register a hash listener with the hasher.
type sorterRegisterRequest struct {
	// The listener to add, or nil to only read the most recent hash.
	listener HashListener

	// Receives the result.
	reply chan sorterRegisterResult
}

// The result of a sorterRegisterRequest.
type sorterRegisterResult struct {
	// Whether there is a reported hash.
	hasLast bool

	// The block of the reported hash.
	lastBlockNumber uint64

	// The reported hash.
	lastHash [32]byte

	// False when the hasher stopped before the listener was added.
	accepted bool
}

// Asks the sorter to exit, and to pass the close on to the hasher, once every request sent before it has been
// served.
type sorterCloseRequest struct{}

// newStateWALSorter returns a sorter whose goroutine is running.
func newStateWALSorter(
	// Cancelled when the WAL stops.
	ctx context.Context,
	// Cancels ctx; the sorter calls it with the error that bricks the WAL.
	cancel context.CancelCauseFunc,
	// The capacity of the channel carrying requests to the goroutine.
	bufferSize uint,
	// The WAL each block is appended to.
	wal seiwal.WAL[stateWALRecord],
	// Computes each block's hash and delivers it to the listeners.
	hasher *stateWALHasher,
) *stateWALSorter {
	s := &stateWALSorter{
		ctx:         ctx,
		cancel:      cancel,
		requestChan: make(chan any, bufferSize),
		wal:         wal,
		hasher:      hasher,
	}
	s.wg.Add(1)
	go s.run()
	return s
}

// ScheduleSort queues a block to be sorted and handed to the WAL and the hasher, blocking while the queue is full.
// It returns false once the WAL stops.
func (s *stateWALSorter) ScheduleSort(
	// The block number.
	blockNumber uint64,
	// The block's mutations. The sorter takes ownership of them.
	mutations []mutation.Mutation,
) bool {
	return s.send(sortJob{blockNumber: blockNumber, mutations: mutations})
}

// Flush blocks until every block written before the call is durable and has had its hash delivered.
func (s *stateWALSorter) Flush() error {
	request := sorterFlushRequest{reply: make(chan error, 1)}
	if !s.send(request) {
		return s.stoppedErr()
	}
	select {
	case err := <-request.reply:
		return err
	case <-s.ctx.Done():
		return s.stoppedErr()
	}
}

// Bounds reports the range of blocks stored in the underlying WAL, including every block written before the call.
func (s *stateWALSorter) Bounds() (bool, uint64, uint64, error) {
	request := sorterBoundsRequest{reply: make(chan sorterBoundsResult, 1)}
	if !s.send(request) {
		return false, 0, 0, s.stoppedErr()
	}
	select {
	case result := <-request.reply:
		return result.ok, result.first, result.last, result.err
	case <-s.ctx.Done():
		return false, 0, 0, s.stoppedErr()
	}
}

// Prune schedules removal of the blocks below lowestBlockNumberToKeep from the underlying WAL.
func (s *stateWALSorter) Prune(lowestBlockNumberToKeep uint64) error {
	request := sorterPruneRequest{lowestBlockNumberToKeep: lowestBlockNumberToKeep, reply: make(chan error, 1)}
	if !s.send(request) {
		return s.stoppedErr()
	}
	select {
	case err := <-request.reply:
		return err
	case <-s.ctx.Done():
		return s.stoppedErr()
	}
}

// Iterator returns an iterator over the underlying WAL that includes every block written before the call.
func (s *stateWALSorter) Iterator(
	// The first block to yield.
	startingBlockNumber uint64,
	// The last block to yield.
	endingBlockNumber uint64,
) (seiwal.Iterator[stateWALRecord], error) {
	request := sorterIteratorRequest{
		startingBlockNumber: startingBlockNumber,
		endingBlockNumber:   endingBlockNumber,
		reply:               make(chan sorterIteratorResult, 1),
	}
	if !s.send(request) {
		return nil, s.stoppedErr()
	}
	select {
	case result := <-request.reply:
		return result.iterator, result.err
	case <-s.ctx.Done():
		return nil, s.stoppedErr()
	}
}

// RegisterListener adds listener to the hasher once every block written before the call has had its hash
// delivered, as stateWALHasher.RegisterListener() does.
func (s *stateWALSorter) RegisterListener(listener HashListener) (
	// False when there is neither a delivered nor a seeded hash.
	hasLast bool,
	// The block of the reported hash.
	lastBlockNumber uint64,
	// The reported hash.
	lastHash [32]byte,
	// False once the WAL stops, in which case listener was not added and the other results are undefined.
	accepted bool,
) {
	request := sorterRegisterRequest{listener: listener, reply: make(chan sorterRegisterResult, 1)}
	if !s.send(request) {
		return false, 0, [32]byte{}, false
	}
	select {
	case result := <-request.reply:
		return result.hasLast, result.lastBlockNumber, result.lastHash, result.accepted
	case <-s.ctx.Done():
		return false, 0, [32]byte{}, false
	}
}

// ScheduleClose queues a request for the sorter, and then the hasher, to exit once every request sent before it has
// been served. It returns false once the WAL stops.
func (s *stateWALSorter) ScheduleClose() bool {
	return s.send(sorterCloseRequest{})
}

// AwaitExit blocks until the goroutine has exited.
func (s *stateWALSorter) AwaitExit() {
	s.wg.Wait()
}

// run serves requests in the order they were sent. A failed append bricks the WAL.
func (s *stateWALSorter) run() {
	defer s.wg.Done()
	for {
		var request any
		select {
		case <-s.ctx.Done():
			return
		case request = <-s.requestChan:
		}

		switch r := request.(type) {
		case sortJob:
			slices.SortFunc(r.mutations, func(a mutation.Mutation, b mutation.Mutation) int {
				return bytes.Compare(a.Key, b.Key)
			})
			if err := s.wal.Append(r.blockNumber, stateWALRecord{mutations: r.mutations}); err != nil {
				s.cancel(fmt.Errorf("failed to append block %d: %w", r.blockNumber, err))
				return
			}
			if !s.hasher.ScheduleHash(r.blockNumber, r.mutations) {
				return
			}
		case sorterFlushRequest:
			r.reply <- s.flush()
		case sorterBoundsRequest:
			ok, first, last, err := s.wal.Bounds()
			r.reply <- sorterBoundsResult{ok: ok, first: first, last: last, err: err}
		case sorterPruneRequest:
			r.reply <- s.wal.PruneBefore(r.lowestBlockNumberToKeep)
		case sorterIteratorRequest:
			iterator, err := s.wal.Iterator(r.startingBlockNumber, r.endingBlockNumber)
			r.reply <- sorterIteratorResult{iterator: iterator, err: err}
		case sorterRegisterRequest:
			hasLast, lastBlockNumber, lastHash, accepted := s.hasher.RegisterListener(r.listener)
			r.reply <- sorterRegisterResult{
				hasLast:         hasLast,
				lastBlockNumber: lastBlockNumber,
				lastHash:        lastHash,
				accepted:        accepted,
			}
		case sorterCloseRequest:
			s.hasher.ScheduleClose()
			return
		default:
			s.cancel(fmt.Errorf("unknown sorter request %T", request))
			return
		}
	}
}

// flush makes every block appended so far durable, then waits for the hasher to deliver their hashes.
func (s *stateWALSorter) flush() error {
	if err := s.wal.Flush(); err != nil {
		return fmt.Errorf("failed to flush state WAL: %w", err)
	}
	if !s.hasher.Flush() {
		return fmt.Errorf("failed to deliver hashes: %w", s.stoppedErr())
	}
	return nil
}

// send hands request to the goroutine, blocking while its channel is full. It returns false once the WAL stops.
func (s *stateWALSorter) send(request any) bool {
	select {
	case s.requestChan <- request:
		return true
	case <-s.ctx.Done():
		return false
	}
}

// stoppedErr describes why the WAL stopped.
func (s *stateWALSorter) stoppedErr() error {
	return fmt.Errorf("state WAL stopped: %w", context.Cause(s.ctx))
}

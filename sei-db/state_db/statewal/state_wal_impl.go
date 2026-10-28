package statewal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
)

var _ StateWAL = (*stateWALImpl)(nil)

// A WAL for storing each block's state mutations by block number.
//
// Each block is sorted, serialized, appended and hashed, and its hash delivered to the listeners, on the WAL's own
// goroutine and in write order. Every request that must stay ordered with writes passes through that goroutine.
//
// Not safe for concurrent use; see the StateWAL interface doc. The gc.PrunableStore surface in
// state_wal_gc.go is the exception: it runs on the collector's goroutine, and touches only lastBlock and
// requestChan.
type stateWALImpl struct {
	// The underlying WAL, keyed by block number, whose entries are serialized blocks. Used only by the goroutine.
	wal seiwal.WAL[[]byte]

	// Set by Close() so subsequent calls fail fast. Only ever touched by the single caller.
	closed bool

	// Cancelled when the WAL stops: by fail() with the fatal error that bricked the WAL as its cause, or by
	// Close() with a nil cause. Every blocking operation aborts on it, and it is the ctx passed to hash listeners.
	ctx context.Context

	// Cancels ctx, recording the fatal error (or nil) as its cause. Only the first call takes effect.
	cancel context.CancelCauseFunc

	// Carries requests to the goroutine, in order.
	requestChan chan any

	// Tracks the goroutine so Close() can wait for it to exit.
	wg sync.WaitGroup

	// The listeners each block's hash is delivered to, in registration order. Owned by the goroutine.
	listeners []HashListener

	// Whether lastHashedBlock and lastHash are set. Owned by the goroutine.
	hasLastHash bool

	// The block of the most recent hash delivered, or of the last stored block when the WAL was opened. Owned by
	// the goroutine.
	lastHashedBlock uint64

	// The hash of lastHashedBlock. Owned by the goroutine.
	lastHash [32]byte

	// The highest block number written. Atomic because the garbage collector reads it off-goroutine
	// (GetLatestBlock); Write is its only mutator.
	//
	// 0 also means nothing has been written yet, so a WAL whose only block is block 0 is
	// indistinguishable from an empty one.
	lastBlock atomic.Uint64

	// Whether any block has been written (this session or recovered from disk), which is what tells an
	// empty WAL apart from one holding only block 0. Only ever touched by the single caller.
	hasBlock bool
}

// New opens (or creates) a state WAL in the configured directory, recovering any files left behind by a
// previous session.
func New(config *Config) (StateWAL, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid state WAL config: %w", err)
	}
	wal, err := seiwal.NewWAL(config.toSeiwalConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to open state WAL: %w", err)
	}
	return newStateWAL(wal, config.RequestBufferSize)
}

// GetRange reports the range of block numbers stored in the state WAL directory configured by config,
// without constructing a live StateWAL. Like the seiwal function it wraps, it runs the recovery/sanity pass
// (which seals any unsealed file left by a prior session) before reading, so it mutates the directory.
//
// The range is read from sealed file names only, so its cost is a directory listing regardless of how much
// data the WAL holds; content is not checked. Use VerifyIntegrity to check for corruption.
//
// NOT SAFE FOR CONCURRENT USE with a live StateWAL, or with another GetRange/PruneAfter, on the same
// directory: it seals files a running WAL owns. Call it only while no StateWAL is open there (e.g. offline,
// at startup before New). For a range query against a live WAL use the instance method GetStoredRange
// instead.
func GetRange(config *Config) (bool, uint64, uint64, error) {
	ok, first, last, err := seiwal.GetRange(config.Path)
	if err != nil {
		return false, 0, 0, fmt.Errorf("failed to read state WAL range: %w", err)
	}
	return ok, first, last, nil
}

// PruneAfter deletes all data for blocks after highestBlockToKeep from the state WAL directory configured by
// config, without constructing a live StateWAL. It runs the recovery/sanity pass, applies the rollback, and
// re-scans the result structurally (file names / sequence contiguity, not contents); blocks with a number
// <= highestBlockToKeep are kept.
//
// NOT SAFE FOR CONCURRENT USE with a live StateWAL, or with another GetRange/PruneAfter, on the same
// directory: it seals, rewrites, and removes files a running WAL owns. Call it only while no StateWAL is open
// there (e.g. offline, at startup before New).
func PruneAfter(config *Config, highestBlockToKeep uint64) error {
	if err := seiwal.PruneAfter(config.Path, highestBlockToKeep); err != nil {
		return fmt.Errorf("failed to prune state WAL: %w", err)
	}
	return nil
}

// VerifyIntegrity reads every sealed file in the state WAL directory configured by config and confirms each
// record's CRC and each file's name-versus-content range. This is the expensive O(total stored bytes) check
// that New/GetRange/PruneAfter deliberately skip; call it only when corruption is suspected. It is read-only
// and reports every problem it finds in a single pass, returning nil when the durable log is clean.
//
// NOT SAFE FOR CONCURRENT USE with a live StateWAL, or with GetRange/PruneAfter, on the same directory. Call
// it only while no StateWAL is open there (e.g. offline, at startup before New).
func VerifyIntegrity(config *Config) error {
	if err := seiwal.VerifyIntegrity(config.Path); err != nil {
		return fmt.Errorf("state WAL integrity check failed: %w", err)
	}
	return nil
}

func newStateWAL(
	wal seiwal.WAL[[]byte],
	// The capacity of the channel carrying requests to the goroutine.
	requestBufferSize uint,
) (StateWAL, error) {
	// Recover the write-ordering position, and the hash reported to listeners registered before any block is
	// written, from the highest block already on disk.
	ok, _, last, err := wal.Bounds()
	if err != nil {
		_ = wal.Close()
		return nil, fmt.Errorf("failed to read WAL bounds: %w", err)
	}
	var lastHash [32]byte
	if ok {
		lastHash, err = computeStoredHash(wal, last)
		if err != nil {
			_ = wal.Close()
			return nil, fmt.Errorf("failed to compute the hash of the last stored block: %w", err)
		}
	}

	ctx, cancel := context.WithCancelCause(context.Background())
	w := &stateWALImpl{
		wal:             wal,
		ctx:             ctx,
		cancel:          cancel,
		requestChan:     make(chan any, requestBufferSize),
		hasLastHash:     ok,
		lastHashedBlock: last,
		lastHash:        lastHash,
	}
	if ok {
		w.lastBlock.Store(last)
		w.hasBlock = true
	}
	w.wg.Add(1)
	go w.run()
	return w, nil
}

// computeStoredHash returns the hash of a block already stored in wal.
func computeStoredHash(
	// The WAL holding the block.
	wal seiwal.WAL[[]byte],
	// The block to compute the hash of.
	blockNumber uint64,
) ([32]byte, error) {
	it, err := wal.Iterator(blockNumber, blockNumber)
	if err != nil {
		return [32]byte{}, fmt.Errorf("failed to create WAL iterator: %w", err)
	}
	defer func() { _ = it.Close() }()

	ok, err := it.Next()
	if err != nil {
		return [32]byte{}, fmt.Errorf("failed to read block %d: %w", blockNumber, err)
	}
	if !ok {
		return [32]byte{}, fmt.Errorf("block %d is not stored", blockNumber)
	}
	_, serialized := it.Entry()
	hash, err := computePlaceholderHash(serialized)
	if err != nil {
		return [32]byte{}, fmt.Errorf("failed to hash block %d: %w", blockNumber, err)
	}
	return hash, nil
}

// Write appends a block's mutations to the WAL as a single record.
func (w *stateWALImpl) Write(blockNumber uint64, mutations []mutation.Mutation) error {
	if w.closed {
		return fmt.Errorf("state WAL is closed")
	}
	if err := w.fatalErr(); err != nil {
		return fmt.Errorf("state WAL failed: %w", err)
	}
	if err := w.checkBlockOrder(blockNumber); err != nil {
		return fmt.Errorf("write rejected: %w", err)
	}

	// The goroutine sorts the block in place, so it is given its own slice and the caller keeps theirs.
	if !w.send(writeRequest{blockNumber: blockNumber, mutations: slices.Clone(mutations)}) {
		return fmt.Errorf("failed to schedule block %d: %w", blockNumber, w.stoppedErr())
	}
	w.lastBlock.Store(blockNumber)
	w.hasBlock = true
	return nil
}

// checkBlockOrder rejects a block number that breaks the contiguity rule: the first block written to an
// empty WAL may be any number, and every block after it must be exactly one greater than the last. It
// only reads the write-ordering state, so a Write that fails downstream of it leaves the head untouched.
func (w *stateWALImpl) checkBlockOrder(blockNumber uint64) error {
	if !w.hasBlock {
		return nil
	}
	last := w.lastBlock.Load()
	if blockNumber != last+1 {
		return fmt.Errorf("block number %d is not contiguous with the last block written %d (expected %d)",
			blockNumber, last, last+1)
	}
	return nil
}

// Flush blocks until all previously scheduled writes are durable and their hashes delivered.
func (w *stateWALImpl) Flush() error {
	if w.closed {
		return fmt.Errorf("state WAL is closed")
	}
	if err := w.fatalErr(); err != nil {
		return fmt.Errorf("state WAL failed: %w", err)
	}
	request := flushRequest{reply: make(chan error, 1)}
	if !w.send(request) {
		return w.stoppedErr()
	}
	select {
	case err := <-request.reply:
		if err != nil {
			return w.fail(fmt.Errorf("failed to flush state WAL: %w", err))
		}
		return nil
	case <-w.ctx.Done():
		return w.stoppedErr()
	}
}

// RegisterHashListener adds listener to those each block's hash is delivered to, and reports the most recent hash.
func (w *stateWALImpl) RegisterHashListener(listener HashListener) (bool, uint64, [32]byte, error) {
	if w.closed {
		return false, 0, [32]byte{}, fmt.Errorf("state WAL is closed")
	}
	if err := w.fatalErr(); err != nil {
		return false, 0, [32]byte{}, fmt.Errorf("state WAL failed: %w", err)
	}
	request := registerRequest{listener: listener, reply: make(chan registerResult, 1)}
	if !w.send(request) {
		return false, 0, [32]byte{}, w.stoppedErr()
	}
	select {
	case result := <-request.reply:
		return result.hasLastHash, result.lastHashedBlock, result.lastHash, nil
	case <-w.ctx.Done():
		return false, 0, [32]byte{}, w.stoppedErr()
	}
}

// GetStoredRange reports the range of complete blocks stored in the WAL.
func (w *stateWALImpl) GetStoredRange() (bool, uint64, uint64, error) {
	if w.closed {
		return false, 0, 0, fmt.Errorf("state WAL is closed")
	}
	if err := w.fatalErr(); err != nil {
		return false, 0, 0, fmt.Errorf("state WAL failed: %w", err)
	}
	request := boundsRequest{reply: make(chan boundsResult, 1)}
	if !w.send(request) {
		return false, 0, 0, w.stoppedErr()
	}
	select {
	case result := <-request.reply:
		if result.err != nil {
			return false, 0, 0, w.fail(fmt.Errorf("failed to read WAL bounds: %w", result.err))
		}
		return result.ok, result.first, result.last, nil
	case <-w.ctx.Done():
		return false, 0, 0, w.stoppedErr()
	}
}

// Prune schedules removal of whole underlying files below lowestBlockNumberToKeep. It does not block on
// completion.
func (w *stateWALImpl) Prune(lowestBlockNumberToKeep uint64) error {
	if w.closed {
		return fmt.Errorf("state WAL is closed")
	}
	if err := w.fatalErr(); err != nil {
		return fmt.Errorf("state WAL failed: %w", err)
	}
	if err := w.prune(lowestBlockNumberToKeep); err != nil {
		return w.fail(fmt.Errorf("failed to prune state WAL: %w", err))
	}
	return nil
}

// Iterator returns an iterator over the inclusive block range [startingBlockNumber, endingBlockNumber]. It
// yields (blockNumber, mutations) for each block.
func (w *stateWALImpl) Iterator(
	startingBlockNumber uint64,
	endingBlockNumber uint64,
) (StateWALIterator, error) {
	if w.closed {
		return nil, fmt.Errorf("state WAL is closed")
	}
	if err := w.fatalErr(); err != nil {
		return nil, fmt.Errorf("state WAL failed: %w", err)
	}
	request := iteratorRequest{
		startingBlockNumber: startingBlockNumber,
		endingBlockNumber:   endingBlockNumber,
		reply:               make(chan iteratorResult, 1),
	}
	if !w.send(request) {
		return nil, w.stoppedErr()
	}
	var result iteratorResult
	select {
	case result = <-request.reply:
	case <-w.ctx.Done():
		return nil, w.stoppedErr()
	}
	it, err := result.iterator, result.err
	if err != nil {
		// A rejected range is the caller's error and leaves the WAL usable; only a genuine WAL failure bricks.
		if errors.Is(err, seiwal.ErrIteratorRange) {
			return nil, fmt.Errorf("failed to create WAL iterator: %w", err)
		}
		return nil, w.fail(fmt.Errorf("failed to create WAL iterator: %w", err))
	}
	return &stateWALIterator{serializedBlocks: it}, nil
}

// Close blocks until all work submitted before the call has been fully handled, then closes the underlying WAL.
func (w *stateWALImpl) Close() error {
	w.closed = true
	w.send(closeRequest{})
	w.wg.Wait()
	w.cancel(nil)
	if err := w.wal.Close(); err != nil {
		return fmt.Errorf("failed to close state WAL: %w", err)
	}
	return nil
}

// fail records err as the fatal error that bricks the WAL, unless the WAL has already stopped, and returns it.
// Once bricked, every subsequent operation fails fast rather than touching the underlying WAL. Safe to call from
// any goroutine.
func (w *stateWALImpl) fail(err error) error {
	w.cancel(err)
	return err
}

// fatalErr returns the fatal error that bricked the WAL, or nil if it has not been bricked. Safe to call from any
// goroutine.
func (w *stateWALImpl) fatalErr() error {
	// Close() cancels with a nil cause, which context reports as context.Canceled; fail() never passes that bare.
	if cause := context.Cause(w.ctx); cause != nil && cause != context.Canceled {
		return cause
	}
	return nil
}

// stoppedErr describes why the WAL stopped: the fatal error that bricked it, or that it was closed.
func (w *stateWALImpl) stoppedErr() error {
	if err := w.fatalErr(); err != nil {
		return fmt.Errorf("state WAL failed: %w", err)
	}
	return fmt.Errorf("state WAL is closed")
}

// prune schedules removal of the blocks below lowestBlockNumberToKeep from the underlying WAL. Safe to call from any
// goroutine.
func (w *stateWALImpl) prune(lowestBlockNumberToKeep uint64) error {
	request := pruneRequest{lowestBlockNumberToKeep: lowestBlockNumberToKeep, reply: make(chan error, 1)}
	if !w.send(request) {
		return w.stoppedErr()
	}
	select {
	case err := <-request.reply:
		return err
	case <-w.ctx.Done():
		return w.stoppedErr()
	}
}

// run serves requests in the order they were sent. A failed write bricks the WAL.
func (w *stateWALImpl) run() {
	defer w.wg.Done()
	for {
		var request any
		select {
		case <-w.ctx.Done():
			return
		case request = <-w.requestChan:
		}

		switch r := request.(type) {
		case writeRequest:
			if err := w.write(r); err != nil {
				w.cancel(err)
				return
			}
		case flushRequest:
			r.reply <- w.wal.Flush()
		case boundsRequest:
			ok, first, last, err := w.wal.Bounds()
			r.reply <- boundsResult{ok: ok, first: first, last: last, err: err}
		case pruneRequest:
			r.reply <- w.wal.PruneBefore(r.lowestBlockNumberToKeep)
		case iteratorRequest:
			iterator, err := w.wal.Iterator(r.startingBlockNumber, r.endingBlockNumber)
			r.reply <- iteratorResult{iterator: iterator, err: err}
		case registerRequest:
			if r.listener != nil {
				w.listeners = append(w.listeners, r.listener)
			}
			r.reply <- registerResult{
				hasLastHash:     w.hasLastHash,
				lastHashedBlock: w.lastHashedBlock,
				lastHash:        w.lastHash,
			}
		case closeRequest:
			return
		default:
			w.cancel(fmt.Errorf("unknown state WAL request %T", request))
			return
		}
	}
}

// write sorts the block's mutations by key, appends their serialization to the WAL, and delivers the block's hash to
// every listener, returning the first error.
func (w *stateWALImpl) write(request writeRequest) error {
	slices.SortFunc(request.mutations, func(a mutation.Mutation, b mutation.Mutation) int {
		return bytes.Compare(a.Key, b.Key)
	})
	serialized := serializeBlock(request.mutations)
	if err := w.wal.Append(request.blockNumber, serialized); err != nil {
		return fmt.Errorf("failed to append block %d: %w", request.blockNumber, err)
	}

	hash, err := computePlaceholderHash(serialized)
	if err != nil {
		return fmt.Errorf("failed to hash block %d: %w", request.blockNumber, err)
	}
	for _, listener := range w.listeners {
		if err := listener(w.ctx, request.blockNumber, hash); err != nil {
			return fmt.Errorf("a hash listener refused block %d: %w", request.blockNumber, err)
		}
	}
	w.hasLastHash = true
	w.lastHashedBlock = request.blockNumber
	w.lastHash = hash
	return nil
}

// send hands request to the goroutine, blocking while its channel is full. It returns false once the WAL stops.
func (w *stateWALImpl) send(request any) bool {
	select {
	case w.requestChan <- request:
		return true
	case <-w.ctx.Done():
		return false
	}
}

// A written block to sort, serialize, append and hash.
type writeRequest struct {
	// The block number.
	blockNumber uint64

	// The block's mutations, in any order.
	mutations []mutation.Mutation
}

// Asks the goroutine to flush the underlying WAL.
type flushRequest struct {
	// Receives the result.
	reply chan error
}

// Asks the goroutine for the range of blocks stored in the underlying WAL.
type boundsRequest struct {
	// Receives the result.
	reply chan boundsResult
}

// The result of a boundsRequest.
type boundsResult struct {
	// Whether the WAL holds any block.
	ok bool

	// The lowest block stored.
	first uint64

	// The highest block stored.
	last uint64

	// Any error reading the range.
	err error
}

// Asks the goroutine to prune the underlying WAL.
type pruneRequest struct {
	// The lowest block to keep.
	lowestBlockNumberToKeep uint64

	// Receives the result.
	reply chan error
}

// Asks the goroutine for an iterator over the underlying WAL.
type iteratorRequest struct {
	// The first block to yield.
	startingBlockNumber uint64

	// The last block to yield.
	endingBlockNumber uint64

	// Receives the result.
	reply chan iteratorResult
}

// The result of an iteratorRequest.
type iteratorResult struct {
	// The iterator, when err is nil.
	iterator seiwal.Iterator[[]byte]

	// Any error creating the iterator.
	err error
}

// Asks the goroutine to add a hash listener and report the most recent hash delivered before it was added.
type registerRequest struct {
	// The listener to add, or nil to only read the most recent hash.
	listener HashListener

	// Receives the result.
	reply chan registerResult
}

// The result of a registerRequest.
type registerResult struct {
	// Whether lastHashedBlock and lastHash are set.
	hasLastHash bool

	// The block of the most recent hash delivered before the listener was added, or of the last stored block when
	// none has been.
	lastHashedBlock uint64

	// The hash of lastHashedBlock.
	lastHash [32]byte
}

// Asks the goroutine to exit once every request sent before it has been served.
type closeRequest struct{}

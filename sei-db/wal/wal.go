package wal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tidwall/wal"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
)

// The size of internal channel buffers if the provided buffer size is less than 1.
const defaultBufferSize = 1024

// The size of write batches if the provided write batch size is less than 1.
const defaultWriteBatchSize = 64

// WAL is a generic write-ahead log implementation.
type WAL[T any] struct {
	ctx    context.Context
	cancel context.CancelFunc

	dir       string
	log       *wal.Log
	config    Config
	logger    logger.Logger
	marshal   MarshalFn[T]
	unmarshal UnmarshalFn[T]

	// The size of write batches.
	writeBatchSize int
	asyncWrites    bool

	writeChan    chan *writeRequest[T]
	truncateChan chan *truncateRequest
	closeReqChan chan struct{}
	closeErrChan chan error
}

// A request to truncate the log.
type truncateRequest struct {
	// If true, truncate before the provided index. Otherwise, truncate after the provided index.
	before bool
	// The index to truncate at.
	index uint64
	// Errors are returned over this channel, nil is written if completed with no error
	errChan chan error
}

// A request to write to the WAL.
type writeRequest[T any] struct {
	// The data to write
	entry T
	// Errors are returned over this channel, nil is written if completed with no error
	errChan chan error
}

// Configuration for the WAL.
type Config struct {
	// The number of recent entries to keep in the log.
	KeepRecent uint64

	// The interval at which to prune the log.
	PruneInterval time.Duration

	// The size of internal buffers. Also controls whether or not the Write method is asynchronous.
	//
	// If BufferSize is greater than 0, then the Write method is asynchronous, and the size of internal
	// buffers is set to the provided value. If Buffer size is less than 1, then the Write method is synchronous,
	// and any internal buffers are set to a default size.
	WriteBufferSize int

	// The size of write batches. If less than or equal to 0, a default of 64 is used.
	// If 1, no batching is done.
	WriteBatchSize int

	// If true, do an fsync after each write.
	FsyncEnabled bool

	// If true, make a deep copy of the data for every write. If false, then it is not safe to modify the data after
	// reading/writing it.
	DeepCopyEnabled bool
}

// NewWAL creates a new generic write-ahead log that persists entries.
// marshal and unmarshal functions are used to serialize/deserialize entries.
// Example:
//
//	NewWAL(
//	    func(e proto.ChangelogEntry) ([]byte, error) { return e.Marshal() },
//	    func(data []byte) (proto.ChangelogEntry, error) {
//	        var e proto.ChangelogEntry
//	        err := e.Unmarshal(data)
//	        return e, err
//	    },
//	    logger, dir, config,
//	)
func NewWAL[T any](
	ctx context.Context,
	marshal MarshalFn[T],
	unmarshal UnmarshalFn[T],
	logger logger.Logger,
	dir string,
	config Config,
) (*WAL[T], error) {
	log, err := open(dir, &wal.Options{
		NoSync: !config.FsyncEnabled,
		NoCopy: !config.DeepCopyEnabled,
	})
	if err != nil {
		return nil, err
	}

	bufferSize := config.WriteBufferSize
	if config.WriteBufferSize <= 0 {
		bufferSize = defaultBufferSize
	}

	asyncWrites := config.WriteBufferSize > 0

	writeBatchSize := config.WriteBatchSize
	if writeBatchSize <= 0 {
		writeBatchSize = defaultWriteBatchSize
	}

	ctx, cancel := context.WithCancel(ctx)

	w := &WAL[T]{
		ctx:            ctx,
		cancel:         cancel,
		dir:            dir,
		log:            log,
		config:         config,
		logger:         logger,
		marshal:        marshal,
		unmarshal:      unmarshal,
		writeBatchSize: writeBatchSize,
		asyncWrites:    asyncWrites,
		closeReqChan:   make(chan struct{}),
		closeErrChan:   make(chan error, 1),
		writeChan:      make(chan *writeRequest[T], bufferSize),
		truncateChan:   make(chan *truncateRequest, bufferSize),
	}

	go w.mainLoop()

	return w, nil

}

// Write will append a new entry to the end of the log.
// Whether the writes is in blocking or async manner depends on the buffer size.
// For async writes, this also checks for any previous async write errors.
func (walLog *WAL[T]) Write(entry T) error {

	errChan := make(chan error, 1)
	req := &writeRequest[T]{
		entry:   entry,
		errChan: errChan,
	}

	err := interuptablePush(walLog.ctx, walLog.writeChan, req)
	if err != nil {
		return fmt.Errorf("failed to push write request: %w", err)
	}

	if walLog.asyncWrites {
		// Do not wait for the write to be durable
		return nil
	}

	err, pullErr := interuptablePull(walLog.ctx, errChan)
	if pullErr != nil {
		return fmt.Errorf("failed to pull write error: %w", pullErr)
	}
	if err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	return nil
}

// This method is called asynchronously in response to a call to Write.
func (walLog *WAL[T]) handleWrite(req *writeRequest[T]) {
	if walLog.writeBatchSize <= 1 {
		walLog.handleUnbatchedWrite(req)
	} else {
		walLog.handleBatchedWrite(req)
	}
}

// handleUnbatchedWrite is called when no batching is enabled. Processes a single write request.
func (walLog *WAL[T]) handleUnbatchedWrite(req *writeRequest[T]) {

	bz, err := walLog.marshal(req.entry)
	if err != nil {
		req.errChan <- fmt.Errorf("marshalling error: %w", err)
		return
	}
	lastOffset, err := walLog.log.LastIndex()
	if err != nil {
		req.errChan <- fmt.Errorf("error fetching last index: %w", err)
		return
	}
	if err := walLog.log.Write(lastOffset+1, bz); err != nil {
		req.errChan <- fmt.Errorf("failed to write: %w", err)
		return
	}

	req.errChan <- nil
}

// handleBatchedWrite is called when batching is enabled. This method may pop pending writes from the writeChan and
// include them in the batch.
func (walLog *WAL[T]) handleBatchedWrite(req *writeRequest[T]) {

	requests := walLog.gatherRequestsForBatch(req)

	lastOffset, err := walLog.log.LastIndex()
	if err != nil {
		err = fmt.Errorf("error fetching last index: %w", err)
		for _, req := range requests {
			req.errChan <- err
		}
		return
	}

	binaryRequests := walLog.marshalRequests(requests)

	batch := &wal.Batch{}
	for _, binaryRequest := range binaryRequests {
		batch.Write(lastOffset+1, binaryRequest)
		lastOffset++
	}

	if err := walLog.log.WriteBatch(batch); err != nil {
		err = fmt.Errorf("failed to write batch: %w", err)
		for _, r := range requests {
			if r.errChan != nil {
				r.errChan <- err
			}
		}
		return
	}

	for _, r := range requests {
		if r.errChan != nil {
			r.errChan <- nil
		}
	}
}

// Gather the requests for a batch. When this method is called, we will already have the first request in the batch.
func (walLog *WAL[T]) gatherRequestsForBatch(initialRequest *writeRequest[T]) []*writeRequest[T] {
	requests := make([]*writeRequest[T], 0)
	requests = append(requests, initialRequest)

	keepLooking := true
	for keepLooking && len(requests) < walLog.writeBatchSize {
		select {
		case next := <-walLog.writeChan:
			requests = append(requests, next)
		default:
			// No more pending writes immediately available, so process the batch we have so far.
			keepLooking = false
		}
	}

	return requests
}

// Marshal the requests for a batch. If a request can't be marshalled, an error is immediately sent
// to that request's caller.
//
// The requests slice passed into this method is modified if some requests
// are not marshalled successfully. Any request that is not marshalled successfully has its errChan
// set to nil to avoid sending more than one response to the caller.
func (walLog *WAL[T]) marshalRequests(requests []*writeRequest[T]) [][]byte {
	binaryRequests := make([][]byte, 0, len(requests))

	for _, req := range requests {
		bz, err := walLog.marshal(req.entry)
		if err != nil {
			err = fmt.Errorf("marshalling error: %w", err)
			req.errChan <- err
			req.errChan = nil // signal that we have already sent a response to the caller
			continue
		}
		binaryRequests = append(binaryRequests, bz)
	}

	return binaryRequests
}

// TruncateAfter will remove all entries that are after the provided `index`.
// In other words the entry at `index` becomes the last entry in the log.
func (walLog *WAL[T]) TruncateAfter(index uint64) error {
	return walLog.sendTruncate(false, index)
}

// TruncateBefore will remove all entries that are before the provided `index`.
// In other words the entry at `index` becomes the first entry in the log.
func (walLog *WAL[T]) TruncateBefore(index uint64) error {
	return walLog.sendTruncate(true, index)
}

// sendTruncate sends a truncate request to the main loop and waits for completion.
func (walLog *WAL[T]) sendTruncate(before bool, index uint64) error {
	req := &truncateRequest{
		before:  before,
		index:   index,
		errChan: make(chan error, 1),
	}

	err := interuptablePush(walLog.ctx, walLog.truncateChan, req)
	if err != nil {
		return fmt.Errorf("failed to push truncate request: %w", err)
	}

	err, pullErr := interuptablePull(walLog.ctx, req.errChan)
	if pullErr != nil {
		return fmt.Errorf("failed to pull truncate error: %w", pullErr)
	}
	if err != nil {
		return fmt.Errorf("failed to truncate: %w", err)
	}

	return nil
}

// handleTruncate runs on the main loop and performs the truncation.
func (walLog *WAL[T]) handleTruncate(req *truncateRequest) {
	var err error
	if req.before {
		err = walLog.log.TruncateFront(req.index)
	} else {
		err = walLog.log.TruncateBack(req.index)
	}
	if err != nil {
		req.errChan <- fmt.Errorf("failed to truncate: %w", err)
		return
	}
	req.errChan <- nil
}

func (walLog *WAL[T]) FirstOffset() (uint64, error) {
	val, err := walLog.log.FirstIndex()
	if err != nil {
		return 0, fmt.Errorf("failed to get first offset: %w", err)
	}
	return val, nil
}

// LastOffset returns the last written offset/index of the log.
func (walLog *WAL[T]) LastOffset() (uint64, error) {
	val, err := walLog.log.LastIndex()
	if err != nil {
		return 0, fmt.Errorf("failed to get last offset: %w", err)
	}
	return val, nil
}

// ReadAt will read the log entry at the provided index.
func (walLog *WAL[T]) ReadAt(index uint64) (T, error) {
	var zero T
	bz, err := walLog.log.Read(index)
	if err != nil {
		return zero, fmt.Errorf("read log failed, %w", err)
	}
	entry, err := walLog.unmarshal(bz)
	if err != nil {
		return zero, fmt.Errorf("unmarshal log failed, %w", err)
	}
	return entry, nil
}

// Replay will read the replay log and process each log entry with the provided function.
func (walLog *WAL[T]) Replay(start uint64, end uint64, processFn func(index uint64, entry T) error) error {
	for i := start; i <= end; i++ {
		bz, err := walLog.log.Read(i)
		if err != nil {
			return fmt.Errorf("read log failed, %w", err)
		}
		entry, err := walLog.unmarshal(bz)
		if err != nil {
			return fmt.Errorf("unmarshal log failed, %w", err)

		}
		err = processFn(i, entry)
		if err != nil {
			return fmt.Errorf("process log failed, %w", err)
		}
	}
	return nil
}

func (walLog *WAL[T]) prune() {
	keepRecent := walLog.config.KeepRecent
	if keepRecent <= 0 || walLog.config.PruneInterval <= 0 {
		// Pruning is disabled. This is a defensive check, since
		// this method should only be called if pruning is enabled.
		return
	}

	lastIndex, err := walLog.log.LastIndex()
	if err != nil {
		walLog.logger.Error("failed to get last index for pruning", "err", err)
		return
	}
	firstIndex, err := walLog.log.FirstIndex()
	if err != nil {
		walLog.logger.Error("failed to get first index for pruning", "err", err)
		return
	}

	if lastIndex > keepRecent && (lastIndex-keepRecent) > firstIndex {
		prunePos := lastIndex - keepRecent
		if err := walLog.log.TruncateFront(prunePos); err != nil {
			walLog.logger.Error(fmt.Sprintf("failed to prune changelog till index %d", prunePos), "err", err)
		}
	}
}

// drain processes all pending requests so in-flight work completes before shutdown.
func (walLog *WAL[T]) drain() {
	for {
		select {
		case req := <-walLog.writeChan:
			walLog.handleWrite(req)
		case req := <-walLog.truncateChan:
			walLog.handleTruncate(req)
		default:
			return
		}
	}
}

// Shut down the WAL. Sends a close request to the main loop so in-flight writes (and other work)
// can complete before teardown. Idempotent.
func (walLog *WAL[T]) Close() error {
	_ = interuptablePush(walLog.ctx, walLog.closeReqChan, struct{}{})
	// If error is non-nil then this is not the first call to Close(), no problem since Close() is idempotent

	err := <-walLog.closeErrChan

	// "reload" error into channel to make Close() idempotent
	walLog.closeErrChan <- err

	if err != nil {
		return fmt.Errorf("error encountered while shutting down: %w", err)
	}

	return nil
}

// open opens the replay log, try to truncate the corrupted tail if there's any
func open(dir string, opts *wal.Options) (*wal.Log, error) {
	if opts == nil {
		opts = wal.DefaultOptions
	}
	rlog, err := wal.Open(dir, opts)
	if errors.Is(err, wal.ErrCorrupt) {
		// try to truncate corrupted tail
		var fis []os.DirEntry
		fis, err = os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("read wal dir fail: %w", err)
		}
		var lastSeg string
		for _, fi := range fis {
			if fi.IsDir() || len(fi.Name()) < 20 {
				continue
			}
			lastSeg = fi.Name()
		}

		if len(lastSeg) == 0 {
			return nil, err
		}
		if err = truncateCorruptedTail(filepath.Join(dir, lastSeg), opts.LogFormat); err != nil {
			return nil, fmt.Errorf("truncate corrupted tail fail: %w", err)
		}

		// try again
		return wal.Open(dir, opts)
	}
	return rlog, err
}

// The main loop doing work in the background.
func (walLog *WAL[T]) mainLoop() {

	var pruneChan <-chan time.Time
	if walLog.config.PruneInterval > 0 && walLog.config.KeepRecent > 0 {
		pruneTicker := time.NewTicker(walLog.config.PruneInterval)
		defer pruneTicker.Stop()
		pruneChan = pruneTicker.C
	}

	running := true
	for running {
		select {
		case <-walLog.ctx.Done():
			running = false
		case req := <-walLog.writeChan:
			walLog.handleWrite(req)
		case req := <-walLog.truncateChan:
			walLog.handleTruncate(req)
		case <-pruneChan:
			walLog.prune()
		case <-walLog.closeReqChan:
			running = false
		}
	}

	walLog.cancel()

	// drain pending work, then tear down
	walLog.drain()

	err := walLog.log.Close()
	if err != nil {
		walLog.closeErrChan <- fmt.Errorf("wal returned error during shutdown: %w", err)
	} else {
		walLog.closeErrChan <- nil
	}
}

// Push to a channel, returning an error if the context is cancelled before the value is pushed.
func interuptablePush[T any](ctx context.Context, ch chan T, value T) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("context cancelled: %w", ctx.Err())
	case ch <- value:
		return nil
	}
}

// Pull from a channel, returning an error if the context is cancelled before the value is pulled.
func interuptablePull[T any](ctx context.Context, ch <-chan T) (T, error) {
	var zero T
	select {
	case <-ctx.Done():
		return zero, fmt.Errorf("context cancelled: %w", ctx.Err())
	case value, ok := <-ch:
		if !ok {
			return zero, fmt.Errorf("channel closed")
		}
		return value, nil
	}
}

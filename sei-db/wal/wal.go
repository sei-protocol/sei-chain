package wal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tidwall/wal"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
)

// WAL is a generic write-ahead log implementation.
type WAL[T any] struct {
	ctx    context.Context
	cancel context.CancelFunc

	dir          string
	log          *wal.Log
	config       Config
	logger       logger.Logger
	marshal      MarshalFn[T]
	unmarshal    UnmarshalFn[T]
	writeChannel chan T
	mtx          sync.RWMutex // guards WAL state: lazy init/close of writeChannel, isClosed checks
	isClosed     bool
	// Once closed, any errors encountered during closing are written to this channel. If none were encountered, nil is written.
	closeChan chan error
	wg        sync.WaitGroup // tracks background goroutines (pruning)

	writeChan chan *writeRequest[T]
}

type Config struct {
	WriteBufferSize int
	KeepRecent      uint64
	PruneInterval   time.Duration
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
		NoSync: true,
		NoCopy: true,
	})
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(ctx)

	w := &WAL[T]{
		ctx:       ctx,
		cancel:    cancel,
		dir:       dir,
		log:       log,
		config:    config,
		logger:    logger,
		marshal:   marshal,
		unmarshal: unmarshal,
		closeChan: make(chan error, 1),
		writeChan: make(chan *writeRequest[T], config.WriteBufferSize),
	}

	go w.mainLoop()

	return w, nil

}

// A request to write to the WAL.
type writeRequest[T any] struct {
	// The data to write
	entry T
	// Errors are returned over this channel, nil is written if completed with no error
	errChan chan error
}

// Write will append a new entry to the end of the log.
// Whether the writes is in blocking or async manner depends on the buffer size.
// For async writes, this also checks for any previous async write errors.
func (walLog *WAL[T]) Write(entry T) error {

	req := &writeRequest[T]{
		entry:   entry,
		errChan: make(chan error, 1),
	}

	select {
	case _, ok := <-walLog.ctx.Done():
		if !ok {
			return fmt.Errorf("WAL is closed, cannot write")
		}
	case walLog.writeChan <- req:
		// request submitted sucessfully
	}

	select {
	case _, ok := <-walLog.ctx.Done():
		if !ok {
			return fmt.Errorf("WAL was closed after write was submitted but before write was finalized, write may or may not be durable")
		}
	case err := <-req.errChan:
		if err != nil {
			return fmt.Errorf("failed to write data: %v", err)
		}
	}

	return nil
}

// This method is called asyncronously in response to a call to Write.
func (walLog *WAL[T]) handleWrite(req *writeRequest[T]) {
	bz, err := walLog.marshal(req.entry)
	if err != nil {
		req.errChan <- fmt.Errorf("marsalling error: %v", err)
		return
	}
	lastOffset, err := walLog.log.LastIndex()
	if err != nil {
		req.errChan <- fmt.Errorf("error fetching last index: %v", err)
		return
	}
	if err := walLog.log.Write(lastOffset+1, bz); err != nil {
		req.errChan <- fmt.Errorf("failed to write: %v", err)
	}
}

// TruncateAfter will remove all entries that are after the provided `index`.
// In other words the entry at `index` becomes the last entry in the log.
func (walLog *WAL[T]) TruncateAfter(index uint64) error {
	return walLog.log.TruncateBack(index)
}

// TruncateBefore will remove all entries that are before the provided `index`.
// In other words the entry at `index` becomes the first entry in the log.
// Need to add write lock because this would change the next write offset
func (walLog *WAL[T]) TruncateBefore(index uint64) error {
	return walLog.log.TruncateFront(index)
}

func (walLog *WAL[T]) FirstOffset() (index uint64, err error) {
	return walLog.log.FirstIndex()
}

// LastOffset returns the last written offset/index of the log
func (walLog *WAL[T]) LastOffset() (index uint64, err error) {
	return walLog.log.LastIndex()
}

// ReadAt will read the log entry at the provided index
func (walLog *WAL[T]) ReadAt(index uint64) (T, error) {
	var zero T
	bz, err := walLog.log.Read(index)
	if err != nil {
		return zero, fmt.Errorf("read log failed, %w", err)
	}
	entry, err := walLog.unmarshal(bz)
	if err != nil {
		return zero, fmt.Errorf("unmarshal rlog failed, %w", err)
	}
	return entry, nil
}

// Replay will read the replay log and process each log entry with the provided function
func (walLog *WAL[T]) Replay(start uint64, end uint64, processFn func(index uint64, entry T) error) error {
	for i := start; i <= end; i++ {
		bz, err := walLog.log.Read(i)
		if err != nil {
			return fmt.Errorf("read log failed, %w", err)
		}
		entry, err := walLog.unmarshal(bz)
		if err != nil {
			return fmt.Errorf("unmarshal rlog failed, %w", err)
		}
		err = processFn(i, entry)
		if err != nil {
			return err
		}
	}
	return nil
}

func (walLog *WAL[T]) prune() {
	keepRecent := walLog.config.KeepRecent
	if keepRecent <= 0 || walLog.config.PruneInterval <= 0 {
		// pruning is disabled
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
		if err := walLog.TruncateBefore(prunePos); err != nil {
			walLog.logger.Error(fmt.Sprintf("failed to prune changelog till index %d", prunePos), "err", err)
		}
	}
}

// Shut down the WAL. This method is idempotent.
func (walLog *WAL[T]) Close() error {
	walLog.cancel()

	err := <-walLog.closeChan

	// "reload" error into channel to make Close() idempotent
	walLog.closeChan <- err

	if err != nil {
		return fmt.Errorf("error encountered while shutting down %v", err)
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

	pruneInterval := walLog.config.PruneInterval
	if pruneInterval < time.Second {
		pruneInterval = time.Second
	}
	pruneTicker := time.NewTicker(walLog.config.PruneInterval)
	defer pruneTicker.Stop()

	running := true
	for running {
		select {
		case <-walLog.ctx.Done():
			running = false
		case req := <-walLog.writeChan:
			walLog.handleWrite(req)
		case <-pruneTicker.C:
			walLog.prune()
		}
	}

	err := walLog.log.Close()
	if err != nil {
		walLog.closeChan <- fmt.Errorf("wal returned error during shutdown: %v", err)
	} else {
		walLog.closeChan <- nil
	}
}

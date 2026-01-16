package wal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tidwall/wal"

	"github.com/sei-protocol/sei-db/common/logger"
)

// WAL is a generic write-ahead log implementation.
type WAL[T any] struct {
	dir             string
	log             *wal.Log
	config          Config
	logger          logger.Logger
	marshal         MarshalFn[T]
	unmarshal       UnmarshalFn[T]
	writeChannel    chan T
	mtx             sync.RWMutex // guards WAL state: lazy init/close of writeChannel, isClosed checks
	asyncWriteErrCh chan error   // buffered=1; async writer reports first error non-blocking
	isClosed        bool
	closeCh         chan struct{}  // signals shutdown to background goroutines
	wg              sync.WaitGroup // tracks background goroutines (pruning)
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
	w := &WAL[T]{
		dir:             dir,
		log:             log,
		config:          config,
		logger:          logger,
		marshal:         marshal,
		unmarshal:       unmarshal,
		closeCh:         make(chan struct{}),
		asyncWriteErrCh: make(chan error, 1),
	}

	// Start the auto pruning goroutine
	if config.KeepRecent > 0 && config.PruneInterval > 0 {
		w.startPruning(config.KeepRecent, config.PruneInterval)
	}
	return w, nil

}

// Write will append a new entry to the end of the log.
// Whether the writes is in blocking or async manner depends on the buffer size.
// For async writes, this also checks for any previous async write errors.
func (walLog *WAL[T]) Write(entry T) error {
	// Never hold walLog.mtx while doing a potentially-blocking send. Close() may run concurrently.
	walLog.mtx.Lock()
	defer walLog.mtx.Unlock()
	if walLog.isClosed {
		return errors.New("wal is closed")
	}
	if err := walLog.getAsyncWriteErrLocked(); err != nil {
		return fmt.Errorf("async WAL write failed previously: %w", err)
	}
	writeBufferSize := walLog.config.WriteBufferSize
	if writeBufferSize > 0 {
		if walLog.writeChannel == nil {
			walLog.writeChannel = make(chan T, writeBufferSize)
			walLog.startAsyncWriteGoroutine()
			walLog.logger.Info(fmt.Sprintf("WAL async write is enabled with buffer size %d", writeBufferSize))
		}
		walLog.writeChannel <- entry
	} else {
		// synchronous write
		bz, err := walLog.marshal(entry)
		if err != nil {
			return err
		}
		lastOffset, err := walLog.log.LastIndex()
		if err != nil {
			return err
		}
		if err := walLog.log.Write(lastOffset+1, bz); err != nil {
			return err
		}
	}
	return nil
}

// startWriteGoroutine will start a goroutine to write entries to the log.
// This should only be called on initialization if async write is enabled
func (walLog *WAL[T]) startAsyncWriteGoroutine() {
	walLog.wg.Add(1)
	ch := walLog.writeChannel
	go func() {
		defer walLog.wg.Done()
		for entry := range ch {
			bz, err := walLog.marshal(entry)
			if err != nil {
				walLog.recordAsyncWriteErr(err)
				return
			}
			nextOffset, err := walLog.NextOffset()
			if err != nil {
				walLog.recordAsyncWriteErr(err)
				return
			}
			err = walLog.log.Write(nextOffset, bz)
			if err != nil {
				walLog.recordAsyncWriteErr(err)
				return
			}

		}
	}()
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

func (walLog *WAL[T]) NextOffset() (index uint64, err error) {
	lastOffset, err := walLog.log.LastIndex()
	if err != nil {
		return 0, err
	}
	return lastOffset + 1, nil
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

func (walLog *WAL[T]) startPruning(keepRecent uint64, pruneInterval time.Duration) {
	walLog.wg.Add(1)
	go func() {
		defer walLog.wg.Done()
		ticker := time.NewTicker(pruneInterval)
		defer ticker.Stop()
		for {
			select {
			case <-walLog.closeCh:
				return
			case <-ticker.C:
				lastIndex, err := walLog.log.LastIndex()
				if err != nil {
					walLog.logger.Error("failed to get last index for pruning", "err", err)
					continue
				}
				firstIndex, err := walLog.log.FirstIndex()
				if err != nil {
					walLog.logger.Error("failed to get first index for pruning", "err", err)
					continue
				}
				if lastIndex > keepRecent && (lastIndex-keepRecent) > firstIndex {
					prunePos := lastIndex - keepRecent
					if err := walLog.TruncateBefore(prunePos); err != nil {
						walLog.logger.Error(fmt.Sprintf("failed to prune changelog till index %d", prunePos), "err", err)
					}
				}
			}
		}
	}()
}

func (walLog *WAL[T]) Close() error {
	walLog.mtx.Lock()
	defer walLog.mtx.Unlock()
	// Close should only be executed once.
	if walLog.isClosed {
		return nil
	}
	// Signal background goroutines to stop.
	close(walLog.closeCh)
	if walLog.writeChannel != nil {
		close(walLog.writeChannel)
		walLog.writeChannel = nil
	}
	// Wait for all background goroutines (pruning + async write) to finish.
	walLog.wg.Wait()
	walLog.isClosed = true
	return walLog.log.Close()
}

// recordAsyncWriteErr records the first async write error (non-blocking).
func (walLog *WAL[T]) recordAsyncWriteErr(err error) {
	if err == nil {
		return
	}
	select {
	case walLog.asyncWriteErrCh <- err:
	default:
		// already recorded
	}
}

// getAsyncWriteErrLocked returns the async write error if present.
// To keep the error "sticky" without an extra cached field, we implement
// a "peek" by reading once and then non-blocking re-inserting the same
// error back into the buffered channel.
// Caller must hold walLog.mtx (read lock is sufficient).
func (walLog *WAL[T]) getAsyncWriteErrLocked() error {
	select {
	case err := <-walLog.asyncWriteErrCh:
		// Put it back so subsequent callers still observe it.
		select {
		case walLog.asyncWriteErrCh <- err:
		default:
		}
		return err
	default:
		return nil
	}
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

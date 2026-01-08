package generic_wal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/tidwall/wal"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/wal/types"
)

// WAL is a generic write-ahead log implementation.
type WAL[T any] struct {
	dir          string
	log          *wal.Log
	config       Config
	logger       logger.Logger
	marshal      types.MarshalFn[T]
	unmarshal    types.UnmarshalFn[T]
	writeChannel chan T
	errSignal    chan error
	nextOffset   uint64
	isClosed     atomic.Bool
}

type Config struct {
	DisableFsync    bool
	ZeroCopy        bool
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
	marshal types.MarshalFn[T],
	unmarshal types.UnmarshalFn[T],
	logger logger.Logger,
	dir string,
	config Config,
) (*WAL[T], error) {
	log, err := open(dir, &wal.Options{
		NoSync: config.DisableFsync,
		NoCopy: config.ZeroCopy,
	})
	if err != nil {
		return nil, err
	}
	w := &WAL[T]{
		dir:       dir,
		log:       log,
		config:    config,
		logger:    logger,
		marshal:   marshal,
		unmarshal: unmarshal,
		// isClosed is zero-initialized to false (atomic.Bool)
	}
	// Finding the nextOffset to write
	lastIndex, err := log.LastIndex()
	if err != nil {
		return nil, err
	}
	w.nextOffset = lastIndex + 1
	// Start the auto pruning goroutine
	if config.KeepRecent > 0 {
		go w.StartPruning(config.KeepRecent, config.PruneInterval)
	}
	return w, nil

}

// Write will append a new entry to the end of the log.
// Whether the writes is in blocking or async manner depends on the buffer size.
func (walLog *WAL[T]) Write(entry T) error {
	channelBufferSize := walLog.config.WriteBufferSize
	if channelBufferSize > 0 {
		if walLog.writeChannel == nil {
			walLog.logger.Info(fmt.Sprintf("async write is enabled with buffer size %d", channelBufferSize))
			walLog.startWriteGoroutine()
		}
		// async write
		walLog.writeChannel <- entry
		walLog.nextOffset++
	} else {
		// synchronous write
		bz, err := walLog.marshal(entry)
		if err != nil {
			return err
		}
		if err := walLog.log.Write(walLog.nextOffset, bz); err != nil {
			return err
		}
		walLog.nextOffset++
	}
	return nil
}

// startWriteGoroutine will start a goroutine to write entries to the log.
// This should only be called on initialization if async write is enabled
func (walLog *WAL[T]) startWriteGoroutine() {
	walLog.writeChannel = make(chan T, walLog.config.WriteBufferSize)
	walLog.errSignal = make(chan error)
	// Capture the starting offset for the goroutine
	writeOffset := walLog.nextOffset
	go func() {
		batch := wal.Batch{}
		defer close(walLog.errSignal)
		for {
			entries := channelBatchRecv(walLog.writeChannel)
			if len(entries) == 0 {
				// channel is closed
				break
			}

			for _, entry := range entries {
				bz, err := walLog.marshal(entry)
				if err != nil {
					walLog.errSignal <- err
					return
				}
				batch.Write(writeOffset, bz)
				writeOffset++
			}

			if err := walLog.log.WriteBatch(&batch); err != nil {
				walLog.errSignal <- err
				return
			}
			batch.Clear()
		}
	}()
}

// TruncateAfter will remove all entries that are after the provided `index`.
// In other words the entry at `index` becomes the last entry in the log.
func (walLog *WAL[T]) TruncateAfter(index uint64) error {
	if err := walLog.log.TruncateBack(index); err != nil {
		return err
	}
	// Update nextOffset to reflect the new end of log
	walLog.nextOffset = index + 1
	return nil
}

// TruncateBefore will remove all entries that are before the provided `index`.
// In other words the entry at `index` becomes the first entry in the log.
func (walLog *WAL[T]) TruncateBefore(index uint64) error {
	return walLog.log.TruncateFront(index)
}

// CheckError check if there's any failed async writes or not
func (walLog *WAL[T]) CheckError() error {
	select {
	case err := <-walLog.errSignal:
		// async wal writing failed, we need to abort the state machine
		return fmt.Errorf("async wal writing goroutine quit unexpectedly: %w", err)
	default:
	}
	return nil
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

func (walLog *WAL[T]) StartPruning(keepRecent uint64, pruneInterval time.Duration) {
	for !walLog.isClosed.Load() {
		lastIndex, _ := walLog.log.LastIndex()
		firstIndex, _ := walLog.log.FirstIndex()
		if lastIndex > keepRecent && (lastIndex-keepRecent) > firstIndex {
			prunePos := lastIndex - keepRecent
			if err := walLog.TruncateBefore(prunePos); err != nil {
				walLog.logger.Error(fmt.Sprintf("failed to prune changelog till index %d", prunePos), "err", err)
			}
		}
		time.Sleep(pruneInterval)
	}
}

func (walLog *WAL[T]) Close() error {
	walLog.isClosed.Store(true)
	var err error
	if walLog.writeChannel != nil {
		close(walLog.writeChannel)
		err = <-walLog.errSignal
		walLog.writeChannel = nil
		walLog.errSignal = nil
	}
	errClose := walLog.log.Close()
	return errorutils.Join(err, errClose)
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

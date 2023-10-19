package rlog

import (
	"fmt"
	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/tidwall/wal"
)

type RLWriter struct {
	rlog         *wal.Log
	config       Config
	logger       logger.Logger
	writeChannel chan *LogEntry
	stopSignal   chan error
}

var (
	_ Writer = (*RLWriter)(nil)
)

func NewWriter(logger logger.Logger, rlog *wal.Log, config Config) (*RLWriter, error) {
	writer := &RLWriter{rlog: rlog, logger: logger, config: config}

	return writer, nil
}

// Write will write a new entry appending the tail of the log.
// Whether the writes is in blocking or async manner depends on the buffer size.
func (writer *RLWriter) Write(entry LogEntry) error {
	channelBufferSize := writer.config.WriteBufferSize
	if channelBufferSize > 0 {
		if writer.writeChannel == nil {
			writer.logger.Info(fmt.Sprintf("async write is enabled with buffer size %d", channelBufferSize))
			writer.writeChannel = make(chan *LogEntry, writer.config.WriteBufferSize)
			writer.stopSignal = make(chan error)
			go writer.startWriteGoroutine()
		}
		// async write
		writer.writeChannel <- &entry
	} else {
		// synchronous write
		bz, err := entry.Data.Marshal()
		if err != nil {
			return err
		}
		if err := writer.rlog.Write(entry.Index, bz); err != nil {
			return err
		}
	}
	return nil
}

// startWriteGoroutine will start a goroutine to write entries to the log.
// This should only be called on initialization if async write is enabled
func (writer *RLWriter) startWriteGoroutine() {
	batch := wal.Batch{}
	defer close(writer.stopSignal)
	for {
		entries := channelBatchRecv(writer.writeChannel)
		if len(entries) == 0 {
			// channel is closed
			break
		}

		for _, entry := range entries {
			bz, err := entry.Data.Marshal()
			if err != nil {
				writer.stopSignal <- err
				return
			}
			batch.Write(entry.Index, bz)
		}

		if err := writer.rlog.WriteBatch(&batch); err != nil {
			writer.stopSignal <- err
			return
		}
		batch.Clear()
	}
}

func channelBatchRecv[T any](ch <-chan *T) []*T {
	// block if channel is empty
	item := <-ch
	if item == nil {
		// channel is closed
		return nil
	}

	remaining := len(ch)
	result := make([]*T, 0, remaining+1)
	result = append(result, item)
	for i := 0; i < remaining; i++ {
		result = append(result, <-ch)
	}
	return result
}

// TruncateAfter will remove all entries that are after the provided `index`.
// In other words the entry at `index` becomes the last entry in the log.
func (writer *RLWriter) TruncateAfter(index uint64) error {
	return writer.rlog.TruncateBack(index)
}

// TruncateBefore will remove all entries that are before the provided `index`.
// In other words the entry at `index` becomes the first entry in the log.
func (writer *RLWriter) TruncateBefore(index uint64) error {
	return writer.rlog.TruncateFront(index)
}

// CheckAsyncCommit check the quit signal of async rlog writing
func (writer *RLWriter) CheckAsyncCommit() error {
	select {
	case err := <-writer.stopSignal:
		// async wal writing failed, we need to abort the state machine
		return fmt.Errorf("async wal writing goroutine quit unexpectedly: %w", err)
	default:
	}
	return nil
}

// WaitAsyncCommit will block and wait for async writes to complete
func (writer *RLWriter) WaitAsyncCommit() error {
	if writer.writeChannel == nil {
		return nil
	}
	close(writer.writeChannel)
	err := <-writer.stopSignal
	writer.writeChannel = nil
	writer.stopSignal = nil
	return err
}

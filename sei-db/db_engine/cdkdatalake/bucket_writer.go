package cdkdatalake

import (
	"context"
	"fmt"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/datalake"
)

var _ objectDeleter = (*bucketWriter)(nil)

// bucketWriter writes and deletes objects in a store's part of the bucket, retrying transient failures. It is not
// goroutine safe, but the results it returns are.
type bucketWriter struct {
	// The store's part of the bucket.
	storeBucket *storeBucket

	// The store's configuration.
	config DataLakeConfig

	// Writes waiting for a writer goroutine. Each is a *putWrite, *streamPutWrite, *deleteWrite or *metadataWrite.
	queue chan any

	// The context of every write. Cancelled by Close().
	ctx context.Context

	// Cancels ctx.
	cancel context.CancelFunc

	// Tracks the writer goroutines.
	goroutineCount sync.WaitGroup

	// Tracks writes submitted and not finished.
	unfinished sync.WaitGroup

	// Holds the first failure of any write.
	firstFailure chan error

	// Receives a value when a write finishes, unless it already holds one.
	finished chan struct{}
}

// newBucketWriter returns a writer whose writes run under ctx, with config.WriterCount goroutines and room for
// config.WorkBufferSize writes waiting for one.
func newBucketWriter(ctx context.Context, storeBucket *storeBucket, config DataLakeConfig) *bucketWriter {
	ctx, cancel := context.WithCancel(ctx)
	w := &bucketWriter{
		storeBucket:  storeBucket,
		config:       config,
		queue:        make(chan any, config.WorkBufferSize),
		ctx:          ctx,
		cancel:       cancel,
		firstFailure: make(chan error, 1),
		finished:     make(chan struct{}, 1),
	}
	for range config.WriterCount {
		w.goroutineCount.Add(1)
		go func() {
			defer w.goroutineCount.Done()
			w.writerGoroutine()
		}()
	}
	return w
}

// Put writes value at key.
func (w *bucketWriter) Put(key uint64, value []byte) *writeResult {
	result := newWriteResult(key)
	w.submit(&putWrite{key: key, value: value, result: result})
	return result
}

// PutStream writes the bytes read from stream at key. The writer closes stream when the write finishes.
func (w *bucketWriter) PutStream(key uint64, stream datalake.DataLakeStream) *writeResult {
	result := newWriteResult(key)
	w.submit(&streamPutWrite{key: key, stream: stream, result: result})
	return result
}

// Delete deletes key. Deleting an absent key succeeds.
func (w *bucketWriter) Delete(key uint64) *writeResult {
	result := newWriteResult(key)
	w.submit(&deleteWrite{key: key, result: result})
	return result
}

// WriteMetadata replaces the content of the store's metadata object with data.
func (w *bucketWriter) WriteMetadata(data []byte) *writeResult {
	result := newWriteResult(0)
	w.submit(&metadataWrite{data: data, result: result})
	return result
}

// Flush waits for every write submitted so far to finish. It returns the first failure of any write the writer has
// run.
func (w *bucketWriter) Flush() error {
	w.unfinished.Wait()
	select {
	case err := <-w.firstFailure:
		// Put it back so that later calls report it too.
		w.firstFailure <- err
		return fmt.Errorf("a write failed: %w", err)
	default:
		return nil
	}
}

// Finished returns a channel that receives a value after writes finish. Several writes finishing may produce a
// single value.
func (w *bucketWriter) Finished() <-chan struct{} {
	return w.finished
}

// Close stops the writer. Writes that have not finished fail, and their streams are closed. No write may be
// submitted during or after the call.
func (w *bucketWriter) Close() {
	w.cancel()
	close(w.queue)
	w.goroutineCount.Wait()
}

// submit queues write for a writer goroutine, waiting while the queue is full.
func (w *bucketWriter) submit(write any) {
	w.unfinished.Add(1)
	w.queue <- write
}

// writerGoroutine finishes queued writes until the writer is closed, failing without performing them once the
// writer's context ends.
func (w *bucketWriter) writerGoroutine() {
	for write := range w.queue {
		if w.ctx.Err() != nil {
			// Failing the write rather than leaving it queued lets every wait on it return.
			w.abandon(write)
		} else {
			w.perform(write)
		}
	}
}

// perform performs write, which must be one of the write types the queue carries.
func (w *bucketWriter) perform(write any) {
	switch write := write.(type) {
	case *putWrite:
		w.finish(write.result, w.performPut(write))
	case *streamPutWrite:
		err := w.performStreamPut(write)
		_ = write.stream.Close()
		w.finish(write.result, err)
	case *deleteWrite:
		w.finish(write.result, w.performDelete(write))
	case *metadataWrite:
		w.finish(write.result, w.performMetadata(write))
	default:
		panic(fmt.Sprintf("bucket writer received a write of unknown type %T", write))
	}
}

// abandon fails write without performing it, which must be one of the write types the queue carries.
func (w *bucketWriter) abandon(write any) {
	switch write := write.(type) {
	case *putWrite:
		w.finish(write.result, fmt.Errorf("put of key %d abandoned: %w", write.key, w.ctx.Err()))
	case *streamPutWrite:
		_ = write.stream.Close()
		w.finish(write.result, fmt.Errorf("stream put of key %d abandoned: %w", write.key, w.ctx.Err()))
	case *deleteWrite:
		w.finish(write.result, fmt.Errorf("delete of key %d abandoned: %w", write.key, w.ctx.Err()))
	case *metadataWrite:
		w.finish(write.result, fmt.Errorf("metadata write abandoned: %w", w.ctx.Err()))
	default:
		panic(fmt.Sprintf("bucket writer received a write of unknown type %T", write))
	}
}

// performPut writes write.value at write.key.
func (w *bucketWriter) performPut(write *putWrite) error {
	for retries := 0; ; retries++ {
		err := w.storeBucket.Put(w.ctx, write.key, write.value)
		retry, failure := w.retryAfter(fmt.Sprintf("put of key %d", write.key), err, retries)
		if !retry {
			return failure
		}
	}
}

// performStreamPut writes the bytes read from write.stream at write.key. A failure of the stream itself is not
// retried.
func (w *bucketWriter) performStreamPut(write *streamPutWrite) error {
	for retries := 0; ; retries++ {
		streamFailed, err := w.storeBucket.PutStream(w.ctx, write.key, write.stream, retries > 0)
		description := fmt.Sprintf("stream put of key %d", write.key)
		if streamFailed {
			return fmt.Errorf("%s failed after %d retries: %w", description, retries, err)
		}
		retry, failure := w.retryAfter(description, err, retries)
		if !retry {
			return failure
		}
	}
}

// performDelete deletes write.key.
func (w *bucketWriter) performDelete(write *deleteWrite) error {
	for retries := 0; ; retries++ {
		err := w.storeBucket.Delete(w.ctx, write.key)
		retry, failure := w.retryAfter(fmt.Sprintf("delete of key %d", write.key), err, retries)
		if !retry {
			return failure
		}
	}
}

// performMetadata replaces the content of the store's metadata object with write.data.
func (w *bucketWriter) performMetadata(write *metadataWrite) error {
	for retries := 0; ; retries++ {
		err := w.storeBucket.WriteMetadata(w.ctx, write.data)
		retry, failure := w.retryAfter("metadata write", err, retries)
		if !retry {
			return failure
		}
	}
}

// retryAfter decides whether to attempt a write again after an attempt that returned err, sleeping the backoff
// first if so.
func (w *bucketWriter) retryAfter(
	// Describes the write for error messages.
	description string,
	// The attempt's error, or nil if it succeeded.
	err error,
	// The number of times the write has already been retried.
	retries int,
) (
	// True if the write should be attempted again.
	retry bool,
	// The write's failure, if it should not be attempted again. Nil if err is nil.
	failure error,
) {
	if err == nil {
		return false, nil
	}
	if w.ctx.Err() != nil {
		return false, fmt.Errorf("%s abandoned: %w", description, err)
	}
	if !isRetryableCDKError(err) || !w.config.mayRetry(retries) {
		return false, fmt.Errorf("%s failed after %d retries: %w", description, retries, err)
	}
	if !sleepBackoff(w.ctx, w.config) {
		return false, fmt.Errorf("%s abandoned while retrying: %w", description, err)
	}
	return true, nil
}

// finish completes result with err, recording err if it is the writer's first failure.
func (w *bucketWriter) finish(result *writeResult, err error) {
	if err != nil {
		select {
		case w.firstFailure <- err:
		default:
		}
	}
	result.complete(err)
	w.unfinished.Done()
	select {
	case w.finished <- struct{}{}:
	default:
	}
}

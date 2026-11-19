package cdkdatalake

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"gocloud.dev/blob"
)

// bucketReader reads a store's part of the bucket, retrying transient failures. It is goroutine safe.
type bucketReader struct {
	// The store's part of the bucket.
	storeBucket *storeBucket

	// The store's configuration.
	config DataLakeConfig

	// Hands reads to idle reader goroutines. Each is a *getRead, *streamRead, *hasRead, *metadataRead or
	// *keyPageRead.
	queue chan any

	// Cancelled by Close().
	ctx context.Context

	// Cancels ctx.
	cancel context.CancelFunc

	// Tracks the reader goroutines.
	goroutineCount sync.WaitGroup
}

// newBucketReader returns a reader with config.ReaderCount goroutines, which stops when ctx ends or Close() is
// called.
func newBucketReader(ctx context.Context, storeBucket *storeBucket, config DataLakeConfig) *bucketReader {
	ctx, cancel := context.WithCancel(ctx)
	r := &bucketReader{
		storeBucket: storeBucket,
		config:      config,
		queue:       make(chan any),
		ctx:         ctx,
		cancel:      cancel,
	}
	for range config.ReaderCount {
		r.goroutineCount.Add(1)
		go func() {
			defer r.goroutineCount.Done()
			r.readerGoroutine()
		}()
	}
	return r
}

// Get starts reading the value at key.
func (r *bucketReader) Get(ctx context.Context, key uint64) *getResult {
	result := newGetResult(key)
	if err := r.submit(ctx, &getRead{key: key, result: result}); err != nil {
		result.complete(nil, false, fmt.Errorf("read of key %d abandoned: %w", key, err))
	}
	return result
}

// GetStream opens a reader over the value at key. The caller must close it.
func (r *bucketReader) GetStream(
	ctx context.Context,
	key uint64,
) (
	value io.ReadCloser,
	// False if the key's object does not exist.
	found bool,
	err error,
) {
	read := &streamRead{key: key, done: make(chan struct{})}
	if err := r.submit(ctx, read); err != nil {
		return nil, false, fmt.Errorf("stream read of key %d abandoned: %w", key, err)
	}
	// A stream discarded here would never be closed, so wait for the read even if ctx ends.
	<-read.done
	return read.stream, read.found, read.err
}

// Has reports whether the object holding key exists.
func (r *bucketReader) Has(ctx context.Context, key uint64) (bool, error) {
	read := &hasRead{key: key, done: make(chan struct{})}
	if err := r.submit(ctx, read); err != nil {
		return false, fmt.Errorf("existence check of key %d abandoned: %w", key, err)
	}
	if err := waitForRead(ctx, read.done); err != nil {
		return false, fmt.Errorf("existence check of key %d: %w", key, err)
	}
	return read.found, read.err
}

// ReadMetadata returns the content of the store's metadata object.
func (r *bucketReader) ReadMetadata(
	ctx context.Context,
) (
	data []byte,
	// False if the metadata object does not exist.
	found bool,
	err error,
) {
	read := &metadataRead{done: make(chan struct{})}
	if err := r.submit(ctx, read); err != nil {
		return nil, false, fmt.Errorf("metadata read abandoned: %w", err)
	}
	if err := waitForRead(ctx, read.done); err != nil {
		return nil, false, fmt.Errorf("metadata read: %w", err)
	}
	return read.data, read.found, read.err
}

// Iterator returns an iterator over the store's keys, starting at startKey or the next key after it in the direction
// of iteration.
func (r *bucketReader) Iterator(
	// If true, the iterator also visits orphaned keys. An orphaned key has an object in the store's part of the
	// bucket that Get() would not return: the key was deleted, pruned or rolled back before its object was
	// removed, or was written at or above the durable watermark before a crash. This option is only legal for
	// reverse-ordered iteration.
	includeOrphans bool,
	// Ignored if includeOrphans is true.
	topology keyTopology,
	startKey uint64,
	reverse bool,
	// The number of values to fetch ahead, or 0 for none.
	prefetchDepth int,
) (*storeIterator, error) {
	if !includeOrphans {
		if !topology.IsComplete() {
			return nil, errors.New("the topology does not say which keys the store holds")
		}
		nextKey := keySequence(topology, startKey, reverse)
		return newStoreIterator(r, func(context.Context) (uint64, bool, error) {
			key, ok := nextKey()
			return key, ok, nil
		}, prefetchDepth), nil
	}
	if !reverse {
		return nil, errors.New("including orphaned keys is only legal for reverse-ordered iteration")
	}
	return newStoreIterator(r, r.listedKeys(startKey), prefetchDepth), nil
}

// Close stops the reader. Reads that have not finished fail.
func (r *bucketReader) Close() {
	r.cancel()
	r.goroutineCount.Wait()
}

// listedKeys returns a function yielding every key that has an object in the store's part of the bucket, from
// startKey or the next key below it down to the lowest.
func (r *bucketReader) listedKeys(startKey uint64) func(ctx context.Context) (uint64, bool, error) {
	pageToken := blob.FirstPageToken
	var page []uint64
	return func(ctx context.Context) (uint64, bool, error) {
		for {
			for len(page) > 0 {
				key := page[0]
				page = page[1:]
				if key <= startKey {
					return key, true, nil
				}
			}
			if len(pageToken) == 0 {
				return 0, false, nil
			}
			keys, nextPageToken, err := r.readKeyPage(ctx, pageToken)
			if err != nil {
				return 0, false, fmt.Errorf("list keys: %w", err)
			}
			page = keys
			pageToken = nextPageToken
		}
	}
}

// readKeyPage lists the page of keys named by pageToken, from highest to lowest.
func (r *bucketReader) readKeyPage(
	ctx context.Context,
	pageToken []byte,
) (
	keys []uint64,
	// The token naming the next page, or empty if this page was the last.
	nextPageToken []byte,
	err error,
) {
	read := &keyPageRead{pageToken: pageToken, done: make(chan struct{})}
	if err := r.submit(ctx, read); err != nil {
		return nil, nil, fmt.Errorf("key listing abandoned: %w", err)
	}
	if err := waitForRead(ctx, read.done); err != nil {
		return nil, nil, fmt.Errorf("key listing: %w", err)
	}
	if read.err != nil {
		return nil, nil, read.err
	}
	keys = make([]uint64, 0, len(read.names))
	for _, name := range read.names {
		key, err := parseInverseKey(name)
		if err != nil {
			return nil, nil, fmt.Errorf("parse object name %q: %w", name, err)
		}
		keys = append(keys, key)
	}
	return keys, read.nextPageToken, nil
}

// submit hands read to a reader goroutine, waiting until one is idle.
func (r *bucketReader) submit(ctx context.Context, read any) error {
	select {
	case r.queue <- read:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for an idle reader: %w", ctx.Err())
	case <-r.ctx.Done():
		return fmt.Errorf("reader is closed: %w", r.ctx.Err())
	}
}

// readerGoroutine performs reads until the reader is closed.
func (r *bucketReader) readerGoroutine() {
	for {
		select {
		case <-r.ctx.Done():
			return
		case read := <-r.queue:
			r.perform(read)
		}
	}
}

// perform performs read, which must be one of the read types the queue carries.
func (r *bucketReader) perform(read any) {
	switch read := read.(type) {
	case *getRead:
		r.performGet(read)
	case *streamRead:
		r.performStream(read)
	case *hasRead:
		r.performHas(read)
	case *metadataRead:
		r.performMetadata(read)
	case *keyPageRead:
		r.performKeyPage(read)
	default:
		panic(fmt.Sprintf("bucket reader received a read of unknown type %T", read))
	}
}

// performGet reads the value at read.key.
func (r *bucketReader) performGet(read *getRead) {
	for retries := 0; ; retries++ {
		value, found, err := r.storeBucket.Get(r.ctx, read.key)
		retry, failure := r.retryAfter(fmt.Sprintf("read of key %d", read.key), err, retries)
		if !retry {
			read.result.complete(value, found, failure)
			return
		}
	}
}

// performStream opens a reader over the value at read.key.
func (r *bucketReader) performStream(read *streamRead) {
	defer close(read.done)
	for retries := 0; ; retries++ {
		var err error
		read.stream, read.found, err = r.storeBucket.GetStream(r.ctx, read.key)
		retry, failure := r.retryAfter(fmt.Sprintf("stream read of key %d", read.key), err, retries)
		if !retry {
			read.err = failure
			return
		}
	}
}

// performHas checks whether the object holding read.key exists.
func (r *bucketReader) performHas(read *hasRead) {
	defer close(read.done)
	for retries := 0; ; retries++ {
		var err error
		read.found, err = r.storeBucket.Has(r.ctx, read.key)
		description := fmt.Sprintf("existence check of key %d", read.key)
		retry, failure := r.retryAfter(description, err, retries)
		if !retry {
			read.err = failure
			return
		}
	}
}

// performMetadata reads the store's metadata object.
func (r *bucketReader) performMetadata(read *metadataRead) {
	defer close(read.done)
	for retries := 0; ; retries++ {
		var err error
		read.data, read.found, err = r.storeBucket.ReadMetadata(r.ctx)
		retry, failure := r.retryAfter("metadata read", err, retries)
		if !retry {
			read.err = failure
			return
		}
	}
}

// performKeyPage lists the page of the store's data objects named by read.pageToken.
func (r *bucketReader) performKeyPage(read *keyPageRead) {
	defer close(read.done)
	for retries := 0; ; retries++ {
		var err error
		read.names, read.nextPageToken, err = r.storeBucket.ListPage(r.ctx, read.pageToken)
		retry, failure := r.retryAfter("key listing", err, retries)
		if !retry {
			read.err = failure
			return
		}
	}
}

// retryAfter decides whether to attempt a read again after an attempt that returned err, sleeping the backoff
// first if so.
func (r *bucketReader) retryAfter(
	// Describes the read for error messages.
	description string,
	// The attempt's error, or nil if it succeeded.
	err error,
	// The number of times the read has already been retried.
	retries int,
) (
	// True if the read should be attempted again.
	retry bool,
	// The read's failure, if it should not be attempted again. Nil if err is nil.
	failure error,
) {
	if err == nil {
		return false, nil
	}
	if r.ctx.Err() != nil {
		return false, fmt.Errorf("%s abandoned: %w", description, err)
	}
	if !isRetryableCDKError(err) || !r.config.mayRetry(retries) {
		return false, fmt.Errorf("%s failed after %d retries: %w", description, retries, err)
	}
	if !sleepBackoff(r.ctx, r.config) {
		return false, fmt.Errorf("%s abandoned while retrying: %w", description, err)
	}
	return true, nil
}

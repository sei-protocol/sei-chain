package datalake

import (
	"context"
	"errors"
	"fmt"
	"io"

	"gocloud.dev/blob"

	"github.com/sei-protocol/sei-chain/sei-db/common/structures"

	// Importing a driver registers its bucket URL scheme. blob.OpenBucket() rejects a BucketURL whose scheme no
	// imported driver has registered, so this list is the set of supported schemes: file://, mem:// and s3://.
	// Supporting gs:// (GCS) or azblob:// (Azure) needs the matching import uncommented, which adds that cloud's
	// SDK as a dependency.
	_ "gocloud.dev/blob/fileblob"
	_ "gocloud.dev/blob/memblob"
	_ "gocloud.dev/blob/s3blob"
	// _ "gocloud.dev/blob/azureblob"
	// _ "gocloud.dev/blob/gcsblob"
)

var _ DataLake = (*cdkDataLake)(nil)

// cdkDataLake is a DataLake backed by a Go CDK blob bucket. Its DataLake methods run on their callers' goroutines and
// hand every change to the store's control loop, a goroutine that owns the fields documented as used only by it.
type cdkDataLake struct {
	// The bucket named by the config, owned by the store.
	bucket *blob.Bucket

	// Performs the store's writes and deletes.
	writer *bucketWriter

	// Performs the store's reads.
	reader *bucketReader

	// The store's configuration.
	config DataLakeConfig

	// Whether the store allows gaps between its keys.
	allowGaps bool

	// Requests for the control loop. Each is a *putRequest, *deleteRequest, *pruneRequest, *flushRequest,
	// *lookupRequest, *boundsRequest or *iteratorRequest.
	requests chan any

	// Closed when the control loop exits.
	done chan struct{}

	// Stops the control loop and the writer.
	cancel context.CancelFunc

	// The error that failed the store, or nil if it has not failed. Used only by the control loop.
	failure error

	// One above the highest key in the store. Never less than the scheduled deletion watermark. Used only by the
	// control loop.
	nextKey uint64

	// In a store that allows gaps, its keys in ascending order. Empty in a store without gaps. The deleter removes
	// the keys it schedules for deletion. Used only by the control loop.
	keys *structures.SortedMap[uint64, struct{}]

	// Decides when the objects of deleted keys are deleted, and tracks those deletes. Used only by the control
	// loop.
	deleter deleter

	// Puts submitted to the writer and not yet complete, in key order. Used only by the control loop.
	putOperations *structures.Deque[*writeResult]

	// Writes the store's metadata and tracks which metadata is durable. Used only by the control loop.
	metadataManager *metadataManager
}

// NewCDKDataLake opens the store named storeName in the bucket named by config, creating it on its first write.
// Before returning, it finishes an interrupted rollback and repairs the top of the key range left by a crash.
func NewCDKDataLake(
	ctx context.Context,
	// Names the store within the bucket. Must match ^[a-zA-Z0-9_-]+$.
	storeName string,
	// If true, keys may be skipped.
	allowGaps bool,
	config DataLakeConfig,
) (DataLake, error) {
	lake, err := newCDKDataLake(ctx, storeName, allowGaps, config)
	if err != nil {
		return nil, fmt.Errorf("open data lake: %w", err)
	}
	return lake, nil
}

// newCDKDataLake opens a store as NewCDKDataLake() does, and starts its control loop.
func newCDKDataLake(
	ctx context.Context,
	// Names the store within the bucket. Must match ^[a-zA-Z0-9_-]+$.
	storeName string,
	// If true, keys may be skipped.
	allowGaps bool,
	config DataLakeConfig,
) (lake *cdkDataLake, err error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	if !storeNameRegex.MatchString(storeName) {
		return nil, fmt.Errorf("invalid store name %q, must match %s", storeName, storeNameRegex)
	}

	bucket, err := blob.OpenBucket(ctx, config.BucketURL)
	if err != nil {
		return nil, fmt.Errorf("open bucket %q: %w", config.BucketURL, err)
	}
	defer func() {
		if err != nil {
			if closeErr := bucket.Close(); closeErr != nil {
				err = errors.Join(err, fmt.Errorf("close bucket: %w", closeErr))
			}
		}
	}()

	storeBucket := newStoreBucket(bucket, storeName)
	reader := newBucketReader(context.Background(), storeBucket, config)
	defer func() {
		if err != nil {
			reader.Close()
		}
	}()

	// Recovery's writes run under ctx, so that ending ctx stops them.
	recoveryWriter := newBucketWriter(ctx, storeBucket, config)
	defer recoveryWriter.Close()
	state, err := recoverState(ctx, reader, recoveryWriter, allowGaps)
	if err != nil {
		return nil, fmt.Errorf("open store %q: %w", storeName, err)
	}

	if state.hasMetadata {
		metadata := state.topology.Metadata()
		if err := recoveryWriter.WriteMetadata(metadata.Serialize()).Wait(); err != nil {
			return nil, fmt.Errorf("write repaired metadata for store %q: %w", storeName, err)
		}
	}
	metadata := state.topology.Metadata()
	keys := structures.NewSortedMap[uint64, struct{}]()
	for _, key := range state.topology.Keys() {
		keys.Put(key, struct{}{})
	}
	var deleter deleter
	if metadata.AllowGaps() {
		deleter = newGappedDeleter(keys, metadata, state.danglingKeys)
	} else {
		deleter = newContiguousDeleter(metadata)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	writer := newBucketWriter(runCtx, storeBucket, config)
	lake = &cdkDataLake{
		bucket:        bucket,
		writer:        writer,
		reader:        reader,
		config:        config,
		allowGaps:     metadata.AllowGaps(),
		requests:      make(chan any, config.RequestBufferSize),
		done:          make(chan struct{}),
		cancel:        cancel,
		nextKey:       metadata.DurableWatermark(),
		keys:          keys,
		deleter:       deleter,
		putOperations: structures.NewDeque[*writeResult](),
		metadataManager: newMetadataManager(
			writer, deleter, config.MetadataWriteInterval, metadata, state.hasMetadata),
	}
	go lake.run(runCtx)
	return lake, nil
}

// Put writes value at key. See DataLake.
func (s *cdkDataLake) Put(ctx context.Context, key uint64, value []byte) error {
	if err := s.submitPut(ctx, &putRequest{key: key, value: value, reply: make(chan error, 1)}); err != nil {
		return fmt.Errorf("put key %d: %w", key, err)
	}
	return nil
}

// PutStream writes the bytes read from value at key. See DataLake.
func (s *cdkDataLake) PutStream(ctx context.Context, key uint64, value DataLakeStream) error {
	if err := s.submitPut(ctx, &putRequest{key: key, stream: value, reply: make(chan error, 1)}); err != nil {
		return fmt.Errorf("put key %d: %w", key, err)
	}
	return nil
}

// Get returns the value at key. See DataLake.
func (s *cdkDataLake) Get(ctx context.Context, key uint64) ([]byte, bool, error) {
	present, err := s.lookup(ctx, key)
	if err != nil {
		return nil, false, fmt.Errorf("get key %d: %w", key, err)
	}
	if !present {
		return nil, false, nil
	}
	value, ok, err := s.reader.Get(ctx, key).Value(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("get key %d: %w", key, err)
	}
	return value, ok, nil
}

// GetStream returns a reader over the value at key. See DataLake.
func (s *cdkDataLake) GetStream(ctx context.Context, key uint64) (io.ReadCloser, bool, error) {
	present, err := s.lookup(ctx, key)
	if err != nil {
		return nil, false, fmt.Errorf("get key %d: %w", key, err)
	}
	if !present {
		return nil, false, nil
	}
	value, ok, err := s.reader.GetStream(ctx, key)
	if err != nil {
		return nil, false, fmt.Errorf("get stream of key %d: %w", key, err)
	}
	return value, ok, nil
}

// Has reports whether key exists. See DataLake.
func (s *cdkDataLake) Has(ctx context.Context, key uint64) (bool, error) {
	present, err := s.lookup(ctx, key)
	if err != nil {
		return false, fmt.Errorf("has key %d: %w", key, err)
	}
	if !present {
		return false, nil
	}
	exists, err := s.reader.Has(ctx, key)
	if err != nil {
		return false, fmt.Errorf("has key %d: %w", key, err)
	}
	return exists, nil
}

// Delete removes key. See DataLake. ctx bounds only the wait to queue the request.
func (s *cdkDataLake) Delete(ctx context.Context, key uint64) error {
	request := &deleteRequest{key: key, reply: make(chan error, 1)}
	if err := s.send(ctx, request); err != nil {
		return fmt.Errorf("delete key %d: %w", key, err)
	}
	// A queued delete runs whatever ctx does, so the caller waits for its outcome.
	outcome, err := awaitReply(context.Background(), s.done, request.reply)
	if err != nil {
		return fmt.Errorf("delete key %d: %w", key, err)
	}
	if outcome != nil {
		return fmt.Errorf("delete key %d: %w", key, outcome)
	}
	return nil
}

// PruneBelow removes every key below key. See DataLake. ctx bounds only the wait to queue the request.
func (s *cdkDataLake) PruneBelow(ctx context.Context, key uint64) error {
	request := &pruneRequest{key: key, reply: make(chan error, 1)}
	if err := s.send(ctx, request); err != nil {
		return fmt.Errorf("prune below key %d: %w", key, err)
	}
	// A queued prune runs whatever ctx does, so the caller waits for its outcome.
	outcome, err := awaitReply(context.Background(), s.done, request.reply)
	if err != nil {
		return fmt.Errorf("prune below key %d: %w", key, err)
	}
	if outcome != nil {
		return fmt.Errorf("prune below key %d: %w", key, outcome)
	}
	return nil
}

// GetDurableWatermark returns the durable watermark. See DataLake.
func (s *cdkDataLake) GetDurableWatermark() uint64 {
	return s.bounds().watermark
}

// GetFirstKey returns the lowest key in the store. See DataLake.
func (s *cdkDataLake) GetFirstKey() (uint64, bool) {
	bounds := s.bounds()
	return bounds.firstKey, bounds.firstKeyOK
}

// Flush blocks until every write accepted before the call is crash durable and its changes to the backing store are
// complete. See DataLake.
func (s *cdkDataLake) Flush(ctx context.Context) error {
	request := &flushRequest{reply: make(chan error, 1)}
	if err := s.send(ctx, request); err != nil {
		return fmt.Errorf("flush: %w", err)
	}
	outcome, err := awaitReply(ctx, s.done, request.reply)
	if err != nil {
		return fmt.Errorf("flush: %w", err)
	}
	if outcome != nil {
		return fmt.Errorf("flush: %w", outcome)
	}
	return nil
}

// Iterator returns an iterator over the keys below the durable watermark. See DataLake.
func (s *cdkDataLake) Iterator(
	ctx context.Context,
	startKey uint64,
	prefetchValues bool,
	reverse bool,
) (DataLakeIterator, error) {
	request := &iteratorRequest{reply: make(chan keyTopologyResult, 1)}
	if err := s.send(ctx, request); err != nil {
		return nil, fmt.Errorf("create iterator: %w", err)
	}
	result, err := awaitReply(ctx, s.done, request.reply)
	if err != nil {
		return nil, fmt.Errorf("create iterator: %w", err)
	}
	if result.err != nil {
		return nil, fmt.Errorf("create iterator: %w", result.err)
	}
	prefetchDepth := 0
	if prefetchValues {
		prefetchDepth = s.config.PrefetchDepth
	}
	iterator, err := s.reader.Iterator(false, result.topology, startKey, reverse, prefetchDepth)
	if err != nil {
		return nil, fmt.Errorf("create iterator: %w", err)
	}
	return iterator, nil
}

// Close stops the store's goroutines and closes its bucket. Writes that are not yet crash durable may be lost.
func (s *cdkDataLake) Close() error {
	s.cancel()
	<-s.done
	s.writer.Close()
	s.reader.Close()
	if err := s.bucket.Close(); err != nil {
		return fmt.Errorf("close bucket: %w", err)
	}
	return nil
}

// submitPut hands request to the control loop and returns its outcome. ctx bounds only the wait to queue the
// request. It closes request.stream if the control loop never handles the put.
func (s *cdkDataLake) submitPut(ctx context.Context, request *putRequest) error {
	if err := s.send(ctx, request); err != nil {
		if request.stream != nil {
			_ = request.stream.Close()
		}
		return fmt.Errorf("queue the put: %w", err)
	}
	// A queued put runs whatever ctx does, so the caller waits for its outcome.
	outcome, err := awaitReply(context.Background(), s.done, request.reply)
	if err != nil {
		if request.stream != nil {
			_ = request.stream.Close()
		}
		return fmt.Errorf("wait for the put: %w", err)
	}
	if outcome != nil {
		return fmt.Errorf("put rejected: %w", outcome)
	}
	return nil
}

// lookup reports whether key has been accepted and not deleted.
func (s *cdkDataLake) lookup(ctx context.Context, key uint64) (bool, error) {
	request := &lookupRequest{key: key, reply: make(chan lookupResult, 1)}
	if err := s.send(ctx, request); err != nil {
		return false, fmt.Errorf("look up key %d: %w", key, err)
	}
	result, err := awaitReply(ctx, s.done, request.reply)
	if err != nil {
		return false, fmt.Errorf("look up key %d: %w", key, err)
	}
	if result.err != nil {
		return false, fmt.Errorf("data lake has failed: %w", result.err)
	}
	return result.present, nil
}

// bounds returns the durable watermark and the first key, or zero bounds once the control loop has exited.
func (s *cdkDataLake) bounds() storeBounds {
	request := &boundsRequest{reply: make(chan storeBounds, 1)}
	if err := s.send(context.Background(), request); err != nil {
		return storeBounds{}
	}
	bounds, err := awaitReply(context.Background(), s.done, request.reply)
	if err != nil {
		return storeBounds{}
	}
	return bounds
}

// send queues request for the control loop. It returns an error if the loop has exited or ctx ends first.
func (s *cdkDataLake) send(ctx context.Context, request any) error {
	select {
	case s.requests <- request:
		return nil
	case <-s.done:
		return errors.New("data lake is closed")
	case <-ctx.Done():
		return fmt.Errorf("wait for the control loop: %w", ctx.Err())
	}
}

// awaitReply waits for the control loop's reply to a request it was sent. It returns an error if ctx ends first, or
// if the loop exits without handling the request.
func awaitReply[T any](
	ctx context.Context,
	// Closed when the control loop exits.
	done <-chan struct{},
	reply <-chan T,
) (T, error) {
	var zero T
	select {
	case value := <-reply:
		return value, nil
	case <-ctx.Done():
		return zero, fmt.Errorf("wait for the control loop: %w", ctx.Err())
	case <-done:
		select {
		case value := <-reply:
			// The control loop replies to every request it handles before it exits.
			return value, nil
		default:
			return zero, errors.New("data lake is closed")
		}
	}
}

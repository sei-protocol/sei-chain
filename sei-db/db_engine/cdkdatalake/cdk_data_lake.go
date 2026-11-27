package cdkdatalake

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/sei-protocol/seilog"
	"gocloud.dev/blob"

	"github.com/sei-protocol/sei-chain/sei-db/common/structures"
	"github.com/sei-protocol/sei-chain/sei-db/datalake"

	// Importing a driver registers its bucket URL scheme. blob.OpenBucket() rejects a BucketURL whose scheme no
	// imported driver has registered, so this list is the set of supported schemes: file://, mem://, r2:// and
	// s3://. Supporting gs:// (GCS) or azblob:// (Azure) needs the matching import uncommented, which adds that
	// cloud's SDK as a dependency.
	_ "github.com/sei-protocol/sei-chain/sei-db/db_engine/cdkdatalake/r2blob"
	_ "gocloud.dev/blob/fileblob"
	_ "gocloud.dev/blob/memblob"
	_ "gocloud.dev/blob/s3blob"
	// _ "gocloud.dev/blob/azureblob"
	// _ "gocloud.dev/blob/gcsblob"
)

var logger = seilog.NewLogger("db", "db-engine", "cdkdatalake")

var _ datalake.DataLake = (*cdkDataLake)(nil)

// cdkDataLake is a datalake.DataLake backed by a Go CDK blob bucket. Its datalake.DataLake methods run on their
// callers' goroutines and hand every change to the store's control loop, a goroutine that owns the fields documented
// as used only by it.
type cdkDataLake struct {
	// The bucket named by the config. The control loop closes it when it exits.
	bucket *blob.Bucket

	// Performs the store's writes and deletes. Used only by the control loop, which stops it when it exits.
	writer *bucketWriter

	// Performs the store's reads. The control loop stops it when it exits.
	reader *bucketReader

	// The store's configuration.
	config DataLakeConfig

	// Whether the store allows gaps between its keys.
	allowGaps bool

	// Requests for the control loop. Each is a *putRequest, *deleteRequest, *pruneRequest, *flushRequest,
	// *closeRequest, *lookupRequest, *boundsRequest or *iteratorRequest.
	requests chan any

	// Closed when the control loop exits.
	done chan struct{}

	// The error that stopped the control loop, or nil. Set before done is closed, and read only after.
	failure error

	// Why closing the store failed, or nil: the failure of the flush Close() requests, or of closing the bucket.
	// Set before done is closed, and read only after.
	closeErr error

	// One above the highest key in the store. Never less than the scheduled deletion watermark. Used only by the
	// control loop.
	nextKey uint64

	// The store's scheduled deletion watermark: every key below it is deleted. Used only by the control loop.
	scheduledDeletionWatermark uint64

	// The lowest key in the store. Meaningless while the store is empty. Used only by the control loop.
	firstUnprunedKey uint64

	// In a store that allows gaps, its keys in ascending order, along with deleted keys the deleter has not yet
	// removed. Empty in a store without gaps. Used only by the control loop.
	keys *structures.SortedMap[uint64, struct{}]

	// Decides when the objects of deleted keys are deleted, and tracks those deletes. Used only by the control
	// loop.
	deleter deleter

	// Puts submitted to the writer and not yet complete, in key order. Used only by the control loop.
	putOperations *structures.Deque[*writeResult]

	// Writes the store's metadata and tracks which metadata is durable. Used only by the control loop.
	metadataManager *metadataManager
}

// NewCDKDataLake opens the store named storeName in the bucket named by config. Before returning, it finishes an
// interrupted rollback and repairs the top of the key range left by a crash.
func NewCDKDataLake(
	ctx context.Context,
	// Names the store within the bucket. Must match ^[a-zA-Z0-9_-]+$.
	storeName string,
	// If true, keys may be skipped. Must match the value the store was first opened with.
	allowGaps bool,
	config DataLakeConfig,
) (datalake.DataLake, error) {
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
	// If true, keys may be skipped. Must match the value the store was first opened with.
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

	metadata := state.topology.Metadata()
	if !metadata.IsNewStore() {
		if err := recoveryWriter.WriteMetadata(metadata.Serialize()).Wait(); err != nil {
			return nil, fmt.Errorf("write repaired metadata for store %q: %w", storeName, err)
		}
	}
	keys := structures.NewSortedMap[uint64, struct{}]()
	for _, key := range state.topology.Keys() {
		keys.Put(key, struct{}{})
	}
	firstUnprunedKey := metadata.ScheduledDeletionWatermark()
	if key, _, ok := keys.PeekFront(); ok {
		firstUnprunedKey = key
	}
	writer := newBucketWriter(context.Background(), storeBucket, config)
	var deleter deleter
	if metadata.AllowGaps() {
		deleter = newGappedDeleter(writer, keys, metadata, state.danglingKeys)
	} else {
		deleter = newContiguousDeleter(writer, metadata)
	}
	lake = &cdkDataLake{
		bucket:                     bucket,
		writer:                     writer,
		reader:                     reader,
		config:                     config,
		allowGaps:                  metadata.AllowGaps(),
		requests:                   make(chan any, config.RequestBufferSize),
		done:                       make(chan struct{}),
		nextKey:                    metadata.DurableWatermark(),
		scheduledDeletionWatermark: metadata.ScheduledDeletionWatermark(),
		firstUnprunedKey:           firstUnprunedKey,
		keys:                       keys,
		deleter:                    deleter,
		putOperations:              structures.NewDeque[*writeResult](),
		metadataManager: newMetadataManager(
			writer, deleter, config.MetadataWriteInterval, metadata),
	}
	go lake.run()
	return lake, nil
}

// Put writes value at key. See datalake.DataLake.
func (s *cdkDataLake) Put(ctx context.Context, key uint64, value []byte) error {
	if err := s.submitPut(ctx, &putRequest{key: key, value: value, reply: make(chan error, 1)}); err != nil {
		return fmt.Errorf("put key %d: %w", key, err)
	}
	return nil
}

// PutStream writes the bytes read from value at key. See datalake.DataLake.
func (s *cdkDataLake) PutStream(ctx context.Context, key uint64, value datalake.DataLakeStream) error {
	if err := s.submitPut(ctx, &putRequest{key: key, stream: value, reply: make(chan error, 1)}); err != nil {
		return fmt.Errorf("put key %d: %w", key, err)
	}
	return nil
}

// Get returns the value at key. See datalake.DataLake.
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

// GetStream returns a reader over the value at key. See datalake.DataLake.
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

// Has reports whether key exists. See datalake.DataLake.
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

// Delete removes key. See datalake.DataLake.
func (s *cdkDataLake) Delete(ctx context.Context, key uint64) error {
	request := &deleteRequest{key: key, reply: make(chan error, 1)}
	if err := s.send(ctx, request); err != nil {
		return fmt.Errorf("delete key %d: %w", key, err)
	}
	outcome, err := awaitReply(ctx, s, request.reply)
	if err != nil {
		return fmt.Errorf("delete key %d: %w", key, err)
	}
	if outcome != nil {
		return fmt.Errorf("delete key %d: %w", key, outcome)
	}
	return nil
}

// PruneBelow removes every key below key. See datalake.DataLake.
func (s *cdkDataLake) PruneBelow(ctx context.Context, key uint64) error {
	request := &pruneRequest{key: key, reply: make(chan error, 1)}
	if err := s.send(ctx, request); err != nil {
		return fmt.Errorf("prune below key %d: %w", key, err)
	}
	outcome, err := awaitReply(ctx, s, request.reply)
	if err != nil {
		return fmt.Errorf("prune below key %d: %w", key, err)
	}
	if outcome != nil {
		return fmt.Errorf("prune below key %d: %w", key, outcome)
	}
	return nil
}

// GetDurableWatermark returns the durable watermark. See datalake.DataLake.
func (s *cdkDataLake) GetDurableWatermark() uint64 {
	return s.bounds().watermark
}

// GetDeletionWatermark returns the lowest key whose object may still exist. See datalake.DataLake.
func (s *cdkDataLake) GetDeletionWatermark() uint64 {
	return s.bounds().deletionWatermark
}

// GetFirstKey returns the lowest key in the store. See datalake.DataLake.
func (s *cdkDataLake) GetFirstKey() (uint64, bool) {
	bounds := s.bounds()
	return bounds.firstKey, bounds.firstKeyOK
}

// Flush blocks until every write accepted before the call is crash durable and its changes to the backing store are
// complete. See datalake.DataLake.
func (s *cdkDataLake) Flush(ctx context.Context) error {
	request := &flushRequest{reply: make(chan struct{}, 1)}
	if err := s.send(ctx, request); err != nil {
		return fmt.Errorf("flush: %w", err)
	}
	if _, err := awaitReply(ctx, s, request.reply); err != nil {
		return fmt.Errorf("flush: %w", err)
	}
	return nil
}

// Iterator returns an iterator over the keys below the durable watermark. See datalake.DataLake.
func (s *cdkDataLake) Iterator(
	ctx context.Context,
	startKey uint64,
	prefetchValues bool,
	reverse bool,
) (datalake.DataLakeIterator, error) {
	request := &iteratorRequest{reply: make(chan keyTopology, 1)}
	if err := s.send(ctx, request); err != nil {
		return nil, fmt.Errorf("create iterator: %w", err)
	}
	topology, err := awaitReply(ctx, s, request.reply)
	if err != nil {
		return nil, fmt.Errorf("create iterator: %w", err)
	}
	prefetchDepth := 0
	if prefetchValues {
		prefetchDepth = s.config.PrefetchDepth
	}
	iterator, err := s.reader.Iterator(false, topology, startKey, reverse, prefetchDepth)
	if err != nil {
		return nil, fmt.Errorf("create iterator: %w", err)
	}
	return iterator, nil
}

// Close makes every accepted write crash durable, then stops the store's goroutines and closes its bucket. A store
// that has failed is not flushed. Close may be called more than once, including concurrently; every call waits for
// the store to close and returns the same outcome.
func (s *cdkDataLake) Close() error {
	// The send fails only once the control loop has exited, which the wait below covers.
	_ = s.send(context.Background(), &closeRequest{})
	<-s.done
	return s.closeErr
}

// submitPut hands request to the control loop and returns its outcome. It closes request.stream if the put is never
// queued, or if the control loop stops without handling it.
func (s *cdkDataLake) submitPut(ctx context.Context, request *putRequest) error {
	if err := s.send(ctx, request); err != nil {
		if request.stream != nil {
			_ = request.stream.Close()
		}
		return fmt.Errorf("queue the put: %w", err)
	}
	outcome, err := awaitReply(ctx, s, request.reply)
	if err != nil {
		s.closeUnhandledStream(request)
		return fmt.Errorf("wait for the put: %w", err)
	}
	if outcome != nil {
		return fmt.Errorf("put rejected: %w", outcome)
	}
	return nil
}

// closeUnhandledStream closes the stream of a queued put if the control loop has stopped without handling it. Once
// the loop handles a put, the loop or the writer closes its stream.
func (s *cdkDataLake) closeUnhandledStream(request *putRequest) {
	if request.stream == nil {
		return
	}
	select {
	case <-s.done:
	default:
		// The loop may still handle the put.
		return
	}
	select {
	case <-request.reply:
		// The loop replied, so it handled the put.
	default:
		_ = request.stream.Close()
	}
}

// lookup reports whether key has been accepted and not deleted.
func (s *cdkDataLake) lookup(ctx context.Context, key uint64) (bool, error) {
	request := &lookupRequest{key: key, reply: make(chan bool, 1)}
	if err := s.send(ctx, request); err != nil {
		return false, fmt.Errorf("look up key %d: %w", key, err)
	}
	present, err := awaitReply(ctx, s, request.reply)
	if err != nil {
		return false, fmt.Errorf("look up key %d: %w", key, err)
	}
	return present, nil
}

// bounds returns the durable watermark and the first key, or zero bounds once the control loop has exited.
func (s *cdkDataLake) bounds() storeBounds {
	request := &boundsRequest{reply: make(chan storeBounds, 1)}
	if err := s.send(context.Background(), request); err != nil {
		return storeBounds{}
	}
	bounds, err := awaitReply(context.Background(), s, request.reply)
	if err != nil {
		return storeBounds{}
	}
	return bounds
}

// send queues request for the control loop.
func (s *cdkDataLake) send(ctx context.Context, request any) error {
	select {
	case s.requests <- request:
		return nil
	case <-s.done:
		return s.stoppedError()
	case <-ctx.Done():
		return fmt.Errorf("wait for the control loop: %w", ctx.Err())
	}
}

// stoppedError describes why the control loop has exited. It may be called only once done is closed.
func (s *cdkDataLake) stoppedError() error {
	if s.failure != nil {
		return fmt.Errorf("data lake has failed: %w", s.failure)
	}
	return errors.New("data lake is closed")
}

// awaitReply waits for the control loop's reply to a request it was sent.
func awaitReply[T any](
	ctx context.Context,
	// The data lake whose control loop was sent the request.
	lake *cdkDataLake,
	reply <-chan T,
) (T, error) {
	var zero T
	select {
	case value := <-reply:
		return value, nil
	case <-ctx.Done():
		return zero, fmt.Errorf("wait for the control loop: %w", ctx.Err())
	case <-lake.done:
		select {
		case value := <-reply:
			// The control loop may reply to a request and then exit.
			return value, nil
		default:
			return zero, lake.stoppedError()
		}
	}
}

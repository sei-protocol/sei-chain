package cdkdatalake

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/datalake"
	"gocloud.dev/blob"
	"gocloud.dev/gcerrors"
)

// storeNameRegex matches the store names NewCDKDataLake() accepts.
var storeNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// inverseKeyLength is the number of decimal digits in an encoded inverse key.
const inverseKeyLength = 20

// metadataObjectName is the name of the metadata object under a store's prefix.
const metadataObjectName = "metadata"

// listPageSize is the number of objects requested per page when listing a store's objects.
const listPageSize = 1000

// contentType is the content type of every object a data lake writes.
const contentType = "application/octet-stream"

// storeBucket is one store's part of a bucket, read and written by uint64 key. A key is stored under the store's
// prefix as its inverse, math.MaxUint64 minus the key, written as 20 decimal digits, so that listing the prefix
// visits keys from highest to lowest.
type storeBucket struct {
	// The bucket holding the objects.
	bucket *blob.Bucket

	// The store name followed by "/".
	prefix string
}

// newStoreBucket returns the part of bucket holding the store named storeName.
func newStoreBucket(bucket *blob.Bucket, storeName string) *storeBucket {
	return &storeBucket{
		bucket: bucket,
		prefix: storeName + "/",
	}
}

// dataObjectKey returns the name of the object holding key.
func (o *storeBucket) dataObjectKey(key uint64) string {
	return fmt.Sprintf("%s%0*d", o.prefix, inverseKeyLength, math.MaxUint64-key)
}

// metadataObjectKey returns the name of the store's metadata object.
func (o *storeBucket) metadataObjectKey() string {
	return o.prefix + metadataObjectName
}

// Put writes value as the object holding key.
func (o *storeBucket) Put(ctx context.Context, key uint64, value []byte) error {
	options := &blob.WriterOptions{ContentType: contentType}
	if err := o.bucket.WriteAll(ctx, o.dataObjectKey(key), value, options); err != nil {
		return fmt.Errorf("write key %d: %w", key, err)
	}
	return nil
}

// PutStream writes the bytes read from stream as the object holding key. A failed attempt leaves no object behind.
// It does not close stream.
func (o *storeBucket) PutStream(
	ctx context.Context,
	key uint64,
	stream datalake.DataLakeStream,
	// If true, stream is reset to its first byte before it is read.
	reset bool,
) (
	// True if the failure came from stream rather than from the bucket.
	streamFailed bool,
	err error,
) {
	if reset {
		if err := stream.Reset(); err != nil {
			return true, fmt.Errorf("reset stream for key %d: %w", key, err)
		}
	}

	// Cancelling the writer's context is how a CDK write is aborted.
	writeCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	options := &blob.WriterOptions{ContentType: contentType}
	writer, err := o.bucket.NewWriter(writeCtx, o.dataObjectKey(key), options)
	if err != nil {
		return false, fmt.Errorf("open writer for key %d: %w", key, err)
	}
	source := &streamSource{stream: stream}
	if _, err := io.Copy(writer, source); err != nil {
		cancel()
		_ = writer.Close()
		return source.err != nil, fmt.Errorf("stream key %d: %w", key, err)
	}
	if err := writer.Close(); err != nil {
		return false, fmt.Errorf("close writer for key %d: %w", key, err)
	}
	return false, nil
}

// streamSource reads a datalake.DataLakeStream and records the first error it returns other than io.EOF.
type streamSource struct {
	// The stream read.
	stream datalake.DataLakeStream

	// The stream's first error other than io.EOF.
	err error
}

// Read reads from the stream.
func (s *streamSource) Read(p []byte) (int, error) {
	n, err := s.stream.Read(p)
	if err != nil && !errors.Is(err, io.EOF) && s.err == nil {
		s.err = err
	}
	return n, err
}

// Delete deletes the object holding key. Deleting an absent object succeeds.
func (o *storeBucket) Delete(ctx context.Context, key uint64) error {
	err := o.bucket.Delete(ctx, o.dataObjectKey(key))
	if err != nil && gcerrors.Code(err) != gcerrors.NotFound {
		return fmt.Errorf("delete key %d: %w", key, err)
	}
	return nil
}

// Get reads the object holding key.
func (o *storeBucket) Get(
	ctx context.Context,
	key uint64,
) (
	value []byte,
	// False if the object does not exist.
	ok bool,
	err error,
) {
	value, err = o.bucket.ReadAll(ctx, o.dataObjectKey(key))
	if gcerrors.Code(err) == gcerrors.NotFound {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read key %d: %w", key, err)
	}
	return value, true, nil
}

// GetStream opens a reader over the object holding key.
func (o *storeBucket) GetStream(
	ctx context.Context,
	key uint64,
) (
	value io.ReadCloser,
	// False if the object does not exist.
	ok bool,
	err error,
) {
	reader, err := o.bucket.NewReader(ctx, o.dataObjectKey(key), nil)
	if gcerrors.Code(err) == gcerrors.NotFound {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("open reader for key %d: %w", key, err)
	}
	return reader, true, nil
}

// Has reports whether the object holding key exists.
func (o *storeBucket) Has(ctx context.Context, key uint64) (bool, error) {
	exists, err := o.bucket.Exists(ctx, o.dataObjectKey(key))
	if err != nil {
		return false, fmt.Errorf("check key %d: %w", key, err)
	}
	return exists, nil
}

// ReadMetadata returns the content of the store's metadata object.
func (o *storeBucket) ReadMetadata(
	ctx context.Context,
) (
	data []byte,
	// False if the metadata object does not exist.
	ok bool,
	err error,
) {
	data, err = o.bucket.ReadAll(ctx, o.metadataObjectKey())
	if gcerrors.Code(err) == gcerrors.NotFound {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read metadata: %w", err)
	}
	return data, true, nil
}

// WriteMetadata replaces the content of the store's metadata object with data.
func (o *storeBucket) WriteMetadata(ctx context.Context, data []byte) error {
	options := &blob.WriterOptions{ContentType: contentType}
	if err := o.bucket.WriteAll(ctx, o.metadataObjectKey(), data, options); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	return nil
}

// ListPage lists the page of the store's data objects named by pageToken, from highest key to lowest. It returns the
// name of each object under the store's prefix.
func (o *storeBucket) ListPage(
	ctx context.Context,
	// blob.FirstPageToken, or the token returned with the previous page.
	pageToken []byte,
) (
	names []string,
	// The token naming the next page, or empty if this page was the last.
	nextPageToken []byte,
	err error,
) {
	options := &blob.ListOptions{Prefix: o.prefix}
	objects, nextPageToken, err := o.bucket.ListPage(ctx, pageToken, listPageSize, options)
	if err != nil {
		return nil, nil, fmt.Errorf("list objects: %w", err)
	}
	names = make([]string, 0, len(objects))
	for _, object := range objects {
		name := strings.TrimPrefix(object.Key, o.prefix)
		if name != metadataObjectName {
			names = append(names, name)
		}
	}
	return names, nextPageToken, nil
}

// parseInverseKey returns the key whose inverse is encoded in name.
func parseInverseKey(name string) (uint64, error) {
	if len(name) != inverseKeyLength {
		return 0, fmt.Errorf("inverse key %q is not %d digits", name, inverseKeyLength)
	}
	inverse, err := strconv.ParseUint(name, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("inverse key %q: %w", name, err)
	}
	return math.MaxUint64 - inverse, nil
}

// isRetryableCDKError reports whether a failed backing store operation may succeed if attempted again.
func isRetryableCDKError(err error) bool {
	switch gcerrors.Code(err) {
	case gcerrors.Unknown, gcerrors.Internal, gcerrors.ResourceExhausted, gcerrors.DeadlineExceeded:
		return true
	default:
		return false
	}
}

// sleepBackoff waits config.RetryBackoff. It returns false if ctx ends first.
func sleepBackoff(ctx context.Context, config DataLakeConfig) bool {
	timer := time.NewTimer(config.RetryBackoff)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

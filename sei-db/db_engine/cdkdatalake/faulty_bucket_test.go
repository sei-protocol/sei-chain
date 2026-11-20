package cdkdatalake

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"gocloud.dev/blob"
	"gocloud.dev/blob/driver"
	"gocloud.dev/blob/memblob"
	"gocloud.dev/gcerrors"
)

// faultyBucketScheme is the URL scheme under which tests open faulty buckets. A URL's host names the bucket.
const faultyBucketScheme = "faulty"

// faultyBuckets holds the faulty buckets tests can open by URL, keyed by name.
var faultyBuckets sync.Map

// faultyBucketCount numbers faulty buckets so that each gets a unique name.
var faultyBucketCount atomic.Uint64

var _ driver.Bucket = (*faultyBucket)(nil)

// faultyBucket is a blob driver that forwards to an in-memory bucket, after first consulting a fault hook that may
// fail or block the operation.
type faultyBucket struct {
	// The bucket every operation is forwarded to.
	inner *blob.Bucket

	// Guards hook.
	lock sync.Mutex

	// Called before each operation with its name ("attributes", "list", "read", "write", "commit", "delete") and
	// object key, where "commit" is the close that completes a write. A non-nil error is returned in place of
	// performing the operation, and a failed commit discards the write.
	hook func(ctx context.Context, op string, key string) error
}

// injectedError is an error returned by a faultyBucket hook, with the CDK error code it reports.
type injectedError struct {
	// The code the driver reports for the error.
	code gcerrors.ErrorCode
}

// faultyBucketURLOpener opens the faulty bucket a URL names.
type faultyBucketURLOpener struct{}

func init() {
	blob.DefaultURLMux().RegisterBucket(faultyBucketScheme, faultyBucketURLOpener{})
}

// newFaultyBucket returns a faultyBucket over a fresh in-memory bucket, and a test configuration that opens it.
func newFaultyBucket(t *testing.T) (*faultyBucket, DataLakeConfig) {
	faulty := &faultyBucket{inner: memblob.OpenBucket(nil)}
	name := fmt.Sprintf("bucket%d", faultyBucketCount.Add(1))
	faultyBuckets.Store(name, faulty)
	t.Cleanup(func() {
		faultyBuckets.Delete(name)
		require.NoError(t, faulty.inner.Close())
	})
	config := DefaultTestDataLakeConfig(t.TempDir())
	config.BucketURL = (&url.URL{Scheme: faultyBucketScheme, Host: name}).String()
	return faulty, config
}

// OpenBucketURL opens the faulty bucket named by the host of u.
func (faultyBucketURLOpener) OpenBucketURL(_ context.Context, u *url.URL) (*blob.Bucket, error) {
	faulty, ok := faultyBuckets.Load(u.Host)
	if !ok {
		return nil, fmt.Errorf("no faulty bucket named %q", u.Host)
	}
	return blob.NewBucket(faulty.(*faultyBucket)), nil
}

// Error describes the injected error.
func (e *injectedError) Error() string {
	return fmt.Sprintf("injected %s error", e.code)
}

// SetHook replaces the fault hook. A nil hook injects nothing.
func (b *faultyBucket) SetHook(hook func(ctx context.Context, op string, key string) error) {
	b.lock.Lock()
	defer b.lock.Unlock()
	b.hook = hook
}

// fault runs the hook for an operation.
func (b *faultyBucket) fault(ctx context.Context, op string, key string) error {
	b.lock.Lock()
	hook := b.hook
	b.lock.Unlock()
	if hook == nil {
		return nil
	}
	return hook(ctx, op, key)
}

func (b *faultyBucket) ErrorCode(err error) gcerrors.ErrorCode {
	var injected *injectedError
	if errors.As(err, &injected) {
		return injected.code
	}
	return gcerrors.Code(err)
}

func (b *faultyBucket) As(any) bool {
	return false
}

func (b *faultyBucket) ErrorAs(error, any) bool {
	return false
}

func (b *faultyBucket) Attributes(ctx context.Context, key string) (*driver.Attributes, error) {
	if err := b.fault(ctx, "attributes", key); err != nil {
		return nil, fmt.Errorf("attributes of %q: %w", key, err)
	}
	attributes, err := b.inner.Attributes(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("attributes of %q: %w", key, err)
	}
	return &driver.Attributes{
		ContentType: attributes.ContentType,
		Metadata:    attributes.Metadata,
		CreateTime:  attributes.CreateTime,
		ModTime:     attributes.ModTime,
		Size:        attributes.Size,
		MD5:         attributes.MD5,
		ETag:        attributes.ETag,
	}, nil
}

func (b *faultyBucket) ListPaged(ctx context.Context, opts *driver.ListOptions) (*driver.ListPage, error) {
	if err := b.fault(ctx, "list", opts.Prefix); err != nil {
		return nil, fmt.Errorf("list %q: %w", opts.Prefix, err)
	}
	token := opts.PageToken
	if len(token) == 0 {
		token = blob.FirstPageToken
	}
	pageSize := opts.PageSize
	if pageSize <= 0 {
		pageSize = 1000
	}
	listOptions := &blob.ListOptions{Prefix: opts.Prefix, Delimiter: opts.Delimiter}
	objects, nextToken, err := b.inner.ListPage(ctx, token, pageSize, listOptions)
	if errors.Is(err, io.EOF) {
		return &driver.ListPage{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list %q: %w", opts.Prefix, err)
	}
	page := &driver.ListPage{NextPageToken: nextToken}
	for _, object := range objects {
		page.Objects = append(page.Objects, &driver.ListObject{
			Key:     object.Key,
			ModTime: object.ModTime,
			Size:    object.Size,
			MD5:     object.MD5,
			IsDir:   object.IsDir,
		})
	}
	return page, nil
}

func (b *faultyBucket) NewRangeReader(
	ctx context.Context,
	key string,
	offset int64,
	length int64,
	_ *driver.ReaderOptions,
) (driver.Reader, error) {
	if err := b.fault(ctx, "read", key); err != nil {
		return nil, fmt.Errorf("read %q: %w", key, err)
	}
	reader, err := b.inner.NewRangeReader(ctx, key, offset, length, nil)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", key, err)
	}
	return &faultyBucketReader{reader: reader}, nil
}

func (b *faultyBucket) NewTypedWriter(
	ctx context.Context,
	key string,
	contentType string,
	_ *driver.WriterOptions,
) (driver.Writer, error) {
	if err := b.fault(ctx, "write", key); err != nil {
		return nil, fmt.Errorf("write %q: %w", key, err)
	}
	// Cancelling the inner writer's context is how a failed commit discards the write.
	writeCtx, cancel := context.WithCancel(ctx)
	writer, err := b.inner.NewWriter(writeCtx, key, &blob.WriterOptions{ContentType: contentType})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("write %q: %w", key, err)
	}
	return &faultyBucketWriter{
		writer: writer,
		cancel: cancel,
		commit: func() error { return b.fault(ctx, "commit", key) },
	}, nil
}

func (b *faultyBucket) Copy(ctx context.Context, dstKey string, srcKey string, _ *driver.CopyOptions) error {
	if err := b.inner.Copy(ctx, dstKey, srcKey, nil); err != nil {
		return fmt.Errorf("copy %q to %q: %w", srcKey, dstKey, err)
	}
	return nil
}

func (b *faultyBucket) Delete(ctx context.Context, key string, _ *driver.DeleteOptions) error {
	if err := b.fault(ctx, "delete", key); err != nil {
		return fmt.Errorf("delete %q: %w", key, err)
	}
	if err := b.inner.Delete(ctx, key); err != nil {
		return fmt.Errorf("delete %q: %w", key, err)
	}
	return nil
}

func (b *faultyBucket) SignedURL(context.Context, string, *driver.SignedURLOptions) (string, error) {
	return "", &injectedError{code: gcerrors.Unimplemented}
}

func (b *faultyBucket) Close() error {
	return nil
}

// faultyBucketReader adapts a portable reader to the driver interface.
type faultyBucketReader struct {
	// The reader adapted.
	reader *blob.Reader
}

func (r *faultyBucketReader) Read(p []byte) (int, error) {
	return r.reader.Read(p)
}

func (r *faultyBucketReader) Close() error {
	return r.reader.Close()
}

func (r *faultyBucketReader) Attributes() *driver.ReaderAttributes {
	return &driver.ReaderAttributes{
		ContentType: r.reader.ContentType(),
		ModTime:     r.reader.ModTime(),
		Size:        r.reader.Size(),
	}
}

func (r *faultyBucketReader) As(any) bool {
	return false
}

// faultyBucketWriter adapts a portable writer to the driver interface, consulting the fault hook before committing.
type faultyBucketWriter struct {
	// The writer adapted.
	writer *blob.Writer

	// Cancels the adapted writer's context.
	cancel context.CancelFunc

	// Runs the fault hook for the commit.
	commit func() error
}

func (w *faultyBucketWriter) Write(p []byte) (int, error) {
	return w.writer.Write(p)
}

func (w *faultyBucketWriter) Close() error {
	defer w.cancel()
	if err := w.commit(); err != nil {
		w.cancel()
		_ = w.writer.Close()
		return fmt.Errorf("commit: %w", err)
	}
	if err := w.writer.Close(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

package datalake

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gocloud.dev/gcerrors"
)

func TestBucketWriterPutsDeletesAndWritesMetadata(t *testing.T) {
	ctx := context.Background()
	config := DefaultTestDataLakeConfig(t.TempDir())
	storeBucket := openTestStoreBucket(t, config, "store")
	writer := newBucketWriter(ctx, storeBucket, config)
	defer writer.Close()

	var puts []*writeResult
	for key := uint64(1); key <= 20; key++ {
		puts = append(puts, writer.Put(key, testValue(key)))
	}
	metadata := testStoreMetadata(t, false, 1, 1, 21)
	metadataWrite := writer.WriteMetadata(metadata.Serialize())
	require.NoError(t, writer.Flush())
	for _, put := range puts {
		err, done := put.Check()
		require.True(t, done)
		require.NoError(t, err)
	}
	require.NoError(t, metadataWrite.Wait())
	require.Equal(t, metadata, readMetadata(t, storeBucket))

	for key := uint64(1); key <= 10; key++ {
		writer.Delete(key)
	}
	require.NoError(t, writer.Flush())
	for key := uint64(1); key <= 20; key++ {
		requireObjectExists(t, storeBucket, key, key > 10)
	}
	value, ok, err := storeBucket.Get(ctx, 15)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, testValue(15), value)
}

func TestBucketWriterRetriesTransientFailures(t *testing.T) {
	faulty, config := newFaultyBucket(t)
	failures := 0
	faulty.SetHook(func(_ context.Context, op string, _ string) error {
		if op == "write" && failures < 3 {
			failures++
			return &injectedError{code: gcerrors.Unknown}
		}
		return nil
	})
	config.WriterCount = 1
	storeBucket := openTestStoreBucket(t, config, "store")
	writer := newBucketWriter(context.Background(), storeBucket, config)
	defer writer.Close()

	require.NoError(t, writer.Put(1, testValue(1)).Wait())
	require.NoError(t, writer.Flush())
	require.Equal(t, 3, failures)
	requireObjectExists(t, storeBucket, 1, true)
}

func TestBucketWriterReportsPermanentFailures(t *testing.T) {
	faulty, config := newFaultyBucket(t)
	faulty.SetHook(func(_ context.Context, op string, _ string) error {
		if op == "write" {
			return &injectedError{code: gcerrors.PermissionDenied}
		}
		return nil
	})
	storeBucket := openTestStoreBucket(t, config, "store")
	writer := newBucketWriter(context.Background(), storeBucket, config)
	defer writer.Close()

	err := writer.Put(1, testValue(1)).Wait()
	require.ErrorContains(t, err, "PermissionDenied")
	require.ErrorContains(t, err, "put of key 1 failed after 0 retries")
	require.ErrorContains(t, writer.Flush(), "PermissionDenied")
	require.ErrorContains(t, writer.Flush(), "PermissionDenied", "the failure is reported again")
}

func TestBucketWriterRestartsStreamsOnRetry(t *testing.T) {
	faulty, config := newFaultyBucket(t)
	failures := 0
	faulty.SetHook(func(_ context.Context, op string, _ string) error {
		if op == "commit" && failures < 2 {
			failures++
			return &injectedError{code: gcerrors.Unknown}
		}
		return nil
	})
	storeBucket := openTestStoreBucket(t, config, "store")
	writer := newBucketWriter(context.Background(), storeBucket, config)
	defer writer.Close()

	value := []byte(strings.Repeat("snapshot", 1000))
	stream := newTestStream(value)
	require.NoError(t, writer.PutStream(1, stream).Wait())
	require.Equal(t, 2, stream.resets)
	require.True(t, stream.closed)
	stored, ok, err := storeBucket.Get(context.Background(), 1)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, value, stored)
}

func TestBucketWriterDoesNotRetryStreamFailures(t *testing.T) {
	config := DefaultTestDataLakeConfig(t.TempDir())
	storeBucket := openTestStoreBucket(t, config, "store")
	writer := newBucketWriter(context.Background(), storeBucket, config)
	defer writer.Close()

	stream := newTestStream([]byte("value"))
	stream.readErr = errors.New("disk on fire")
	require.ErrorContains(t, writer.PutStream(1, stream).Wait(), "disk on fire")
	require.Equal(t, 0, stream.resets)
	require.True(t, stream.closed)
}

func TestBucketWriterCloseAbandonsUnfinishedOperations(t *testing.T) {
	faulty, config := newFaultyBucket(t)
	faulty.SetHook(func(hookCtx context.Context, op string, _ string) error {
		if op == "write" {
			// Block every write until the writer is closed.
			<-hookCtx.Done()
			return hookCtx.Err()
		}
		return nil
	})
	config.WriterCount = 1
	storeBucket := openTestStoreBucket(t, config, "store")
	writer := newBucketWriter(context.Background(), storeBucket, config)

	running := newTestStream([]byte("running"))
	queued := newTestStream([]byte("queued"))
	runningPut := writer.PutStream(1, running)
	queuedPut := writer.PutStream(2, queued)
	_, done := queuedPut.Check()
	require.False(t, done)

	writer.Close()
	require.Error(t, runningPut.Wait())
	require.Error(t, queuedPut.Wait())
	require.True(t, running.closed)
	require.True(t, queued.closed)
}

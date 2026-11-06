package datalake

import (
	"context"
	"math"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"gocloud.dev/gcerrors"
)

func TestBucketReaderRetriesTransientFailures(t *testing.T) {
	ctx := context.Background()
	faulty, config := newFaultyBucket(t)
	storeBucket := openTestStoreBucket(t, config, "store")
	require.NoError(t, storeBucket.Put(ctx, 1, testValue(1)))

	failures := 0
	faulty.SetHook(func(_ context.Context, op string, _ string) error {
		if op == "read" && failures < 3 {
			failures++
			return &injectedError{code: gcerrors.Unknown}
		}
		return nil
	})
	config.ReaderCount = 1
	reader := newBucketReader(ctx, storeBucket, config)
	defer reader.Close()

	value, found, err := reader.Get(ctx, 1).Value(ctx)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, testValue(1), value)
	require.Equal(t, 3, failures)
}

func TestBucketReaderIteratorWithOrphansRetriesTransientFailures(t *testing.T) {
	ctx := context.Background()
	faulty, config := newFaultyBucket(t)
	storeBucket := openTestStoreBucket(t, config, "store")
	keyCount := uint64(2*listPageSize + listPageSize/2)
	for key := uint64(1); key <= keyCount; key++ {
		require.NoError(t, storeBucket.Put(ctx, key, nil))
	}

	lists := 0
	failures := 0
	faulty.SetHook(func(_ context.Context, op string, _ string) error {
		if op != "list" {
			return nil
		}
		lists++
		if lists == 2 {
			// Fail the listing of the second page once.
			failures++
			return &injectedError{code: gcerrors.Unknown}
		}
		return nil
	})
	reader := newBucketReader(ctx, storeBucket, config)
	defer reader.Close()

	topology := testStoreMetadata(t, false, 1, 1, keyCount+1).KeyTopology()
	iterator, err := reader.Iterator(true, topology, math.MaxUint64, true, 0)
	require.NoError(t, err)
	defer func() { require.NoError(t, iterator.Close()) }()
	var scanned []uint64
	for {
		ok, err := iterator.Next(ctx)
		require.NoError(t, err)
		if !ok {
			break
		}
		scanned = append(scanned, iterator.Key())
	}
	expected := keyRange(1, keyCount)
	slices.Reverse(expected)
	require.Equal(t, expected, scanned)
	require.Equal(t, 1, failures)
}

func TestBucketReaderIteratorWithOrphansReportsPermanentFailures(t *testing.T) {
	ctx := context.Background()
	faulty, config := newFaultyBucket(t)
	storeBucket := openTestStoreBucket(t, config, "store")
	require.NoError(t, storeBucket.Put(ctx, 1, nil))
	faulty.SetHook(func(_ context.Context, op string, _ string) error {
		if op == "list" {
			return &injectedError{code: gcerrors.PermissionDenied}
		}
		return nil
	})
	reader := newBucketReader(ctx, storeBucket, config)
	defer reader.Close()

	topology := testStoreMetadata(t, false, 1, 1, 2).KeyTopology()
	iterator, err := reader.Iterator(true, topology, math.MaxUint64, true, 0)
	require.NoError(t, err)
	defer func() { require.NoError(t, iterator.Close()) }()
	_, err = iterator.Next(ctx)
	require.ErrorContains(t, err, "PermissionDenied")
	require.ErrorContains(t, err, "key listing failed after 0 retries")
}

func TestBucketReaderIteratorWithOrphansRequiresReverseOrder(t *testing.T) {
	config := DefaultTestDataLakeConfig(t.TempDir())
	reader := newBucketReader(context.Background(), openTestStoreBucket(t, config, "store"), config)
	defer reader.Close()

	_, err := reader.Iterator(true, newStoreMetadata(false).KeyTopology(), 0, false, 0)
	require.ErrorContains(t, err, "only legal for reverse-ordered iteration")
}

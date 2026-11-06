package datalake

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// newTestMetadataManager returns a manager of a new store without gaps, its deleter, and the bucket it writes to.
func newTestMetadataManager(
	t *testing.T,
	// The minimum time between the starts of metadata writes made by WriteIfDue().
	writeInterval time.Duration,
) (*metadataManager, *contiguousDeleter, *storeBucket) {
	config := DefaultTestDataLakeConfig(t.TempDir())
	bucket := openTestStoreBucket(t, config, "store")
	writer := newBucketWriter(context.Background(), bucket, config)
	t.Cleanup(writer.Close)
	metadata := *newStoreMetadata(false)
	deleter := newContiguousDeleter(metadata)
	return newMetadataManager(writer, deleter, writeInterval, metadata, false), deleter, bucket
}

func TestMetadataManagerWriteAndWaitMakesMetadataDurable(t *testing.T) {
	manager, deleter, bucket := newTestMetadataManager(t, time.Hour)
	require.False(t, manager.Written())

	deleter.Start(0)
	deleter.Prune(3)
	require.Empty(t, takeDeletes(deleter, math.MaxUint64), "the raised watermark is not durable")

	require.NoError(t, manager.WriteAndWait(5))
	require.True(t, manager.Written())
	require.Equal(t, testStoreMetadata(t, false, 3, 0, 5), readMetadata(t, bucket))
	require.Equal(t, []uint64{0, 1, 2}, deleteKeys(takeDeletes(deleter, math.MaxUint64)),
		"the deleter learns of the durable metadata")
}

func TestMetadataManagerWriteIfDueHonorsInterval(t *testing.T) {
	manager, deleter, _ := newTestMetadataManager(t, time.Hour)
	deleter.Start(0)

	require.NoError(t, manager.WriteIfDue(0))
	require.Nil(t, manager.inFlightWrite, "the metadata is unchanged")

	require.NoError(t, manager.WriteIfDue(1))
	require.NotNil(t, manager.inFlightWrite, "no write has started yet, so one is due")
	require.NoError(t, manager.inFlightWrite.Wait())
	require.NoError(t, manager.Collect())

	require.NoError(t, manager.WriteIfDue(2))
	require.Nil(t, manager.inFlightWrite, "the interval has not passed")
}

func TestMetadataManagerWriteIfChangedReportsCurrent(t *testing.T) {
	manager, deleter, bucket := newTestMetadataManager(t, time.Hour)
	deleter.Start(0)
	require.NoError(t, manager.WriteAndWait(1))

	current, err := manager.WriteIfChanged(1)
	require.NoError(t, err)
	require.True(t, current)

	current, err = manager.WriteIfChanged(2)
	require.NoError(t, err)
	require.False(t, current, "a write has started, whatever the interval")
	current, err = manager.WriteIfChanged(2)
	require.NoError(t, err)
	require.False(t, current, "the write is in flight")

	require.NoError(t, manager.inFlightWrite.Wait())
	require.NoError(t, manager.Collect())
	current, err = manager.WriteIfChanged(2)
	require.NoError(t, err)
	require.True(t, current)
	require.Equal(t, testStoreMetadata(t, false, 0, 0, 2), readMetadata(t, bucket))
}

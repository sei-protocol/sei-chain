package cdkdatalake

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// newTestMetadataManager returns a manager of a new store without gaps, its deleter, and the bucket it writes to.
func newTestMetadataManager(
	t *testing.T,
	// The minimum time between the starts of unforced metadata writes.
	writeInterval time.Duration,
) (*metadataManager, *contiguousDeleter, *storeBucket) {
	config := DefaultTestDataLakeConfig(t.TempDir())
	bucket := openTestStoreBucket(t, config, "store")
	writer := newBucketWriter(context.Background(), bucket, config)
	t.Cleanup(writer.Close)
	metadata := *newStoreMetadata(false)
	deleter := newContiguousDeleter(writer, metadata)
	return newMetadataManager(writer, deleter, writeInterval, metadata), deleter, bucket
}

// isNewStore reports whether manager's durable metadata is a new store's.
func isNewStore(manager *metadataManager) bool {
	metadata := manager.DurableMetadata()
	return metadata.IsNewStore()
}

func TestMetadataManagerWaitedWriteMakesMetadataDurable(t *testing.T) {
	manager, deleter, bucket := newTestMetadataManager(t, time.Hour)
	require.True(t, isNewStore(manager))

	deleter.SetFirstKey(0)
	deleter.Prune(3)

	require.NoError(t, manager.Write(3, 5, true, true))
	require.False(t, isNewStore(manager))
	require.Equal(t, testStoreMetadata(t, false, 3, 0, 5), readMetadata(t, bucket))
	require.Equal(t, *testStoreMetadata(t, false, 3, 0, 5), manager.DurableMetadata())
}

func TestMetadataManagerWritesUnchangedMetadataOfUnwrittenStore(t *testing.T) {
	manager, deleter, bucket := newTestMetadataManager(t, time.Hour)
	deleter.SetFirstKey(0)

	require.NoError(t, manager.Write(0, 0, false, true))
	require.False(t, isNewStore(manager))
	require.Equal(t, testStoreMetadata(t, false, 0, 0, 0), readMetadata(t, bucket))

	require.NoError(t, manager.Write(0, 0, true, false))
	require.Nil(t, manager.inFlightWrite, "the written metadata is unchanged")
}

func TestMetadataManagerUnforcedWriteHonorsInterval(t *testing.T) {
	manager, deleter, _ := newTestMetadataManager(t, time.Hour)
	deleter.SetFirstKey(0)

	require.NoError(t, manager.Write(0, 1, false, true))
	require.False(t, isNewStore(manager), "no write has started yet, so one is due")

	require.NoError(t, manager.Write(0, 2, false, false))
	require.Nil(t, manager.inFlightWrite, "the interval has not passed")
}

func TestMetadataManagerForcedWriteIgnoresInterval(t *testing.T) {
	manager, deleter, bucket := newTestMetadataManager(t, time.Hour)
	deleter.SetFirstKey(0)
	require.NoError(t, manager.Write(0, 1, true, true))

	require.NoError(t, manager.Write(0, 2, true, false))
	require.NotNil(t, manager.inFlightWrite, "a write has started, whatever the interval")
	inFlight := manager.inFlightWrite
	require.NoError(t, manager.Write(0, 3, true, false))
	require.Same(t, inFlight, manager.inFlightWrite, "no second write starts while one is in flight")

	require.NoError(t, manager.Write(0, 2, true, true))
	require.Equal(t, testStoreMetadata(t, false, 0, 0, 2), readMetadata(t, bucket))
}

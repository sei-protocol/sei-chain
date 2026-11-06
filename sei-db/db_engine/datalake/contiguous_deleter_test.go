package datalake

import (
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// takeDeletes takes every delete d produces now.
func takeDeletes(d deleter, durableWatermark uint64) []*deleteWrite {
	var writes []*deleteWrite
	for {
		write, ok := d.Next(durableWatermark)
		if !ok {
			return writes
		}
		writes = append(writes, write)
	}
}

// deleteKeys returns the keys writes delete.
func deleteKeys(writes []*deleteWrite) []uint64 {
	keys := make([]uint64, 0, len(writes))
	for _, write := range writes {
		keys = append(keys, write.key)
	}
	return keys
}

// finishDeletes completes writes successfully.
func finishDeletes(writes ...*deleteWrite) {
	for _, write := range writes {
		write.result.complete(nil)
	}
}

// contiguousMetadata returns metadata of a store without gaps with the given watermarks.
func contiguousMetadata(
	t *testing.T,
	scheduledDeletionWatermark uint64,
	deletionWatermark uint64,
	durableWatermark uint64,
) storeMetadata {
	metadata := newStoreMetadata(false)
	require.NoError(t, metadata.SetWatermarks(scheduledDeletionWatermark, deletionWatermark, durableWatermark))
	return *metadata
}

func TestContiguousDeleterWaitsForDurableMetadata(t *testing.T) {
	d := newContiguousDeleter(*newStoreMetadata(false))
	d.Start(10)

	d.Prune(13)
	require.Equal(t, uint64(13), d.ScheduledDeletionWatermark())
	require.Empty(t, takeDeletes(d, math.MaxUint64), "the raised watermark is not durable")

	d.MetadataDurable(contiguousMetadata(t, 12, 0, 12))
	require.Equal(t, []uint64{10, 11}, deleteKeys(takeDeletes(d, math.MaxUint64)))

	d.MetadataDurable(contiguousMetadata(t, 13, 0, 13))
	require.Equal(t, []uint64{12}, deleteKeys(takeDeletes(d, math.MaxUint64)))
}

func TestContiguousDeleterWaitsForPuts(t *testing.T) {
	d := newContiguousDeleter(*newStoreMetadata(false))
	d.Start(0)
	d.Prune(5)
	d.MetadataDurable(contiguousMetadata(t, 5, 0, 5))

	require.Equal(t, []uint64{0, 1}, deleteKeys(takeDeletes(d, 2)))
	require.Equal(t, []uint64{2, 3, 4}, deleteKeys(takeDeletes(d, 10)))
}

func TestContiguousDeleterWatermarkAndIdle(t *testing.T) {
	d := newContiguousDeleter(*newStoreMetadata(false))
	d.Start(0)
	require.True(t, d.Idle())
	d.Delete(0)
	d.Prune(3)
	require.False(t, d.Idle())
	require.Equal(t, uint64(0), d.DeletionWatermark())

	d.MetadataDurable(contiguousMetadata(t, 3, 0, 3))
	writes := takeDeletes(d, math.MaxUint64)
	require.Equal(t, []uint64{0, 1, 2}, deleteKeys(writes))

	finishDeletes(writes[1], writes[2])
	require.NoError(t, d.Collect())
	require.Equal(t, uint64(0), d.DeletionWatermark(), "the delete of key 0 is still running")
	require.False(t, d.Idle())

	finishDeletes(writes[0])
	require.NoError(t, d.Collect())
	require.Equal(t, uint64(3), d.DeletionWatermark())
	require.True(t, d.Idle())
}

func TestContiguousDeleterReportsFailedDelete(t *testing.T) {
	d := newContiguousDeleter(*newStoreMetadata(false))
	d.Start(0)
	d.Prune(1)
	d.MetadataDurable(contiguousMetadata(t, 1, 0, 1))

	writes := takeDeletes(d, math.MaxUint64)
	writes[0].result.complete(errors.New("boom"))
	require.Error(t, d.Collect())
}

func TestContiguousDeleterResumesRecoveredDeletes(t *testing.T) {
	d := newContiguousDeleter(contiguousMetadata(t, 8, 5, 10))

	require.Equal(t, uint64(8), d.ScheduledDeletionWatermark())
	require.Equal(t, uint64(5), d.DeletionWatermark())
	require.Equal(t, []uint64{5, 6, 7}, deleteKeys(takeDeletes(d, math.MaxUint64)))
}

func TestContiguousDeleterStartSkipsUnwrittenKeys(t *testing.T) {
	d := newContiguousDeleter(contiguousMetadata(t, 4, 4, 4))
	d.Start(100)

	require.Equal(t, uint64(100), d.DeletionWatermark(), "no key from 4 up to 100 was ever written")
	require.Empty(t, takeDeletes(d, math.MaxUint64))
}

func TestContiguousDeleterStartKeepsUndurableDeletesWaiting(t *testing.T) {
	d := newContiguousDeleter(contiguousMetadata(t, 4, 4, 6))
	d.Prune(6)
	d.Start(100)

	require.Equal(t, uint64(4), d.DeletionWatermark())
	require.Empty(t, takeDeletes(d, math.MaxUint64), "keys 4 and 5 still wait for durable metadata")

	d.MetadataDurable(contiguousMetadata(t, 100, 4, 100))
	require.Equal(t, []uint64{4, 5}, deleteKeys(takeDeletes(d, math.MaxUint64)))
}

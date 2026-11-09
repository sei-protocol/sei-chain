package datalake

import (
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// recordingObjectDeleter records every delete it is handed and leaves each unfinished.
type recordingObjectDeleter struct {
	// The deletes handed to it and not yet taken by takeDeletes(), in the order they arrived.
	submitted []*writeResult
}

// Delete records a delete of key and returns its unfinished result.
func (r *recordingObjectDeleter) Delete(key uint64) *writeResult {
	result := newWriteResult(key)
	r.submitted = append(r.submitted, result)
	return result
}

// takeDeletes has d start every delete that may start now, and returns them.
func takeDeletes(d deleter, writer *recordingObjectDeleter, durableWatermark uint64) []*writeResult {
	d.DoBackgroundWork(durableWatermark, math.MaxInt)
	taken := writer.submitted
	writer.submitted = nil
	return taken
}

// deleteKeys returns the keys deletes delete.
func deleteKeys(deletes []*writeResult) []uint64 {
	keys := make([]uint64, 0, len(deletes))
	for _, result := range deletes {
		keys = append(keys, result.Key())
	}
	return keys
}

// finishDeletes completes deletes successfully.
func finishDeletes(deletes ...*writeResult) {
	for _, result := range deletes {
		result.complete(nil)
	}
}

// newTestContiguousDeleter returns a contiguous deleter of a store whose durable metadata is metadata, and the
// writer that records its deletes.
func newTestContiguousDeleter(metadata storeMetadata) (*contiguousDeleter, *recordingObjectDeleter) {
	writer := &recordingObjectDeleter{}
	return newContiguousDeleter(writer, metadata), writer
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

func TestContiguousDeleterPruneSchedulesOnlyNewKeys(t *testing.T) {
	d, writer := newTestContiguousDeleter(*newStoreMetadata(false))
	d.SetFirstKey(10)

	d.Prune(13)
	require.Equal(t, []uint64{10, 11, 12}, deleteKeys(takeDeletes(d, writer, math.MaxUint64)))

	d.Prune(12)
	require.Empty(t, takeDeletes(d, writer, math.MaxUint64))

	d.Prune(15)
	require.Equal(t, []uint64{13, 14}, deleteKeys(takeDeletes(d, writer, math.MaxUint64)))
}

func TestContiguousDeleterWaitsForPuts(t *testing.T) {
	d, writer := newTestContiguousDeleter(*newStoreMetadata(false))
	d.SetFirstKey(0)
	d.Prune(5)

	require.Equal(t, []uint64{0, 1}, deleteKeys(takeDeletes(d, writer, 2)))
	require.Equal(t, []uint64{2, 3, 4}, deleteKeys(takeDeletes(d, writer, 10)))
}

func TestContiguousDeleterStartsAtMostLimit(t *testing.T) {
	d, writer := newTestContiguousDeleter(*newStoreMetadata(false))
	d.SetFirstKey(0)
	d.Prune(5)

	d.DoBackgroundWork(math.MaxUint64, 2)
	require.Equal(t, []uint64{0, 1}, deleteKeys(writer.submitted))
	d.DoBackgroundWork(math.MaxUint64, 2)
	require.Equal(t, []uint64{0, 1, 2, 3}, deleteKeys(writer.submitted))
}

func TestContiguousDeleterWatermarkWaitsForEarliestDelete(t *testing.T) {
	d, writer := newTestContiguousDeleter(*newStoreMetadata(false))
	d.SetFirstKey(0)
	d.Prune(1)
	d.Prune(3)
	require.Equal(t, uint64(0), d.DeletionWatermark())

	deletes := takeDeletes(d, writer, math.MaxUint64)
	require.Equal(t, []uint64{0, 1, 2}, deleteKeys(deletes))

	finishDeletes(deletes[1], deletes[2])
	require.NoError(t, d.FinalizeDeletes())
	require.Equal(t, uint64(0), d.DeletionWatermark(), "the delete of key 0 is still running")

	finishDeletes(deletes[0])
	require.NoError(t, d.FinalizeDeletes())
	require.Equal(t, uint64(3), d.DeletionWatermark())
}

func TestContiguousDeleterRejectsDelete(t *testing.T) {
	d, writer := newTestContiguousDeleter(*newStoreMetadata(false))
	d.SetFirstKey(0)

	require.Error(t, d.Delete(0))
	require.Empty(t, takeDeletes(d, writer, math.MaxUint64))
}

func TestContiguousDeleterReportsFailedDelete(t *testing.T) {
	d, writer := newTestContiguousDeleter(*newStoreMetadata(false))
	d.SetFirstKey(0)
	d.Prune(1)

	deletes := takeDeletes(d, writer, math.MaxUint64)
	deletes[0].complete(errors.New("boom"))
	require.Error(t, d.FinalizeDeletes())
}

func TestContiguousDeleterResumesRecoveredDeletes(t *testing.T) {
	d, writer := newTestContiguousDeleter(contiguousMetadata(t, 8, 5, 10))

	require.Equal(t, uint64(5), d.DeletionWatermark())
	require.Equal(t, []uint64{5, 6, 7}, deleteKeys(takeDeletes(d, writer, math.MaxUint64)))
}

func TestContiguousDeleterSetFirstKeySkipsUnwrittenKeys(t *testing.T) {
	d, writer := newTestContiguousDeleter(contiguousMetadata(t, 4, 4, 4))
	d.SetFirstKey(100)
	d.Prune(100)

	require.Equal(t, uint64(100), d.DeletionWatermark(), "no key from 4 up to 100 was ever written")
	require.Empty(t, takeDeletes(d, writer, math.MaxUint64))
}

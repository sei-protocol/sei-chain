package cdkdatalake

import (
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/structures"
)

// newKeySet returns a key set holding keys.
func newKeySet(keys ...uint64) *structures.SortedMap[uint64, struct{}] {
	set := structures.NewSortedMap[uint64, struct{}]()
	for _, key := range keys {
		set.Put(key, struct{}{})
	}
	return set
}

// gappedMetadata returns metadata of a store with gaps with the given deletion watermarks.
func gappedMetadata(t *testing.T, scheduledDeletionWatermark uint64, deletionWatermark uint64) storeMetadata {
	metadata := newStoreMetadata(true)
	err := metadata.SetWatermarks(scheduledDeletionWatermark, deletionWatermark, scheduledDeletionWatermark)
	require.NoError(t, err)
	return *metadata
}

// newTestGappedDeleter returns a gapped deleter of a store holding keys, and the writer that records its deletes.
func newTestGappedDeleter(
	keys *structures.SortedMap[uint64, struct{}],
	metadata storeMetadata,
	// Keys below the metadata's scheduled deletion watermark whose objects still exist, in ascending order.
	danglingKeys []uint64,
) (*gappedDeleter, *recordingObjectDeleter) {
	writer := &recordingObjectDeleter{}
	return newGappedDeleter(writer, keys, metadata, danglingKeys), writer
}

// keySetKeys returns the keys in set, in ascending order.
func keySetKeys(set *structures.SortedMap[uint64, struct{}]) []uint64 {
	var keys []uint64
	for key := range set.Forward() {
		keys = append(keys, key)
	}
	return keys
}

func TestGappedDeleterPruneDeletesEachKey(t *testing.T) {
	keys := newKeySet(2, 5, 9, 20)
	d, writer := newTestGappedDeleter(keys, gappedMetadata(t, 2, 0), nil)

	d.Prune(10)
	require.Equal(t, []uint64{20}, keySetKeys(keys))
	require.Equal(t, []uint64{2, 5, 9}, deleteKeys(takeDeletes(d, writer, math.MaxUint64)))
}

func TestGappedDeleterDeleteRemovesOneKey(t *testing.T) {
	keys := newKeySet(2, 5, 9)
	d, writer := newTestGappedDeleter(keys, gappedMetadata(t, 2, 0), nil)

	require.NoError(t, d.Delete(5))
	require.Equal(t, []uint64{2, 9}, keySetKeys(keys))
	require.Equal(t, []uint64{5}, deleteKeys(takeDeletes(d, writer, math.MaxUint64)))
}

func TestGappedDeleterWaitsForPuts(t *testing.T) {
	keys := newKeySet(2, 5, 9)
	d, writer := newTestGappedDeleter(keys, gappedMetadata(t, 2, 0), nil)
	require.NoError(t, d.Delete(9))
	require.NoError(t, d.Delete(5))

	require.Empty(t, takeDeletes(d, writer, 5))
	require.Equal(t, []uint64{5}, deleteKeys(takeDeletes(d, writer, 6)))
	require.Equal(t, []uint64{9}, deleteKeys(takeDeletes(d, writer, 10)))
}

func TestGappedDeleterReportsFailedDelete(t *testing.T) {
	keys := newKeySet(2, 5)
	d, writer := newTestGappedDeleter(keys, gappedMetadata(t, 2, 0), nil)
	require.NoError(t, d.Delete(2))

	deletes := takeDeletes(d, writer, math.MaxUint64)
	deletes[0].complete(errors.New("boom"))
	require.Error(t, d.FinalizeDeletes())
}

func TestGappedDeleterDeletesDanglingKeys(t *testing.T) {
	keys := newKeySet(12)
	d, writer := newTestGappedDeleter(keys, gappedMetadata(t, 10, 0), []uint64{3, 7})

	require.Equal(t, []uint64{3, 7}, deleteKeys(takeDeletes(d, writer, 12)))
	require.Equal(t, []uint64{12}, keySetKeys(keys))
}

func TestGappedDeleterKeepsRecoveredDeletionWatermark(t *testing.T) {
	keys := newKeySet(4, 5, 9)
	d, writer := newTestGappedDeleter(keys, gappedMetadata(t, 4, 1), nil)

	d.Prune(6)
	deletes := takeDeletes(d, writer, math.MaxUint64)
	require.Equal(t, []uint64{4, 5}, deleteKeys(deletes))
	finishDeletes(deletes...)
	require.NoError(t, d.FinalizeDeletes())

	require.Equal(t, uint64(1), d.DeletionWatermark())
}

package datalake

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

// keySetKeys returns the keys in set, in ascending order.
func keySetKeys(set *structures.SortedMap[uint64, struct{}]) []uint64 {
	var keys []uint64
	for key := range set.Forward() {
		keys = append(keys, key)
	}
	return keys
}

func TestGappedDeleterPruneDeletesEachKeyWithoutWaitingForMetadata(t *testing.T) {
	keys := newKeySet(2, 5, 9, 20)
	d := newGappedDeleter(keys, gappedMetadata(t, 2, 0), nil)

	d.Prune(10)
	require.Equal(t, uint64(10), d.ScheduledDeletionWatermark())
	require.Equal(t, []uint64{20}, keySetKeys(keys))
	require.Equal(t, []uint64{2, 5, 9}, deleteKeys(takeDeletes(d, math.MaxUint64)))
}

func TestGappedDeleterDeleteRemovesOneKey(t *testing.T) {
	keys := newKeySet(2, 5, 9)
	d := newGappedDeleter(keys, gappedMetadata(t, 2, 0), nil)

	d.Delete(5)
	require.Equal(t, []uint64{2, 9}, keySetKeys(keys))
	require.Equal(t, uint64(2), d.ScheduledDeletionWatermark())
	require.Equal(t, []uint64{5}, deleteKeys(takeDeletes(d, math.MaxUint64)))
}

func TestGappedDeleterWaitsForPuts(t *testing.T) {
	keys := newKeySet(2, 5, 9)
	d := newGappedDeleter(keys, gappedMetadata(t, 2, 0), nil)
	d.Delete(9)
	d.Delete(5)

	require.Empty(t, takeDeletes(d, 5))
	require.Equal(t, []uint64{5}, deleteKeys(takeDeletes(d, 6)))
	require.Equal(t, []uint64{9}, deleteKeys(takeDeletes(d, 10)))
}

func TestGappedDeleterIdle(t *testing.T) {
	keys := newKeySet(2, 5, 9, 20)
	d := newGappedDeleter(keys, gappedMetadata(t, 2, 0), nil)
	require.True(t, d.Idle())

	d.Delete(9)
	d.Prune(6)
	require.False(t, d.Idle())

	writes := takeDeletes(d, math.MaxUint64)
	require.Equal(t, []uint64{2, 5, 9}, deleteKeys(writes))
	require.False(t, d.Idle())

	finishDeletes(writes[0], writes[2])
	require.NoError(t, d.Collect())
	require.False(t, d.Idle(), "the delete of key 9 is collected only after the delete of key 5")

	finishDeletes(writes[1])
	require.NoError(t, d.Collect())
	require.True(t, d.Idle())
}

func TestGappedDeleterReportsFailedDelete(t *testing.T) {
	keys := newKeySet(2, 5)
	d := newGappedDeleter(keys, gappedMetadata(t, 2, 0), nil)
	d.Delete(2)

	writes := takeDeletes(d, math.MaxUint64)
	writes[0].result.complete(errors.New("boom"))
	require.Error(t, d.Collect())
}

func TestGappedDeleterDeletesDanglingKeys(t *testing.T) {
	keys := newKeySet(12)
	d := newGappedDeleter(keys, gappedMetadata(t, 10, 0), []uint64{3, 7})

	require.Equal(t, []uint64{3, 7}, deleteKeys(takeDeletes(d, 12)))
	require.Equal(t, []uint64{12}, keySetKeys(keys))
}

func TestGappedDeleterKeepsRecoveredDeletionWatermark(t *testing.T) {
	keys := newKeySet(4, 5, 9)
	d := newGappedDeleter(keys, gappedMetadata(t, 4, 1), nil)

	d.Prune(6)
	writes := takeDeletes(d, math.MaxUint64)
	require.Equal(t, []uint64{4, 5}, deleteKeys(writes))
	finishDeletes(writes...)
	require.NoError(t, d.Collect())

	require.Equal(t, uint64(6), d.ScheduledDeletionWatermark())
	require.Equal(t, uint64(1), d.DeletionWatermark())
}

package datalake

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStoreMetadataRoundTrips(t *testing.T) {
	for _, allowGaps := range []bool{false, true} {
		for _, rollback := range []bool{false, true} {
			metadata := newStoreMetadata(allowGaps)
			require.NoError(t, metadata.SetWatermarks(7, 3, math.MaxUint64))
			if rollback {
				require.NoError(t, metadata.StartRollback(5))
			}
			parsed, err := deserializeStoreMetadata(metadata.Serialize())
			require.NoError(t, err)
			require.Equal(t, metadata, parsed)
		}
	}
}

func TestStoreMetadataFinishRollbackLowersWatermarks(t *testing.T) {
	metadata := newStoreMetadata(false)
	require.Error(t, metadata.FinishRollback(0), "no rollback in progress")

	require.NoError(t, metadata.SetWatermarks(10, 5, 20))
	require.NoError(t, metadata.StartRollback(15))
	require.Error(t, metadata.StartRollback(15), "already in progress")
	require.True(t, metadata.RollbackInProgress())
	require.Equal(t, uint64(15), metadata.RollbackWatermark())

	// The highest key left is 12, below the rollback watermark.
	require.NoError(t, metadata.FinishRollback(13))
	require.Equal(t, uint64(10), metadata.ScheduledDeletionWatermark())
	require.Equal(t, uint64(5), metadata.DeletionWatermark())
	require.Equal(t, uint64(13), metadata.DurableWatermark())
	require.False(t, metadata.RollbackInProgress())
	require.Equal(t, uint64(0), metadata.RollbackWatermark())

	// No key is left, so the durable watermark falls to the scheduled deletion watermark.
	require.NoError(t, metadata.StartRollback(8))
	require.NoError(t, metadata.FinishRollback(0))
	require.Equal(t, uint64(8), metadata.ScheduledDeletionWatermark())
	require.Equal(t, uint64(5), metadata.DeletionWatermark())
	require.Equal(t, uint64(8), metadata.DurableWatermark())

	// A rollback below every watermark lowers all three.
	require.NoError(t, metadata.StartRollback(3))
	require.NoError(t, metadata.FinishRollback(0))
	require.Equal(t, uint64(3), metadata.ScheduledDeletionWatermark())
	require.Equal(t, uint64(3), metadata.DeletionWatermark())
	require.Equal(t, uint64(3), metadata.DurableWatermark())
}

func TestStoreMetadataSetWatermarksRejectsInvalidWatermarks(t *testing.T) {
	metadata := newStoreMetadata(false)
	require.NoError(t, metadata.SetWatermarks(10, 5, 8))

	invalid := []struct {
		scheduledDeletion uint64
		deletion          uint64
		durable           uint64
	}{
		{scheduledDeletion: 9, deletion: 5, durable: 8},    // scheduled deletion decreases
		{scheduledDeletion: 10, deletion: 4, durable: 8},   // deletion decreases
		{scheduledDeletion: 10, deletion: 5, durable: 7},   // durable decreases
		{scheduledDeletion: 10, deletion: 11, durable: 20}, // deletion above scheduled deletion
		{scheduledDeletion: 20, deletion: 9, durable: 8},   // deletion above durable
	}
	for i, c := range invalid {
		require.Error(t, metadata.SetWatermarks(c.scheduledDeletion, c.deletion, c.durable), "case %d", i)
		require.Equal(t, uint64(10), metadata.ScheduledDeletionWatermark(), "case %d", i)
		require.Equal(t, uint64(5), metadata.DeletionWatermark(), "case %d", i)
		require.Equal(t, uint64(8), metadata.DurableWatermark(), "case %d", i)
	}

	require.NoError(t, metadata.SetWatermarks(20, 8, 8))
}

func TestStoreMetadataRejectsBadData(t *testing.T) {
	metadata := newStoreMetadata(false)
	require.NoError(t, metadata.SetWatermarks(1, 0, 1))
	data := metadata.Serialize()

	_, err := deserializeStoreMetadata(data[:3])
	require.Error(t, err)

	_, err = deserializeStoreMetadata(data[:len(data)-1])
	require.Error(t, err)

	badVersion := append([]byte{}, data...)
	badVersion[3] = 99
	_, err = deserializeStoreMetadata(badVersion)
	require.Error(t, err)

	badGapFlag := append([]byte{}, data...)
	badGapFlag[28] = 2
	_, err = deserializeStoreMetadata(badGapFlag)
	require.Error(t, err)

	badRollbackFlag := append([]byte{}, data...)
	badRollbackFlag[29] = 2
	_, err = deserializeStoreMetadata(badRollbackFlag)
	require.Error(t, err)

	// A rollback watermark without a rollback in progress.
	strayRollbackWatermark := append([]byte{}, data...)
	binary.BigEndian.PutUint64(strayRollbackWatermark[30:38], 4)
	_, err = deserializeStoreMetadata(strayRollbackWatermark)
	require.Error(t, err)

	// A deletion watermark above the scheduled deletion watermark.
	badWatermarks := append([]byte{}, data...)
	binary.BigEndian.PutUint64(badWatermarks[12:20], 2)
	_, err = deserializeStoreMetadata(badWatermarks)
	require.Error(t, err)
}

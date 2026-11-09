package datalake

import (
	"context"
	"encoding/binary"
	"fmt"
)

// storeMetadataVersion is the serialization version of storeMetadata.
const storeMetadataVersion uint32 = 1

// storeMetadataSize is the length of a serialized storeMetadata: the version, three watermarks, the gap flag, the
// rollback flag and the rollback watermark.
const storeMetadataSize = 4 + 3*8 + 1 + 1 + 8

// storeMetadata is the content of a store's metadata object.
type storeMetadata struct {
	// Every key strictly less than this watermark has been scheduled for deletion.
	scheduledDeletionWatermark uint64

	// Every key strictly less than this watermark has been deleted.
	deletionWatermark uint64

	// Every write to a key strictly less than this watermark is crash durable.
	durableWatermark uint64

	// Whether the store allows gaps between its keys.
	allowGaps bool

	// Whether a rollback has started and not finished.
	rollbackInProgress bool

	// While a rollback is in progress, the rollback keeps every key strictly less than this watermark and deletes
	// the rest. Zero otherwise.
	rollbackWatermark uint64

	// True for the metadata of a new store until its watermarks are first set. Not serialized.
	newStore bool
}

// newStoreMetadata returns the metadata of a new store, with every watermark at 0.
func newStoreMetadata(allowGaps bool) *storeMetadata {
	return &storeMetadata{allowGaps: allowGaps, newStore: true}
}

// loadStoreMetadata reads a store's metadata object.
func loadStoreMetadata(
	ctx context.Context,
	reader *bucketReader,
) (
	metadata *storeMetadata,
	// False if the store has no metadata object.
	found bool,
	err error,
) {
	data, found, err := reader.ReadMetadata(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("read metadata object: %w", err)
	}
	if !found {
		return nil, false, nil
	}
	metadata, err = deserializeStoreMetadata(data)
	if err != nil {
		return nil, false, fmt.Errorf("deserialize metadata: %w", err)
	}
	return metadata, true, nil
}

// KeyTopology returns the topology the metadata records. In a store with gaps, which keys exist is unknown unless
// the range they lie in is empty.
func (m *storeMetadata) KeyTopology() keyTopology {
	topology := keyTopology{metadata: *m}
	if _, _, ok := topology.Range(); !ok && m.allowGaps {
		// No key lies in an empty range.
		return topology.WithKeys(nil)
	}
	return topology
}

// ScheduledDeletionWatermark returns the watermark strictly below which every key has been scheduled for deletion.
func (m *storeMetadata) ScheduledDeletionWatermark() uint64 {
	return m.scheduledDeletionWatermark
}

// DeletionWatermark returns the watermark strictly below which every key has been deleted.
func (m *storeMetadata) DeletionWatermark() uint64 {
	return m.deletionWatermark
}

// DurableWatermark returns the watermark strictly below which every write is crash durable.
func (m *storeMetadata) DurableWatermark() uint64 {
	return m.durableWatermark
}

// IsNewStore reports whether this is the metadata of a new store, whose watermarks have never been set.
func (m *storeMetadata) IsNewStore() bool {
	return m.newStore
}

// AllowGaps reports whether the store allows gaps between its keys.
func (m *storeMetadata) AllowGaps() bool {
	return m.allowGaps
}

// RollbackInProgress reports whether a rollback has started and not finished.
func (m *storeMetadata) RollbackInProgress() bool {
	return m.rollbackInProgress
}

// RollbackWatermark returns the watermark strictly below which an in-progress rollback keeps keys, or zero if no
// rollback is in progress.
func (m *storeMetadata) RollbackWatermark() uint64 {
	return m.rollbackWatermark
}

// StartRollback records a rollback that keeps every key strictly less than watermark.
func (m *storeMetadata) StartRollback(watermark uint64) error {
	if m.rollbackInProgress {
		return fmt.Errorf("a rollback to %d is already in progress", m.rollbackWatermark)
	}
	m.rollbackInProgress = true
	m.rollbackWatermark = watermark
	return nil
}

// FinishRollback records that the rollback is complete. It lowers the scheduled deletion and deletion watermarks
// to the rollback watermark, and the durable watermark to just above the highest key left, if they are above those.
func (m *storeMetadata) FinishRollback(
	// One above the highest key left in the store's part of the bucket, or 0 if none is left.
	remainingEnd uint64,
) error {
	if !m.rollbackInProgress {
		return fmt.Errorf("no rollback is in progress")
	}
	m.scheduledDeletionWatermark = min(m.scheduledDeletionWatermark, m.rollbackWatermark)
	m.deletionWatermark = min(m.deletionWatermark, m.rollbackWatermark)
	// Keys below the scheduled deletion watermark are no longer logically part of the store, even while their
	// objects await deletion.
	m.durableWatermark = min(m.durableWatermark, max(m.scheduledDeletionWatermark, remainingEnd))
	m.rollbackInProgress = false
	m.rollbackWatermark = 0
	return nil
}

// SetWatermarks replaces the three watermarks. The metadata is no longer a new store's.
func (m *storeMetadata) SetWatermarks(scheduledDeletion uint64, deletion uint64, durable uint64) error {
	if scheduledDeletion < m.scheduledDeletionWatermark {
		return fmt.Errorf("scheduled deletion watermark would decrease from %d to %d",
			m.scheduledDeletionWatermark, scheduledDeletion)
	}
	if deletion < m.deletionWatermark {
		return fmt.Errorf("deletion watermark would decrease from %d to %d", m.deletionWatermark, deletion)
	}
	if durable < m.durableWatermark {
		return fmt.Errorf("durable watermark would decrease from %d to %d", m.durableWatermark, durable)
	}
	if deletion > scheduledDeletion {
		return fmt.Errorf("deletion watermark %d would exceed the scheduled deletion watermark %d",
			deletion, scheduledDeletion)
	}
	if deletion > durable {
		return fmt.Errorf("deletion watermark %d would exceed the durable watermark %d", deletion, durable)
	}
	m.scheduledDeletionWatermark = scheduledDeletion
	m.deletionWatermark = deletion
	m.durableWatermark = durable
	m.newStore = false
	return nil
}

// Serialize returns the metadata's binary form.
func (m *storeMetadata) Serialize() []byte {
	data := make([]byte, storeMetadataSize)
	binary.BigEndian.PutUint32(data[0:4], storeMetadataVersion)
	binary.BigEndian.PutUint64(data[4:12], m.scheduledDeletionWatermark)
	binary.BigEndian.PutUint64(data[12:20], m.deletionWatermark)
	binary.BigEndian.PutUint64(data[20:28], m.durableWatermark)
	if m.allowGaps {
		data[28] = 1
	}
	if m.rollbackInProgress {
		data[29] = 1
	}
	binary.BigEndian.PutUint64(data[30:38], m.rollbackWatermark)
	return data
}

// deserializeStoreMetadata parses metadata written by Serialize().
func deserializeStoreMetadata(data []byte) (*storeMetadata, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("metadata is %d bytes, too short to hold a version", len(data))
	}
	version := binary.BigEndian.Uint32(data[0:4])
	if version != storeMetadataVersion {
		return nil, fmt.Errorf("unsupported metadata version %d", version)
	}
	if len(data) != storeMetadataSize {
		return nil, fmt.Errorf("metadata is %d bytes, expected %d", len(data), storeMetadataSize)
	}
	if data[28] > 1 {
		return nil, fmt.Errorf("metadata gap flag is %d, expected 0 or 1", data[28])
	}
	if data[29] > 1 {
		return nil, fmt.Errorf("metadata rollback flag is %d, expected 0 or 1", data[29])
	}
	rollbackWatermark := binary.BigEndian.Uint64(data[30:38])
	if data[29] == 0 && rollbackWatermark != 0 {
		return nil, fmt.Errorf(
			"metadata has rollback watermark %d but no rollback in progress", rollbackWatermark)
	}

	metadata := newStoreMetadata(data[28] == 1)
	err := metadata.SetWatermarks(
		binary.BigEndian.Uint64(data[4:12]),
		binary.BigEndian.Uint64(data[12:20]),
		binary.BigEndian.Uint64(data[20:28]),
	)
	if err != nil {
		return nil, fmt.Errorf("invalid metadata watermarks: %w", err)
	}
	if data[29] == 1 {
		if err := metadata.StartRollback(rollbackWatermark); err != nil {
			return nil, fmt.Errorf("invalid metadata rollback: %w", err)
		}
	}
	return metadata, nil
}

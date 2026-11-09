package datalake

import (
	"fmt"
	"time"
)

// metadataManager writes a store's metadata object and tracks which metadata is crash durable. At most one metadata
// write is in flight at a time. Not thread safe.
type metadataManager struct {
	// Performs the metadata writes.
	writer *bucketWriter

	// Supplies the deletion watermark.
	deleter deleter

	// The minimum time between the starts of unforced metadata writes.
	writeInterval time.Duration

	// The last metadata known to be crash durable.
	durableMetadata storeMetadata

	// The metadata write in flight, or nil.
	inFlightWrite *writeResult

	// The metadata inFlightWrite is writing.
	inFlightMetadata storeMetadata

	// When the most recent metadata write started.
	lastWriteStart time.Time
}

// newMetadataManager returns a manager for a store whose durable metadata is durableMetadata.
func newMetadataManager(
	writer *bucketWriter,
	deleter deleter,
	// The minimum time between the starts of unforced metadata writes.
	writeInterval time.Duration,
	// The store's metadata as recovered, or the metadata of a new store.
	durableMetadata storeMetadata,
) *metadataManager {
	return &metadataManager{
		writer:          writer,
		deleter:         deleter,
		writeInterval:   writeInterval,
		durableMetadata: durableMetadata,
	}
}

// CurrentMetadata returns the store's metadata as of now.
func (m *metadataManager) CurrentMetadata(
	// The lowest key not deleted.
	scheduledDeletionWatermark uint64,
	// The lowest key whose put may not be complete.
	durableWatermark uint64,
) (storeMetadata, error) {
	metadata := m.durableMetadata
	err := metadata.SetWatermarks(scheduledDeletionWatermark, m.deleter.DeletionWatermark(), durableWatermark)
	if err != nil {
		return storeMetadata{}, fmt.Errorf("update metadata: %w", err)
	}
	return metadata, nil
}

// DurableMetadata returns the last metadata known to be crash durable.
func (m *metadataManager) DurableMetadata() storeMetadata {
	return m.durableMetadata
}

// FinalizeWrite records the metadata write in flight as durable if it has finished.
func (m *metadataManager) FinalizeWrite() error {
	if m.inFlightWrite == nil {
		return nil
	}
	err, done := m.inFlightWrite.Check()
	if !done {
		return nil
	}
	m.inFlightWrite = nil
	if err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	m.durableMetadata = m.inFlightMetadata
	return nil
}

// Write starts a write of the store's current metadata unless a write is in flight, or the metadata matches the last
// durable write and the metadata object exists. Unless force is true, it writes only once the write interval has
// passed since the last write started.
func (m *metadataManager) Write(
	// The lowest key not deleted.
	scheduledDeletionWatermark uint64,
	// The lowest key whose put may not be complete.
	durableWatermark uint64,
	// True to write whatever the write interval.
	force bool,
	// True to first wait for the write in flight, and to return only once the metadata written is durable.
	wait bool,
) error {
	if wait {
		if err := m.waitForInFlightWrite(); err != nil {
			return fmt.Errorf("finish the metadata write in flight: %w", err)
		}
	}
	if m.inFlightWrite == nil && (force || time.Since(m.lastWriteStart) >= m.writeInterval) {
		metadata, err := m.CurrentMetadata(scheduledDeletionWatermark, durableWatermark)
		if err != nil {
			return fmt.Errorf("start a metadata write: %w", err)
		}
		if metadata != m.durableMetadata {
			m.start(metadata)
		}
	}
	if wait {
		if err := m.waitForInFlightWrite(); err != nil {
			return fmt.Errorf("finish the metadata write: %w", err)
		}
	}
	return nil
}

// start hands the writer a write of metadata, waiting while the writer's queue is full. No write may be in flight.
func (m *metadataManager) start(metadata storeMetadata) {
	m.inFlightMetadata = metadata
	m.lastWriteStart = time.Now()
	m.inFlightWrite = m.writer.WriteMetadata(metadata.Serialize())
}

// waitForInFlightWrite waits for the metadata write in flight, if any, and records it as durable.
func (m *metadataManager) waitForInFlightWrite() error {
	if m.inFlightWrite == nil {
		return nil
	}
	// FinalizeWrite() reports the write's outcome.
	_ = m.inFlightWrite.Wait()
	if err := m.FinalizeWrite(); err != nil {
		return fmt.Errorf("wait for the metadata write: %w", err)
	}
	return nil
}

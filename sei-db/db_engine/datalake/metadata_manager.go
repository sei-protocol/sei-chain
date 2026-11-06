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

	// Supplies the deletion watermarks, and learns of each metadata write that becomes durable.
	deleter deleter

	// The minimum time between the starts of metadata writes made by WriteIfDue().
	writeInterval time.Duration

	// The last metadata known to be crash durable.
	durableMetadata storeMetadata

	// True once the metadata object has been written.
	written bool

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
	// The minimum time between the starts of metadata writes made by WriteIfDue().
	writeInterval time.Duration,
	// The store's metadata as recovered, or the metadata of a new store.
	durableMetadata storeMetadata,
	// True if the store's metadata object exists.
	written bool,
) *metadataManager {
	return &metadataManager{
		writer:          writer,
		deleter:         deleter,
		writeInterval:   writeInterval,
		durableMetadata: durableMetadata,
		written:         written,
	}
}

// Snapshot returns the store's metadata as of now. It returns an error if the watermarks break a metadata invariant.
func (m *metadataManager) Snapshot(
	// The lowest key whose put may not be complete.
	durableWatermark uint64,
) (storeMetadata, error) {
	snapshot := m.durableMetadata
	err := snapshot.SetWatermarks(
		m.deleter.ScheduledDeletionWatermark(), m.deleter.DeletionWatermark(), durableWatermark)
	if err != nil {
		return storeMetadata{}, fmt.Errorf("update metadata: %w", err)
	}
	return snapshot, nil
}

// Collect records the metadata write in flight as durable if it has finished, and tells the deleter. It returns an
// error if the write failed.
func (m *metadataManager) Collect() error {
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
	m.written = true
	m.deleter.MetadataDurable(m.durableMetadata)
	return nil
}

// WriteIfDue starts a metadata write if the metadata has changed since the last durable write, no write is in flight,
// and the write interval has passed since the last write started.
func (m *metadataManager) WriteIfDue(
	// The lowest key whose put may not be complete.
	durableWatermark uint64,
) error {
	if m.inFlightWrite != nil || time.Since(m.lastWriteStart) < m.writeInterval {
		return nil
	}
	snapshot, err := m.Snapshot(durableWatermark)
	if err != nil {
		return fmt.Errorf("start a due metadata write: %w", err)
	}
	if snapshot != m.durableMetadata {
		m.start(snapshot)
	}
	return nil
}

// WriteIfChanged starts a metadata write, whatever the write interval, if the metadata has changed since the last
// durable write and no write is in flight.
func (m *metadataManager) WriteIfChanged(
	// The lowest key whose put may not be complete.
	durableWatermark uint64,
) (
	// True if no write is in flight and the durable metadata is current.
	current bool,
	err error,
) {
	if m.inFlightWrite != nil {
		return false, nil
	}
	snapshot, err := m.Snapshot(durableWatermark)
	if err != nil {
		return false, fmt.Errorf("start a metadata write: %w", err)
	}
	if snapshot == m.durableMetadata {
		return true, nil
	}
	m.start(snapshot)
	return false, nil
}

// WriteAndWait writes the metadata once any write in flight has finished, and waits until it is durable.
func (m *metadataManager) WriteAndWait(
	// The lowest key whose put may not be complete.
	durableWatermark uint64,
) error {
	if m.inFlightWrite != nil {
		// Metadata writes must land in the order they start. Its outcome is collected below.
		_ = m.inFlightWrite.Wait()
		if err := m.Collect(); err != nil {
			return fmt.Errorf("finish the metadata write in flight: %w", err)
		}
	}
	snapshot, err := m.Snapshot(durableWatermark)
	if err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	m.start(snapshot)
	_ = m.inFlightWrite.Wait()
	if err := m.Collect(); err != nil {
		return fmt.Errorf("wait for the metadata write: %w", err)
	}
	return nil
}

// Written reports whether the store's metadata object has been written.
func (m *metadataManager) Written() bool {
	return m.written
}

// start hands the writer a metadata write of snapshot, waiting while the writer's queue is full. No write may be in
// flight.
func (m *metadataManager) start(snapshot storeMetadata) {
	m.inFlightMetadata = snapshot
	m.lastWriteStart = time.Now()
	m.inFlightWrite = m.writer.WriteMetadata(snapshot.Serialize())
}

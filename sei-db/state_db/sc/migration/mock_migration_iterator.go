package migration

import (
	"bytes"
	"sort"
	"strings"
)

// MapMigrationIterator is a MigrationIterator backed by an in-memory map.
// Useful as a test double and as a reference implementation for validating
// test logic independently of any real DB.
//
// The underlying Data map may be mutated between NextBatch calls. If
// autoRebuild is true, the iterator automatically re-flattens and repositions
// before each NextBatch call. Otherwise, call Rebuild manually after mutating.
type MapMigrationIterator struct {
	Data        map[string]map[string][]byte
	autoRebuild bool
	entries     []ValueToMigrate
	position    int
	boundary    MigrationBoundary
}

var _ MigrationIterator = (*MapMigrationIterator)(nil)

// NewMapMigrationIterator creates a MapMigrationIterator from the given data,
// positioned at the start (boundary defaults to MigrationBoundaryNotStarted).
// If autoRebuild is true, the iterator re-reads from Data before every
// NextBatch call, so external mutations are picked up automatically.
func NewMapMigrationIterator(data map[string]map[string][]byte, autoRebuild bool) *MapMigrationIterator {
	m := &MapMigrationIterator{Data: data, autoRebuild: autoRebuild, boundary: MigrationBoundaryNotStarted}
	m.Rebuild()
	return m
}

func (m *MapMigrationIterator) SetBoundary(boundary MigrationBoundary) {
	m.boundary = boundary
	m.Rebuild()
}

// Rebuild re-flattens and re-sorts the Data map, then repositions the
// iterator so that the next NextBatch call resumes just past the current
// boundary. Call this after adding or removing entries from Data.
func (m *MapMigrationIterator) Rebuild() {
	m.entries = flattenAndSort(m.Data)
	m.position = computeStartPosition(m.entries, m.boundary)
}

func (m *MapMigrationIterator) NextBatch(size int) ([]ValueToMigrate, MigrationBoundary, error) {
	if m.autoRebuild {
		m.Rebuild()
	}
	if m.position >= len(m.entries) {
		m.boundary = MigrationBoundaryComplete
		return nil, MigrationBoundaryComplete, nil
	}

	end := m.position + size
	if end > len(m.entries) {
		end = len(m.entries)
	}

	batch := make([]ValueToMigrate, end-m.position)
	copy(batch, m.entries[m.position:end])
	m.position = end

	last := batch[len(batch)-1]
	m.boundary = NewMigrationBoundary(last.ModuleName, last.Key)
	return batch, m.boundary, nil
}

// flattenAndSort converts a nested map into a sorted slice of ValueToMigrate,
// ordered lexicographically by (ModuleName, Key).
func flattenAndSort(data map[string]map[string][]byte) []ValueToMigrate {
	totalSize := 0
	for _, kvs := range data {
		totalSize += len(kvs)
	}
	entries := make([]ValueToMigrate, 0, totalSize)
	for moduleName, kvs := range data {
		for k, v := range kvs {
			entries = append(entries, ValueToMigrate{
				ModuleName: moduleName,
				Key:        []byte(k),
				Value:      v,
			})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].ModuleName != entries[j].ModuleName {
			return strings.Compare(entries[i].ModuleName, entries[j].ModuleName) < 0
		}
		return bytes.Compare(entries[i].Key, entries[j].Key) < 0
	})
	return entries
}

// computeStartPosition returns the index of the first entry that has not yet
// been migrated according to the given boundary.
func computeStartPosition(entries []ValueToMigrate, boundary MigrationBoundary) int {
	if boundary.Status() == MigrationNotStarted {
		return 0
	}
	if boundary.Status() == MigrationComplete {
		return len(entries)
	}
	for i, e := range entries {
		if !boundary.IsMigrated(e.ModuleName, e.Key) {
			return i
		}
	}
	return len(entries)
}

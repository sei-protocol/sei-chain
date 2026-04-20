package migration

import (
	"fmt"
	"sort"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
)

// MemiavlMigrationIterator is a MigrationIterator that walks a memiavl.DB.
//
// The set of trees to migrate is fixed at construction time. Trees added
// to the DB after construction are not migrated; a tree that was present
// at construction but later disappears causes NextBatch to return an
// error.
//
// The reserved MigrationStore tree is always excluded: it holds migration
// metadata owned by MigrationManager and is not eligible for migration.
type MemiavlMigrationIterator struct {
	db        *memiavl.DB
	treeNames []string
	treeIdx   int
	boundary  MigrationBoundary
}

var _ MigrationIterator = (*MemiavlMigrationIterator)(nil)

// NewMemiavlMigrationIterator creates a MemiavlMigrationIterator positioned at
// the start of the given DB (boundary defaults to MigrationBoundaryNotStarted).
// Any tree named MigrationStore is filtered out and will never appear in a
// returned batch.
func NewMemiavlMigrationIterator(db *memiavl.DB) *MemiavlMigrationIterator {
	namedTrees := db.Trees()
	treeNames := make([]string, 0, len(namedTrees))
	for _, nt := range namedTrees {
		if nt.Name == MigrationStore {
			continue
		}
		treeNames = append(treeNames, nt.Name)
	}
	return &MemiavlMigrationIterator{
		db:        db,
		treeNames: treeNames,
		treeIdx:   0,
		boundary:  MigrationBoundaryNotStarted,
	}
}

func (m *MemiavlMigrationIterator) SetBoundary(boundary MigrationBoundary) {
	m.boundary = boundary
	m.treeIdx = computeStartTreeIndex(m.treeNames, boundary)
}

func (m *MemiavlMigrationIterator) NextBatch(size int) ([]ValueToMigrate, MigrationBoundary, error) {
	if size <= 0 {
		return nil, m.boundary, fmt.Errorf("batch size must be positive, got %d", size)
	}
	if m.boundary.Equals(MigrationBoundaryComplete) {
		return nil, MigrationBoundaryComplete, nil
	}

	batch := make([]ValueToMigrate, 0, size)
	firstKey := true

	for m.treeIdx < len(m.treeNames) && len(batch) < size {
		name := m.treeNames[m.treeIdx]
		tree := m.db.TreeByName(name)
		if tree == nil {
			return nil, m.boundary, fmt.Errorf("tree %q no longer exists in db", name)
		}

		var start []byte
		if firstKey && m.boundary.Status() == MigrationInProgress && m.boundary.ModuleName() == name {
			// tree.Iterator's start bound is inclusive, so append a 0x00
			// byte to get a start key strictly greater than boundary.Key().
			key := m.boundary.Key()
			start = make([]byte, len(key)+1)
			copy(start, key)
		}
		firstKey = false

		iter := tree.Iterator(start, nil, true)
		for ; iter.Valid() && len(batch) < size; iter.Next() {
			batch = append(batch, ValueToMigrate{
				ModuleName: name,
				Key:        copyBytes(iter.Key()),
				Value:      copyBytes(iter.Value()),
			})
		}
		exhausted := !iter.Valid()
		if err := iter.Close(); err != nil {
			return nil, m.boundary, fmt.Errorf("failed to close tree iterator for %s: %w", name, err)
		}

		if exhausted {
			m.treeIdx++
		}
	}

	if len(batch) == 0 {
		m.boundary = MigrationBoundaryComplete
		return nil, MigrationBoundaryComplete, nil
	}

	last := batch[len(batch)-1]
	m.boundary = NewMigrationBoundary(last.ModuleName, last.Key)
	return batch, m.boundary, nil
}

// computeStartTreeIndex returns the index of the first tree that may contain
// unmigrated keys according to the given boundary.
func computeStartTreeIndex(treeNames []string, boundary MigrationBoundary) int {
	switch boundary.Status() {
	case MigrationNotStarted:
		return 0
	case MigrationComplete:
		return len(treeNames)
	}
	return sort.Search(len(treeNames), func(i int) bool {
		return treeNames[i] >= boundary.ModuleName()
	})
}

// copyBytes returns a newly allocated copy of b, or nil if b is nil.
func copyBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

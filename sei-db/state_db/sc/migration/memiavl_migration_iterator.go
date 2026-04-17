package migration

import (
	"sort"
	"strings"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
)

// MemiavlMigrationIterator is a MigrationIterator that walks a memiavl.DB in
// lexicographic (moduleName, key) order. It caches the tree list at
// construction time and creates/closes short-lived tree iterators within each
// NextBatch call, so no resources are held between calls.
type MemiavlMigrationIterator struct {
	// Snapshot of the DB's trees, cached at construction time and assumed static
	// (i.e. assumed that trees are not added/removed)
	trees []memiavl.NamedTree
	// Index into trees for the next tree to scan.
	treeIdx int
	// Tracks how far the migration has progressed; updated after each NextBatch.
	boundary MigrationBoundary
}

var _ MigrationIterator = (*MemiavlMigrationIterator)(nil)

// NewMemiavlMigrationIterator creates a MemiavlMigrationIterator that will
// walk the given DB starting just past the provided boundary.
func NewMemiavlMigrationIterator(
	db *memiavl.DB,
	boundary MigrationBoundary,
) *MemiavlMigrationIterator {
	trees := db.Trees()
	treeIdx := computeStartTreeIndex(trees, boundary)
	return &MemiavlMigrationIterator{
		trees:    trees,
		treeIdx:  treeIdx,
		boundary: boundary,
	}
}

func (m *MemiavlMigrationIterator) NextBatch(size int) ([]ValueToMigrate, MigrationBoundary, error) {
	if m.boundary.Equals(MigrationBoundaryComplete) {
		return nil, MigrationBoundaryComplete, nil
	}

	batch := make([]ValueToMigrate, 0, size)
	firstKey := true

	for m.treeIdx < len(m.trees) && len(batch) < size {
		tree := m.trees[m.treeIdx]

		var start []byte
		if firstKey && m.boundary.Status() == MigrationInProgress && m.boundary.ModuleName() == tree.Name {
			// Start just past the boundary key. Appending 0x00 makes the
			// start key strictly greater than boundary.Key() because the
			// memiavl iterator's start bound is inclusive.
			start = append(m.boundary.Key(), 0x00) //nolint:gocritic // intentional append to copy
		}
		firstKey = false

		iter := tree.Iterator(start, nil, true)
		for ; iter.Valid() && len(batch) < size; iter.Next() {
			batch = append(batch, ValueToMigrate{
				ModuleName: tree.Name,
				Key:        copyBytes(iter.Key()),
				Value:      copyBytes(iter.Value()),
			})
		}
		exhausted := !iter.Valid()
		_ = iter.Close()

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
func computeStartTreeIndex(trees []memiavl.NamedTree, boundary MigrationBoundary) int {
	if boundary.Status() == MigrationNotStarted {
		return 0
	}
	if boundary.Status() == MigrationComplete {
		return len(trees)
	}
	return sort.Search(len(trees), func(i int) bool {
		return strings.Compare(trees[i].Name, boundary.ModuleName()) >= 0
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

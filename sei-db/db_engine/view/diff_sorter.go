package view

import (
	"fmt"
)

// materializeDiffAtVersion orders a sealed version's writes by key, per shard, on the sort pool.
//
// Work done ahead of the flush rather than for it: the flush calls materializeSortedDiffs itself and so
// finds the result already published, or waits for it. That is also why a failure here is dropped — the
// flush repeats the same call and reports it.
//
// Must be called without versionLock held: submitting can block when the pool's queue is full.
func (c *viewManager) materializeDiffAtVersion(version uint64) {
	for _, shard := range c.shards {
		c.sortPool.Submit(func() {
			_ = shard.MaterializeSortedDiff(version)
		})
	}
}

// materializeSortedDiffs materializes one version on every shard and collects the resulting diffs,
// waiting for any shard another caller is already materializing.
func (c *viewManager) materializeSortedDiffs(version uint64) ([][]Write, error) {
	shardDiffs := make([][]Write, len(c.shards))
	for i, shard := range c.shards {
		if err := shard.MaterializeSortedDiff(version); err != nil {
			return nil, fmt.Errorf("failed to materialize shard %d at version %d: %w", i, version, err)
		}
		diff, err := shard.SortedDiff(version)
		if err != nil {
			return nil, fmt.Errorf("failed to read the diff of shard %d at version %d: %w", i, version, err)
		}
		shardDiffs[i] = diff
	}
	return shardDiffs, nil
}

// forEachMergedEntry walks every entry across a version's per-shard diffs in ascending key order.
func forEachMergedEntry(shardDiffs [][]Write, visit func(entry Write) error) error {
	merge := newDiffMerge(shardDiffs)
	for {
		entry, ok := merge.next()
		if !ok {
			return nil
		}
		if err := visit(entry); err != nil {
			return err
		}
	}
}

// diffMerge yields the entries of disjoint, key-ordered diffs in ascending key order, keeping each
// diff's head in a min-heap.
type diffMerge struct {
	diffs   [][]Write
	cursors []int
	// heap holds the indices of the diffs not yet exhausted, ordered by their head's key.
	heap []int
}

func newDiffMerge(diffs [][]Write) *diffMerge {
	m := &diffMerge{diffs: diffs, cursors: make([]int, len(diffs)), heap: make([]int, 0, len(diffs))}
	for i, diff := range diffs {
		if len(diff) > 0 {
			m.heap = append(m.heap, i)
		}
	}
	for i := len(m.heap)/2 - 1; i >= 0; i-- {
		m.down(i)
	}
	return m
}

// next returns the smallest remaining entry, or false once every diff is exhausted.
func (m *diffMerge) next() (Write, bool) {
	if len(m.heap) == 0 {
		return Write{}, false
	}
	top := m.heap[0]
	entry := m.diffs[top][m.cursors[top]]
	m.cursors[top]++
	if m.cursors[top] == len(m.diffs[top]) {
		last := len(m.heap) - 1
		m.heap[0] = m.heap[last]
		m.heap = m.heap[:last]
	}
	if len(m.heap) > 0 {
		m.down(0)
	}
	return entry, true
}

func (m *diffMerge) head(heapIndex int) string {
	diff := m.heap[heapIndex]
	return m.diffs[diff][m.cursors[diff]].Key
}

func (m *diffMerge) down(i int) {
	for {
		smallest := i
		if left := 2*i + 1; left < len(m.heap) && m.head(left) < m.head(smallest) {
			smallest = left
		}
		if right := 2*i + 2; right < len(m.heap) && m.head(right) < m.head(smallest) {
			smallest = right
		}
		if smallest == i {
			return
		}
		m.heap[i], m.heap[smallest] = m.heap[smallest], m.heap[i]
		i = smallest
	}
}

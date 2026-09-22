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
	c.sortPool.Submit(func() {
		_, _ = c.materializeSortedDiffs(version)
	})
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
	cursors := make([]int, len(shardDiffs))

	for {
		// The smallest head is scanned for rather than kept in a heap: the diffs are disjoint, there are
		// ShardCount of them, and a handful of comparisons per key beats maintaining one. Worth
		// revisiting if a deployment raises the shard count by an order of magnitude.
		next := -1
		for i, diff := range shardDiffs {
			if cursors[i] >= len(diff) {
				continue
			}
			if next == -1 || diff[cursors[i]].Key < shardDiffs[next][cursors[next]].Key {
				next = i
			}
		}
		if next == -1 {
			return nil
		}

		entry := shardDiffs[next][cursors[next]]
		cursors[next]++
		if err := visit(entry); err != nil {
			return err
		}
	}
}

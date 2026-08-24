package snapshot

import (
	"fmt"
	"slices"
	"strings"
)

// diffEntry is one write from a version's diff. A nil value is a tombstone, matching the convention in
// the shards' diff maps. The key is a string because that is how the shards hold it; the conversion to
// bytes happens once, at the batch.
type diffEntry struct {
	key   string
	value []byte
}

// sortedDiffResult is a version's writes ordered by key, or the reason they could not be gathered.
type sortedDiffResult struct {
	entries []diffEntry
	err     error
}

// sortDiffAtVersion orders a sealed version's writes by key on the sort pool, delivering them to the
// flush through the version's sortedDiff channel.
//
// Ordering the writes is what makes them cheap for pebble to absorb. Pebble's memtable is a skiplist that
// caches the splice it last inserted at and reuses it when the next key falls inside; a key outside it
// restarts the descent from the top of the list. Arriving in map order made every insert a full-height
// descent through a structure too large to cache.
//
// Must be called without versionLock held: submitting can block when the pool's queue is full, and the
// queue drains only as the flush consumes results, which needs that lock.
func (c *snapshotEngine) sortDiffAtVersion(version uint64) {
	counter := c.sortedDiffChannel(version)
	if counter == nil {
		return
	}

	c.sortPool.Submit(func() {
		entries, err := c.gatherSortedDiff(version)
		counter <- sortedDiffResult{entries: entries, err: err}
	})
}

// awaitSortedDiff returns a version's writes ordered by key, waiting for the sort pool to finish them if
// it has not already.
//
// Reports an error rather than blocking forever if the engine shuts down while waiting, matching every
// other blocked wait in the engine: a sort job that never answers must not wedge the flush.
func (c *snapshotEngine) awaitSortedDiff(version uint64) ([]diffEntry, error) {
	channel := c.sortedDiffChannel(version)
	if channel == nil {
		return nil, fmt.Errorf("version %d is no longer tracked, cannot flush it", version)
	}

	select {
	case result := <-channel:
		if result.err != nil {
			return nil, fmt.Errorf("failed to sort diff at version %d: %w", version, result.err)
		}
		return result.entries, nil
	case <-c.ctx.Done():
		return nil, fmt.Errorf("engine shut down while awaiting the sorted diff at version %d: %w",
			version, c.shutdownError())
	}
}

// sortedDiffChannel returns the channel a version's sorted writes are delivered on, or nil if the version
// is no longer tracked.
func (c *snapshotEngine) sortedDiffChannel(version uint64) chan sortedDiffResult {
	c.versionLock.Lock()
	defer c.versionLock.Unlock()

	counter, ok := c.versionMap[version]
	if !ok {
		return nil
	}
	return counter.sortedDiff
}

// gatherSortedDiff collects every shard's writes at one version and orders them by key.
//
// No deduplication is needed or possible: a key belongs to exactly one shard, so the shards' diffs are
// disjoint. Comparison is bytewise to match pebble's default comparer, which is what decides whether an
// ascending run is actually ascending as far as the memtable is concerned.
func (c *snapshotEngine) gatherSortedDiff(version uint64) ([]diffEntry, error) {
	// Every shard's diff is taken first so the slice can be sized exactly, rather than growing as it
	// fills. Each shard is locked once.
	diffs := make([]map[string][]byte, len(c.shards))
	total := 0
	for i, shard := range c.shards {
		shardDiffs, err := shard.GetDiffsForVersions(version, version+1)
		if err != nil {
			return nil, fmt.Errorf("failed to get diff for shard %d at version %d: %w", i, version, err)
		}
		diffs[i] = shardDiffs[0]
		total += len(shardDiffs[0])
	}

	entries := make([]diffEntry, 0, total)
	for _, diff := range diffs {
		for key, value := range diff {
			entries = append(entries, diffEntry{key: key, value: value})
		}
	}

	slices.SortFunc(entries, func(a diffEntry, b diffEntry) int {
		return strings.Compare(a.key, b.key)
	})

	return entries, nil
}

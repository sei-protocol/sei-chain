package view

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// batchRecordOverhead is the framing pebble puts around one record in a batch: a kind byte, and a
// uvarint length for the key and for the value. Taken at the widest a uvarint length can be, so the
// estimate it feeds is never short and the batch it sizes never has to grow.
const batchRecordOverhead = 1 + 2*binary.MaxVarintLen32

// sortedDiffResult is a version's writes, ordered by key and already encoded into a batch for the flush
// to absorb, or the failure that stopped them being gathered.
type sortedDiffResult struct {
	batch types.Batch
	err   error
}

// sortDiffAtVersion materializes a sealed version's writes as an ordered diff per shard on the sort pool
// and encodes them into a batch, delivering it to the flush through the version's sortedDiff channel.
// Must be called without versionLock held.
func (c *viewManager) sortDiffAtVersion(version uint64) {
	channel := c.sortedDiffChannel(version)

	// Blocks when the pool's queue is full, and the queue drains only as the flush consumes results,
	// which needs versionLock: holding it here would deadlock against the work that would free it.
	c.sortPool.Submit(func() {
		batch, err := c.buildSortedDiffBatch(version)
		channel <- sortedDiffResult{batch: batch, err: err}
	})
}

// awaitSortedDiff returns the batch holding a version's writes in key order, waiting for the sort pool to
// finish it if it has not already, or an error if the manager shuts down first. The batch belongs to the
// caller, which must close it.
func (c *viewManager) awaitSortedDiff(version uint64) (types.Batch, error) {
	channel := c.sortedDiffChannel(version)

	select {
	case result := <-channel:
		if result.err != nil {
			return nil, fmt.Errorf("failed to sort diff at version %d: %w", version, result.err)
		}
		return result.batch, nil
	case <-c.ctx.Done():
		return nil, fmt.Errorf("manager shut down while awaiting the sorted diff at version %d: %w",
			version, c.shutdownError())
	}
}

// sortedDiffChannel returns the channel a version's sorted writes are delivered on. The version must be
// one the manager is tracking.
func (c *viewManager) sortedDiffChannel(version uint64) chan sortedDiffResult {
	c.versionLock.Lock()
	defer c.versionLock.Unlock()

	return c.versionMap[version].sortedDiff
}

// buildSortedDiffBatch encodes one version's writes into a batch, ordered by key across every shard.
func (c *viewManager) buildSortedDiffBatch(version uint64) (types.Batch, error) {
	shardDiffs, err := c.materializeSortedDiffs(version)
	if err != nil {
		return nil, err
	}

	batch := c.db.NewBatchWithSize(encodedSizeOf(shardDiffs))
	err = forEachMergedEntry(shardDiffs, func(entry diffEntry) error {
		if entry.value == nil {
			return batch.DeleteString(entry.key)
		}
		return batch.SetString(entry.key, entry.value)
	})
	if err != nil {
		_ = batch.Close()
		return nil, fmt.Errorf("failed to encode the diff at version %d: %w", version, err)
	}

	return batch, nil
}

// encodedSizeOf estimates the bytes a version's diffs occupy once encoded into a batch.
func encodedSizeOf(shardDiffs [][]diffEntry) int {
	size := 0
	for _, diff := range shardDiffs {
		size += len(diff) * batchRecordOverhead
		for _, entry := range diff {
			size += len(entry.key) + len(entry.value)
		}
	}
	return size
}

// materializeSortedDiffs materializes one version on every shard and collects the resulting diffs.
func (c *viewManager) materializeSortedDiffs(version uint64) ([][]diffEntry, error) {
	shardDiffs := make([][]diffEntry, len(c.shards))
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
func forEachMergedEntry(shardDiffs [][]diffEntry, visit func(entry diffEntry) error) error {
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
			if next == -1 || diff[cursors[i]].key < shardDiffs[next][cursors[next]].key {
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

package view

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

// versionMutations is one sealed version's writes, sorted by key, each carrying the value its key held in
// the version before. The fields other than ready may be read only once ready is closed.
type versionMutations struct {
	// Closed once the mutations are published. Never closed if building them failed, which bricks the
	// manager.
	ready chan struct{}

	// The version's writes in ascending key order.
	mutations []gigatypes.Mutation

	// For each shard, the positions in mutations of its keys, ascending.
	shardPositions [][]uint32
}

// materializeMutations returns a sealed version's mutations, built in the background on the sort pool:
// every shard's writes are extracted, sorted by key, and given the value each key held in the version before.
// They are published once ready is closed. A failure bricks the manager, and they are never published.
//
// Must be called without versionLock held: submitting can block when the sort pool's queue is full.
func (c *viewManager) materializeMutations(version uint64) *versionMutations {
	out := &versionMutations{ready: make(chan struct{})}
	c.materializationsInProgress.Add(1)

	c.sortPool.Submit(func() {
		defer c.materializationsInProgress.Done()

		shardWrites := make([]map[string][]byte, len(c.shards))
		for i, s := range c.shards {
			writes, err := s.ExtractMaterializedWrites(version)
			if err != nil {
				c.brick(fmt.Errorf("materialize version %d: extract the writes of shard %d: %w",
					version, i, err))
				return
			}
			shardWrites[i] = writes
		}

		mutations, err := sortWrites(shardWrites)
		if err != nil {
			c.brick(fmt.Errorf("materialize version %d: %w", version, err))
			return
		}
		shardPositions := c.indexByShard(mutations, shardWrites)
		if err := c.readPriorValues(version, mutations, shardPositions); err != nil {
			c.brick(fmt.Errorf("materialize version %d: %w", version, err))
			return
		}

		out.mutations = mutations
		out.shardPositions = shardPositions

		c.versionLock.Lock()
		// The prior values were read from the version before, which is held back from retirement until
		// now.
		if predecessor, tracked := c.versionMap[version-1]; tracked {
			predecessor.successorMaterialized = true
			c.maybeWakeLifecycleLocked()
		}
		c.versionLock.Unlock()

		close(out.ready)
	})

	return out
}

// sortWrites merges every shard's writes into one slice of mutations, sorted by key.
func sortWrites(shardWrites []map[string][]byte) ([]gigatypes.Mutation, error) {
	total := 0
	for _, writes := range shardWrites {
		total += len(writes)
	}
	if total > math.MaxUint32 {
		return nil, fmt.Errorf("%d writes are more than a shard position can address", total)
	}

	mutations := make([]gigatypes.Mutation, 0, total)
	for _, writes := range shardWrites {
		for key, value := range writes {
			mutations = append(mutations, gigatypes.NewMutation(key, value, nil))
		}
	}
	// Bytewise, to match pebble's default comparer: the flush writes in this order, and pebble absorbs an
	// ascending batch far more cheaply, because its memtable caches the splice it last inserted at.
	slices.SortFunc(mutations, func(a gigatypes.Mutation, b gigatypes.Mutation) int {
		return strings.Compare(a.Key(), b.Key())
	})
	return mutations, nil
}

// indexByShard returns, for each shard, the positions in mutations of the keys it wrote.
func (c *viewManager) indexByShard(mutations []gigatypes.Mutation, shardWrites []map[string][]byte) [][]uint32 {
	positions := make([]uint32, len(mutations))
	shardPositions := make([][]uint32, len(shardWrites))
	start := 0
	for i, writes := range shardWrites {
		end := start + len(writes)
		// Empty, with capacity for exactly this shard's keys, so the appends below fill its part of
		// positions in place.
		shardPositions[i] = positions[start:start:end]
		start = end
	}

	for i := range mutations {
		shardIndex := c.shardManager.ShardString(mutations[i].Key())
		position := uint32(i) //nolint:gosec // sortWrites bounds the count
		shardPositions[shardIndex] = append(shardPositions[shardIndex], position)
	}
	return shardPositions
}

// readPriorValues reads every mutation's prior value, each shard's keys in their own task on the misc pool,
// and returns once all of them are read.
func (c *viewManager) readPriorValues(version uint64, mutations []gigatypes.Mutation, shardPositions [][]uint32) error {
	errs := make([]error, len(c.shards))
	var wg sync.WaitGroup
	wg.Add(len(c.shards))
	for i, s := range c.shards {
		c.miscPool.Submit(func() {
			defer wg.Done()
			if err := s.ReadPriorValues(version, mutations, shardPositions[i]); err != nil {
				errs[i] = fmt.Errorf("read the prior values of shard %d: %w", i, err)
			}
		})
	}
	wg.Wait()
	return errors.Join(errs...)
}

// awaitMutations returns a version's mutations once they are published.
func (c *viewManager) awaitMutations(version uint64, m *versionMutations) ([]gigatypes.Mutation, error) {
	select {
	case <-m.ready:
		return m.mutations, nil
	case <-c.ctx.Done():
		return nil, fmt.Errorf("view manager shut down while awaiting the mutations at version %d: %w",
			version, c.shutdownError())
	}
}

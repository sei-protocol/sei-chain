package lthash

import (
	"context"
	"fmt"
	"sync"
)

// hashCombiner sums each block's leaf hashes onto the block before it and publishes the result.
type hashCombiner struct {
	// dbNames is the canonical set of data databases, so every result describes all of them.
	dbNames []string

	// combined is the hash state as of the most recently combined block, or the seed before any block
	// has been combined.
	combined *BlockHash

	// Gathered blocks arrive here, in block order.
	combineJobChan <-chan any

	// When a block is fully hashed, the result is put onto this channel.
	blockHashChan chan *BlockHash

	// Cancelled when the engine is stopping, to release a publish that nobody is reading.
	ctx context.Context

	// brick latches a failure on the engine, which reports it from Close().
	brick func(error)

	// wg tracks run(), so that the engine can wait for it to return.
	wg sync.WaitGroup
}

func newHashCombiner(
	// The canonical set of database names, so that every hash describes all of them.
	dbNames []string,
	// The hash state the first block is summed onto.
	runningHash *BlockHash,
	// Gathered blocks arrive here, in block order.
	combineJobChan <-chan any,
	// Cancelled when the engine is stopping, to release a publish that nobody is reading.
	ctx context.Context,
	// Depth of the channel finished hashes are published on.
	hashChanSize uint32,
	// Latches a failure on the engine, which reports it from Close().
	brick func(error),
) *hashCombiner {
	c := &hashCombiner{
		dbNames:        append([]string(nil), dbNames...),
		combined:       runningHash,
		combineJobChan: combineJobChan,
		blockHashChan:  make(chan *BlockHash, hashChanSize),
		ctx:            ctx,
		brick:          brick,
	}
	c.wg.Go(c.run)
	return c
}

// run combines blocks until the gatherer stops sending them.
func (c *hashCombiner) run() {
	defer close(c.blockHashChan)

	publishing := true
	for job := range c.combineJobChan {
		switch job := job.(type) {
		case *gatheredBlock:
			if publishing {
				publishing = c.combineBlock(job)
			}
		case *flushRequest:
			close(job.doneChan)
		default:
			// Bricked rather than stopped: the gatherer's send cannot be abandoned, so this goroutine
			// has to keep draining combineJobChan until it closes. Close() reports the latched error.
			c.brick(fmt.Errorf("unknown combine job type %T", job))
		}
	}
}

// combineBlock waits for one block's leaf hashes, sums them onto the running state, and publishes it,
// reporting whether the stream may carry on.
func (c *hashCombiner) combineBlock(job *gatheredBlock) bool {
	if job.err != nil {
		// The running hashes describe nothing trustworthy once a block has failed, so no later block
		// may be derived from them.
		c.publish(&BlockHash{BlockNumber: job.blockNumber, Error: job.err})
		c.brick(job.err)
		return false
	}

	deltas := make(map[ModuleKey]*ModuleHashInfo)
	for i := 0; i < job.hashes.count; i++ {
		result := <-job.hashes.resultChan
		if acc := deltas[result.key]; acc != nil {
			mergeDelta(acc, result.info)
		} else {
			deltas[result.key] = result.info
		}
	}

	c.combined = combine(
		c.dbNames,
		deltas,
		c.combined.PerDB,
		c.combined.PerModule,
		c.combined.PerModuleStats)
	c.combined.BlockNumber = job.blockNumber

	return c.publish(c.combined)
}

// publish hands a finished hash to whoever is reading AwaitHash(), reporting whether it was taken. It
// gives up if the engine is stopping, since a stopped engine has no reader left to hand it to.
func (c *hashCombiner) publish(hash *BlockHash) bool {
	select {
	case c.blockHashChan <- hash:
		return true
	case <-c.ctx.Done():
		return false
	}
}

// combine folds deltas onto the previous block's hashes and derives the rest: each touched per-DB
// root as the homomorphic sum of its module hashes, and the global root as the sum of the per-DB roots.
//
// The returned maps are freshly allocated, cloned from prev, so the caller may hold the result while
// later blocks are folded. A nil or empty delta map carries the previous hashes forward unchanged.
//
// MixIn and MixOut are commutative and associative, so the result is identical to a single serial fold:
// the global store hash, and so the consensus AppHash, does not depend on worker count or chunking.
func combine(
	// The canonical set of data databases, so that every one has an entry in the result even if this
	// block did not touch it, and the caller can swap the maps in wholesale.
	dbNames []string,
	// This block's change to each (database, module), or nil for a block that changed nothing.
	deltas map[ModuleKey]*ModuleHashInfo,
	prevPerDB map[string]*LtHash,
	prevPerModule map[string]map[string]*LtHash,
	prevPerModuleStats map[string]map[string]ModuleStats,
) *BlockHash {
	newPerDB := make(map[string]*LtHash, len(dbNames))
	newPerModule := make(map[string]map[string]*LtHash, len(dbNames))
	newPerModuleStats := make(map[string]map[string]ModuleStats, len(dbNames))
	for _, dbName := range dbNames {
		if h := prevPerDB[dbName]; h != nil {
			newPerDB[dbName] = h.Clone()
		} else {
			newPerDB[dbName] = New()
		}
		newPerModule[dbName] = cloneModuleMap(prevPerModule[dbName])
		newPerModuleStats[dbName] = cloneModuleStatsMap(prevPerModuleStats[dbName])
	}

	touched := make(map[string]struct{}, len(dbNames))
	for key, delta := range deltas {
		modBucket := newPerModule[key.DBName]
		statBucket := newPerModuleStats[key.DBName]
		if modBucket == nil {
			// Defensive: a database not in dbNames still gets buckets so the delta
			// is not silently dropped.
			modBucket = make(map[string]*LtHash)
			newPerModule[key.DBName] = modBucket
			statBucket = make(map[string]ModuleStats)
			newPerModuleStats[key.DBName] = statBucket
		}
		cur := modBucket[key.Module]
		if cur == nil {
			cur = New()
			modBucket[key.Module] = cur
		}
		cur.MixIn(delta.Hash)
		statBucket[key.Module] = statBucket[key.Module].Add(
			ModuleStats{KeyCount: delta.KeyCount, Bytes: delta.Bytes})
		touched[key.DBName] = struct{}{}
	}
	for dbName := range touched {
		newPerDB[dbName] = SumModuleHashes(newPerModule[dbName])
	}

	return &BlockHash{
		PerDB:          newPerDB,
		PerModule:      newPerModule,
		PerModuleStats: newPerModuleStats,
		Global:         SumDBHashes(dbNames, newPerDB),
	}
}

// NewBlockHash returns the hash state of a store that has hashed nothing: an identity hash for every
// database in dbNames, and an identity global root.
func NewBlockHash(dbNames []string) *BlockHash {
	return combine(dbNames, nil, nil, nil, nil)
}

// SumDBHashes returns the store-wide root: the homomorphic sum of every data database's per-DB root.
func SumDBHashes(
	// The canonical set of data databases. Summing over this rather than over perDB is what makes a
	// database missing from perDB contribute the identity rather than be skipped silently.
	dbNames []string,
	perDB map[string]*LtHash,
) *LtHash {
	global := New()
	for _, dbName := range dbNames {
		if h := perDB[dbName]; h != nil {
			global.MixIn(h)
		}
	}
	return global
}

// SumModuleHashes returns the homomorphic sum of a DB's per-module hashes, i.e.
// its derived per-DB root. A nil/empty map yields the identity hash.
func SumModuleHashes(moduleHashes map[string]*LtHash) *LtHash {
	root := New()
	for _, h := range moduleHashes {
		if h != nil {
			root.MixIn(h)
		}
	}
	return root
}

// cloneModuleMap deep-copies a per-module hash map (cloning each LtHash). A
// nil/empty source yields a fresh empty map.
func cloneModuleMap(src map[string]*LtHash) map[string]*LtHash {
	dst := make(map[string]*LtHash, len(src))
	for module, h := range src {
		if h != nil {
			dst[module] = h.Clone()
		}
	}
	return dst
}

// cloneModuleStatsMap copies a per-module stats map. ModuleStats is a value
// type, so a shallow per-entry copy is a full copy. A nil/empty source yields a
// fresh empty map.
func cloneModuleStatsMap(src map[string]ModuleStats) map[string]ModuleStats {
	dst := make(map[string]ModuleStats, len(src))
	for module, s := range src {
		dst[module] = s
	}
	return dst
}

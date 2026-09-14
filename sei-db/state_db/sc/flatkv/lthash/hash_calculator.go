package lthash

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
)

const (
	// computeChunkSize is the number of KV pairs one task carries. Splitting a
	// module's pairs into fixed-size chunks lets a single large module (e.g. the
	// EVM storage DB in a big block) fan out across many workers instead of
	// pinning one. Small enough to balance load, large enough to amortize the
	// per-task scheduling overhead and result bookkeeping.
	computeChunkSize = 100

	// parallelThreshold is the minimum total pair count before the worker pool
	// is engaged. Below it, the pool hand-off + merge overhead outweighs the
	// parallelism, so the delta is computed inline on the caller goroutine. Kept
	// a multiple of computeChunkSize (>= 2x) so that any batch which does go
	// parallel splits into several chunks rather than paying the pool tax to run
	// a single chunk on one worker.
	parallelThreshold = 1000
)

// ModuleFunc extracts the owning module name from a physical key. Injected by
// the caller so the HashCalculator stays decoupled from the key-encoding package.
type ModuleFunc func(physicalKey []byte) (module string, err error)

// DBPairs couples a data DB dir with the LtHash pairs to fold into it this
// block.
type DBPairs struct {
	Dir   string
	Pairs []KVPairWithLastValue
}

// Result holds the recomputed hash state after folding a block's pairs. PerDB
// and PerModule contain an entry for every DB dir the HashCalculator was
// configured with (so callers can swap them in wholesale). Global is the
// homomorphic sum of the per-DB roots. PerModuleStats holds the per-(dir,
// module) key-count / byte totals accumulated alongside the hash.
type Result struct {
	PerDB          map[string]*LtHash
	PerModule      map[string]map[string]*LtHash
	PerModuleStats map[string]map[string]ModuleStats
	Global         *LtHash
}

// HashCalculator encapsulates the per-block lattice-hash pipeline over an
// injected CPU-bound worker pool:
//
// Compute hashes individual keys and combines the per-worker results into the
// final per-module hashes, then derives each per-DB root and the global hash
// from those. Callers supply the key/old-value/new-value triples; reading the
// old values is not this package's job.
//
// The pool is supplied by the caller (the FlatKV store) rather than created
// here. The HashCalculator does not own the pool and never closes it; pool
// lifecycle is the caller's responsibility.
//
// The pool distributes independent per-chunk tasks, so ComputeModuleHashInfos is
// safe to call concurrently from multiple goroutines that share one
// HashCalculator (the state-sync importer runs a goroutine per DB). The live
// commit path is additionally serialized by FlatKV's write lock.
type HashCalculator struct {
	pool     threading.Pool
	dbDirs   []string
	moduleOf ModuleFunc
}

// NewHashCalculator creates a HashCalculator that runs on the provided pool.
// dbDirs is the canonical, ordered set of data DB directories; moduleOf extracts
// a physical key's owning module. The pool is owned by the caller — closing it
// is the caller's responsibility, not the HashCalculator's.
func NewHashCalculator(pool threading.Pool, dbDirs []string, moduleOf ModuleFunc) *HashCalculator {
	return &HashCalculator{
		pool:     pool,
		dbDirs:   append([]string(nil), dbDirs...),
		moduleOf: moduleOf,
	}
}

// ModuleKey identifies a single (data DB dir, module) accumulator.
type ModuleKey struct {
	Dir    string
	Module string
}

// Compute folds pairSets into the previous hashes and derives the full result:
// per-module hashes (via ComputeModuleHashInfos), each touched per-DB root as the
// homomorphic sum of its module hashes, and the global hash as the sum of the
// per-DB roots.
//
// The returned maps are freshly allocated (cloned from prev), so the caller can
// swap them in without aliasing. Because MixIn/MixOut are commutative and
// associative, the result is identical to a single serial fold — the global
// store hash (and consensus AppHash) is independent of worker count or chunking.
//
// Used by the live commit path, which maintains a running per-DB/per-module
// hash (and per-module stats) across blocks.
func (c *HashCalculator) Compute(
	pairSets []DBPairs,
	prevPerDB map[string]*LtHash,
	prevPerModule map[string]map[string]*LtHash,
	prevPerModuleStats map[string]map[string]ModuleStats,
) (*Result, error) {
	newPerDB := make(map[string]*LtHash, len(c.dbDirs))
	newPerModule := make(map[string]map[string]*LtHash, len(c.dbDirs))
	newPerModuleStats := make(map[string]map[string]ModuleStats, len(c.dbDirs))
	for _, dir := range c.dbDirs {
		if h := prevPerDB[dir]; h != nil {
			newPerDB[dir] = h.Clone()
		} else {
			newPerDB[dir] = New()
		}
		newPerModule[dir] = cloneModuleMap(prevPerModule[dir])
		newPerModuleStats[dir] = cloneModuleStatsMap(prevPerModuleStats[dir])
	}

	deltas, err := c.ComputeModuleHashInfos(pairSets)
	if err != nil {
		return nil, err
	}

	touched := make(map[string]struct{}, len(c.dbDirs))
	for key, delta := range deltas {
		modBucket := newPerModule[key.Dir]
		statBucket := newPerModuleStats[key.Dir]
		if modBucket == nil {
			// Defensive: a DB dir not in c.dbDirs still gets buckets so the
			// delta is not silently dropped.
			modBucket = make(map[string]*LtHash)
			newPerModule[key.Dir] = modBucket
			statBucket = make(map[string]ModuleStats)
			newPerModuleStats[key.Dir] = statBucket
		}
		cur := modBucket[key.Module]
		if cur == nil {
			cur = New()
			modBucket[key.Module] = cur
		}
		cur.MixIn(delta.Hash)
		statBucket[key.Module] = statBucket[key.Module].Add(ModuleStats{KeyCount: delta.KeyCount, Bytes: delta.Bytes})
		touched[key.Dir] = struct{}{}
	}
	for dir := range touched {
		newPerDB[dir] = SumModuleHashes(newPerModule[dir])
	}

	global := New()
	for _, dir := range c.dbDirs {
		global.MixIn(newPerDB[dir])
	}

	return &Result{
		PerDB:          newPerDB,
		PerModule:      newPerModule,
		PerModuleStats: newPerModuleStats,
		Global:         global,
	}, nil
}

// ModuleHashInfo is the per-(dir, module) change computed for one block/batch:
// the homomorphic hash delta plus the net key-count and byte deltas implied by
// the same MixIn/MixOut transitions.
type ModuleHashInfo struct {
	Hash     *LtHash
	KeyCount int64
	Bytes    int64
}

// ComputeModuleHashInfos is the shared per-module hashing primitive used by both
// the live commit path (via Compute) and the state-sync importer. It processes
// the changeset pairs identically for both: bucket each DB's pairs by module,
// split every bucket into fixed-size chunks, and distribute those chunks across
// the shared worker pool to compute the per-(dir, module) homomorphic hash delta
// and the accompanying key-count / byte deltas.
//
// Each chunk is an independent, self-terminating task, so ComputeModuleHashInfos is
// safe to call concurrently from multiple goroutines sharing one pool (the
// importer runs a goroutine per DB). It never holds a worker while waiting on
// another task, so no oversubscription or deadlock can arise from the nesting.
//
// The caller decides how to apply the deltas: Compute mixes them onto a running
// per-block hash; the importer folds them into its per-DB accumulators.
func (c *HashCalculator) ComputeModuleHashInfos(pairSets []DBPairs) (map[ModuleKey]*ModuleHashInfo, error) {
	// The tasks below slice the scratch's backing arrays, so returning it to the pool is
	// only safe because both compute paths join every task before they return.
	scratch := moduleBucketPool.Get().(*moduleBuckets)
	defer moduleBucketPool.Put(scratch)

	tasks, total, err := c.buildTasks(scratch, pairSets)
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, nil
	}
	if total < parallelThreshold {
		return computeDeltasSerial(tasks), nil
	}
	return c.computeDeltasParallel(tasks), nil
}

// lthashTask is one unit of parallel work: a chunk of pairs that all belong to
// a single (db, module) bucket.
type lthashTask struct {
	key   ModuleKey
	pairs []KVPairWithLastValue
}

// moduleBuckets is the scratch one ComputeModuleHashInfos call buckets its pairs into.
// Carrying the backing arrays from block to block is what keeps this off the allocator:
// a steady workload reaches a size that fits and stops allocating here entirely.
//
// The buckets hold the previous block's pairs until the next one overwrites them, so a
// pooled scratch pins one block's keys and values for as long as it sits idle.
type moduleBuckets struct {
	pairs map[ModuleKey][]KVPairWithLastValue
	tasks []lthashTask
}

var moduleBucketPool = sync.Pool{
	New: func() any {
		return &moduleBuckets{pairs: make(map[ModuleKey][]KVPairWithLastValue)}
	},
}

// reset prepares the scratch for another block. A bucket's length on entry is still the
// previous block's requirement for that module, and that is what sizes it: the buffer is
// kept when it already fits, and otherwise replaced with room for twice the requirement.
// Sizing from the requirement rather than from the last capacity is what lets a bucket
// shrink again after a large block instead of holding its peak forever.
func (b *moduleBuckets) reset() {
	for key, pairs := range b.pairs {
		want := 2 * len(pairs)
		if want == 0 {
			delete(b.pairs, key)
			continue
		}
		if cap(pairs) < want || cap(pairs) > 2*want {
			b.pairs[key] = make([]KVPairWithLastValue, 0, want)
			continue
		}
		b.pairs[key] = pairs[:0]
	}
	b.tasks = b.tasks[:0]
}

// buildTasks buckets every DB's pairs by module and splits each bucket into fixed-size
// tasks. It also returns the total pair count so callers can pick the serial vs parallel
// path. The returned tasks alias the scratch and stay valid until it is next reset.
func (c *HashCalculator) buildTasks(
	scratch *moduleBuckets,
	pairSets []DBPairs,
) (tasks []lthashTask, total int, err error) {
	scratch.reset()
	for _, ps := range pairSets {
		total += len(ps.Pairs)
		for _, pair := range ps.Pairs {
			module, err := c.moduleOf(pair.Key)
			if err != nil {
				return nil, 0, fmt.Errorf("failed to bucket %s pairs by module: %w", ps.Dir, err)
			}
			key := ModuleKey{Dir: ps.Dir, Module: module}
			scratch.pairs[key] = append(scratch.pairs[key], pair)
		}
	}
	for key, mpairs := range scratch.pairs {
		for start := 0; start < len(mpairs); start += computeChunkSize {
			end := start + computeChunkSize
			if end > len(mpairs) {
				end = len(mpairs)
			}
			scratch.tasks = append(scratch.tasks, lthashTask{key: key, pairs: mpairs[start:end]})
		}
	}
	return scratch.tasks, total, nil
}

// foldChunk computes the homomorphic hash delta and the net key-count / byte
// deltas for one chunk of pairs. Key presence is defined exactly as the hash
// defines it: a prior value exists iff LastValue is non-empty (an unmix), and a
// new value exists iff the entry is not a delete and Value is non-empty (a mix).
//   - add    (!old,  new): +1 key, + (len(key)+len(newVal)) bytes
//   - update ( old,  new):  0 keys, + (len(newVal)-len(oldVal)) bytes
//   - delete ( old, !new): -1 key, - (len(key)+len(oldVal)) bytes
//   - no-op  (!old, !new): unchanged (delete of an absent key)
func foldChunk(pairs []KVPairWithLastValue) *ModuleHashInfo {
	d := &ModuleHashInfo{Hash: New()}
	acc := active.newAccumulator()
	// One serialization buffer serves the whole chunk; it grows to the largest
	// pair in it and is reused for every hash after that.
	var scratch []byte
	for _, kv := range pairs {
		// A member exists iff serializeKV would produce a non-nil buffer, i.e.
		// key and value are both non-empty. Keeping these predicates identical
		// to the mix conditions guarantees the stats track exactly the set the
		// hash represents.
		hadOld := len(kv.Key) > 0 && len(kv.LastValue) > 0
		hasNew := len(kv.Key) > 0 && !kv.Delete && len(kv.Value) > 0
		if hadOld && hasNew && bytes.Equal(kv.LastValue, kv.Value) {
			// Rewriting a value with itself mixes out and back in the same
			// hash, and moves neither the key count nor the byte total.
			continue
		}
		if hadOld {
			scratch = serializeKVInto(scratch, kv.Key, kv.LastValue)
			acc.fold(scratch, true)
		}
		if hasNew {
			scratch = serializeKVInto(scratch, kv.Key, kv.Value)
			acc.fold(scratch, false)
		}
		switch {
		case !hadOld && hasNew:
			d.KeyCount++
			d.Bytes += int64(len(kv.Key)) + int64(len(kv.Value))
		case hadOld && hasNew:
			d.Bytes += int64(len(kv.Value)) - int64(len(kv.LastValue))
		case hadOld && !hasNew:
			d.KeyCount--
			d.Bytes -= int64(len(kv.Key)) + int64(len(kv.LastValue))
		}
	}
	acc.finish(d.Hash)
	return d
}

// mergeDelta folds src into dst (hash + counts). dst must be non-nil.
func mergeDelta(dst, src *ModuleHashInfo) {
	dst.Hash.MixIn(src.Hash)
	dst.KeyCount += src.KeyCount
	dst.Bytes += src.Bytes
}

// computeDeltasSerial folds all tasks into per-(db,module) deltas on the caller
// goroutine. Used for small blocks where pool overhead does not pay off.
func computeDeltasSerial(tasks []lthashTask) map[ModuleKey]*ModuleHashInfo {
	deltas := make(map[ModuleKey]*ModuleHashInfo)
	for _, task := range tasks {
		d := foldChunk(task.pairs)
		if acc := deltas[task.key]; acc != nil {
			mergeDelta(acc, d)
		} else {
			deltas[task.key] = d
		}
	}
	return deltas
}

// computeDeltasParallel distributes tasks across the fixed pool as independent,
// self-terminating units — one fold per chunk — then merges results as they
// arrive. A buffered result channel (capacity = task count) ensures workers
// never block on send, so a full pool queue only backpressures the submitter
// while already-running chunks drain. This is safe when several goroutines
// share one pool (the importer's per-DB workers all call through here).
// MixIn/addition are commutative, so merge order does not matter.
func (c *HashCalculator) computeDeltasParallel(tasks []lthashTask) map[ModuleKey]*ModuleHashInfo {
	type result struct {
		key  ModuleKey
		info *ModuleHashInfo
	}
	// Buffer must be large enough for every task: we submit all work before
	// draining results, and Submit can block when the pool queue is full. If a
	// finished worker then blocked on an unbuffered send here, nothing would
	// free a queue slot and we'd deadlock. MixIn/addition are commutative, so
	// merge order does not matter.
	results := make(chan result, len(tasks))
	for i := range tasks {
		task := tasks[i]
		c.pool.Submit(func() {
			results <- result{key: task.key, info: foldChunk(task.pairs)}
		})
	}

	merged := make(map[ModuleKey]*ModuleHashInfo)
	for range tasks {
		r := <-results
		if acc := merged[r.key]; acc != nil {
			mergeDelta(acc, r.info)
		} else {
			merged[r.key] = r.info
		}
	}
	return merged
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

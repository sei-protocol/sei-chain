package lthash

import (
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

// OldValueReader reads the previously-committed serialized value for a set of
// physical keys within a single logical DB (identified by dir). It is
// implemented by the caller (the FlatKV store) over its underlying DBs plus any
// pending-write overlay. Only keys that have a prior value appear in the result;
// a key mapped to a nil value means "resolved, but no bytes to unmix" (e.g. the
// key is pending a deletion earlier in the same block).
type OldValueReader interface {
	ReadOldValues(dir string, physKeys map[string]struct{}) (map[string][]byte, error)
}

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

// HashCalculator owns the CPU-bound lattice-hash worker pool and encapsulates
// the per-block lattice-hash pipeline:
//
//  1. ReadOldValues — grab the prior value for each changed key (in parallel).
//  2. Compute — hash individual keys and combine the per-worker results into
//     the final per-module hashes, then derive each per-DB root and the global
//     hash from those.
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

// NewHashCalculator creates a HashCalculator backed by a fixed pool of `workers`
// goroutines (clamped to >= 1). dbDirs is the canonical, ordered set of data DB
// directories; moduleOf extracts a physical key's owning module.
func NewHashCalculator(name string, workers int, dbDirs []string, moduleOf ModuleFunc) *HashCalculator {
	if workers < 1 {
		workers = 1
	}
	return &HashCalculator{
		pool:     threading.NewFixedPool(name, workers, workers),
		dbDirs:   append([]string(nil), dbDirs...),
		moduleOf: moduleOf,
	}
}

// Close shuts down the worker pool.
func (c *HashCalculator) Close() {
	if c.pool != nil {
		c.pool.Close()
	}
}

// ReadOldValues fetches prior serialized values for keysByDB (dir -> physical
// keyset), reading each DB in parallel over the pool. This is step (1) of the
// pipeline: "take the changed keys and grab the old value for each".
func (c *HashCalculator) ReadOldValues(
	reader OldValueReader,
	keysByDB map[string]map[string]struct{},
) (map[string]map[string][]byte, error) {
	type job struct {
		dir  string
		keys map[string]struct{}
	}
	var jobs []job
	for dir, keySet := range keysByDB {
		if len(keySet) == 0 {
			continue
		}
		jobs = append(jobs, job{dir: dir, keys: keySet})
	}

	out := make(map[string]map[string][]byte, len(jobs))
	if len(jobs) == 0 {
		return out, nil
	}

	results := make([]map[string][]byte, len(jobs))
	errs := make([]error, len(jobs))
	var wg sync.WaitGroup
	wg.Add(len(jobs))
	for i := range jobs {
		idx := i
		c.pool.Submit(func() {
			defer wg.Done()
			results[idx], errs[idx] = reader.ReadOldValues(jobs[idx].dir, jobs[idx].keys)
		})
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("read old values for %s: %w", jobs[i].dir, err)
		}
		out[jobs[i].dir] = results[i]
	}
	return out, nil
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
	tasks, total, err := c.buildTasks(pairSets)
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

// buildTasks buckets each DB's pairs by module and splits every bucket into
// fixed-size tasks. It also returns the total pair count so callers can pick the
// serial vs parallel path.
func (c *HashCalculator) buildTasks(pairSets []DBPairs) (tasks []lthashTask, total int, err error) {
	for _, ps := range pairSets {
		if len(ps.Pairs) == 0 {
			continue
		}
		total += len(ps.Pairs)
		byModule, err := BucketByModule(ps.Pairs, c.moduleOf)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to bucket %s pairs by module: %w", ps.Dir, err)
		}
		for module, mpairs := range byModule {
			for start := 0; start < len(mpairs); start += computeChunkSize {
				end := start + computeChunkSize
				if end > len(mpairs) {
					end = len(mpairs)
				}
				tasks = append(tasks, lthashTask{
					key:   ModuleKey{Dir: ps.Dir, Module: module},
					pairs: mpairs[start:end],
				})
			}
		}
	}
	return tasks, total, nil
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
	for _, kv := range pairs {
		// A member exists iff serializeKV would produce a non-nil buffer, i.e.
		// key and value are both non-empty. Keeping these predicates identical
		// to the mix conditions guarantees the stats track exactly the set the
		// hash represents.
		hadOld := len(kv.Key) > 0 && len(kv.LastValue) > 0
		hasNew := len(kv.Key) > 0 && !kv.Delete && len(kv.Value) > 0
		if hadOld {
			h := hash(serializeKV(kv.Key, kv.LastValue))
			d.Hash.MixOut(h)
			putLtHashToPool(h)
		}
		if hasNew {
			h := hash(serializeKV(kv.Key, kv.Value))
			d.Hash.MixIn(h)
			putLtHashToPool(h)
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
// self-terminating units — one fold per chunk — then merges the per-chunk
// results. Unlike a "long-lived worker loop reading a task channel" design, no
// submitted task ever blocks waiting for a later submit, so this is safe when
// several goroutines share one pool (the importer's per-DB workers all call
// through here): a full queue only backpressures the submitter while
// already-running chunks drain and free workers.
func (c *HashCalculator) computeDeltasParallel(tasks []lthashTask) map[ModuleKey]*ModuleHashInfo {
	results := make([]*ModuleHashInfo, len(tasks))
	var wg sync.WaitGroup
	wg.Add(len(tasks))
	for i := range tasks {
		idx := i
		c.pool.Submit(func() {
			defer wg.Done()
			results[idx] = foldChunk(tasks[idx].pairs)
		})
	}
	wg.Wait()

	// Merge per-chunk results. Multiple chunks of the same (db, module) bucket
	// sum into one delta; MixIn/addition are commutative so chunk order is
	// irrelevant.
	merged := make(map[ModuleKey]*ModuleHashInfo)
	for i, task := range tasks {
		if acc := merged[task.key]; acc != nil {
			mergeDelta(acc, results[i])
		} else {
			merged[task.key] = results[i]
		}
	}
	return merged
}

// BucketByModule groups LtHash pairs by their owning module, derived from each
// physical key via moduleOf. Used to decompose a per-DB root into additive
// per-module hashes without changing the root.
func BucketByModule(
	pairs []KVPairWithLastValue,
	moduleOf ModuleFunc,
) (map[string][]KVPairWithLastValue, error) {
	byModule := make(map[string][]KVPairWithLastValue)
	for _, pair := range pairs {
		module, err := moduleOf(pair.Key)
		if err != nil {
			return nil, err
		}
		byModule[module] = append(byModule[module], pair)
	}
	return byModule, nil
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

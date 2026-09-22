package lthash

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
)

// The hash phase: turning a block's changed key-value pairs into one homomorphic delta per (database,
// module). Nothing here depends on any other block, which is what lets several blocks be hashed at once.

// leafHasher turns one block's mutations into leaf hashes, fanned out across the pool.
type leafHasher struct {
	// Computes the leaf hashes. Owned by the caller, and must stay open at least as long as this.
	pool threading.Pool

	// Derives the module a raw key belongs to, which is how a block's mutations are bucketed.
	moduleParser ModuleParser

	// How many KV pairs each task carries.
	chunkSize uint32
}

// leafHashes is one block's leaf hashing in flight: exactly count results arrive on resultChan, in
// whatever order the workers finish.
//
// The channel is per-block, which is what keeps blocks from interleaving while several fold at once. It
// is buffered to count so a worker never blocks on send, which would hold a pool slot against the
// combiner.
type leafHashes struct {
	count      int
	resultChan chan *chunkResult
}

func newLeafHasher(pool threading.Pool, moduleParser ModuleParser, chunkSize uint32) *leafHasher {
	return &leafHasher{pool: pool, moduleParser: moduleParser, chunkSize: chunkSize}
}

// Submits one block's leaf hashing, returning the results still to arrive.
func (h *leafHasher) submit(mutations []DatabaseMutations) (leafHashes, error) {
	tasks, err := buildTasks(h.moduleParser, mutations, h.chunkSize)
	if err != nil {
		return leafHashes{}, err
	}

	pending := leafHashes{count: len(tasks), resultChan: make(chan *chunkResult, len(tasks))}
	for i := range tasks {
		task := tasks[i]
		h.pool.Submit(func() {
			pending.resultChan <- &chunkResult{key: task.key, info: hashChunk(task.mutations)}
		})
	}
	return pending, nil
}

// ComputeModuleHashInfos buckets each database's mutations by module, splits every bucket into fixed-size
// and distributes those chunks across pool to compute the per-(database, module) homomorphic hash delta and
// the accompanying key-count / byte deltas.
//
// Each chunk is an independent, self-terminating task, so this is safe to call concurrently from
// several goroutines sharing one pool — the state-sync importer runs a goroutine per DB. It never holds
// a worker while waiting on another task, so no oversubscription or deadlock can arise from the nesting.
func ComputeModuleHashInfos(
	pool threading.Pool,
	moduleOf ModuleParser,
	mutations []DatabaseMutations,
	// How many KV pairs each task carries.
	chunkSize uint32,
) (map[ModuleKey]*ModuleHashInfo, error) {
	tasks, err := buildTasks(moduleOf, mutations, chunkSize)
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, nil
	}
	return hashChunks(pool, tasks), nil
}

// lthashTask is one unit of parallel work: a chunk of pairs that all belong to
// a single (database, module) bucket.
type lthashTask struct {
	key       ModuleKey
	mutations []KeyMutation
}

// buildTasks buckets each database's mutations by module and splits every bucket into fixed-size
// tasks.
func buildTasks(moduleOf ModuleParser, mutations []DatabaseMutations, chunkSize uint32) ([]lthashTask, error) {
	size := int(chunkSize)
	var tasks []lthashTask
	for _, dbMutations := range mutations {
		if len(dbMutations.Mutations) == 0 {
			continue
		}
		byModule, err := BucketByModule(dbMutations.Mutations, moduleOf)
		if err != nil {
			return nil, fmt.Errorf("failed to bucket %s mutations by module: %w", dbMutations.DBName, err)
		}
		for module, moduleMutations := range byModule {
			for start := 0; start < len(moduleMutations); start += size {
				end := start + size
				if end > len(moduleMutations) {
					end = len(moduleMutations)
				}
				tasks = append(tasks, lthashTask{
					key:       ModuleKey{DBName: dbMutations.DBName, Module: module},
					mutations: moduleMutations[start:end],
				})
			}
		}
	}
	return tasks, nil
}

// ComputeLtHash applies mutations to prev and returns the result. A nil prev starts from zero.
func ComputeLtHash(prev *LtHash, mutations []KeyMutation) *LtHash {
	result := New()
	if prev != nil {
		result = prev.Clone()
	}
	result.MixIn(hashChunk(mutations).Hash)
	return result
}

// hashChunk computes the homomorphic hash delta and the net key-count / byte
// deltas for one chunk of pairs. Key presence is defined exactly as the hash
// defines it: a prior value exists iff LastValue is non-empty (an unmix), and a
// new value exists iff the entry is not a delete and Value is non-empty (a mix).
//   - add    (!old,  new): +1 key, + (len(key)+len(newVal)) bytes
//   - update ( old,  new):  0 keys, + (len(newVal)-len(oldVal)) bytes
//   - delete ( old, !new): -1 key, - (len(key)+len(oldVal)) bytes
//   - no-op  (!old, !new): unchanged (delete of an absent key)
func hashChunk(mutations []KeyMutation) *ModuleHashInfo {
	d := &ModuleHashInfo{Hash: New()}
	for _, mutation := range mutations {
		// A member exists iff serializeKV would produce a non-nil buffer, i.e.
		// key and value are both non-empty. Keeping these predicates identical
		// to the mix conditions guarantees the stats track exactly the set the
		// hash represents.
		hadOld := len(mutation.Key) > 0 && len(mutation.LastValue) > 0
		hasNew := len(mutation.Key) > 0 && !mutation.Delete && len(mutation.Value) > 0
		if hadOld {
			h := hash(serializeKV(mutation.Key, mutation.LastValue))
			d.Hash.MixOut(h)
			putLtHashToPool(h)
		}
		if hasNew {
			h := hash(serializeKV(mutation.Key, mutation.Value))
			d.Hash.MixIn(h)
			putLtHashToPool(h)
		}
		switch {
		case !hadOld && hasNew:
			d.KeyCount++
			d.Bytes += int64(len(mutation.Key)) + int64(len(mutation.Value))
		case hadOld && hasNew:
			d.Bytes += int64(len(mutation.Value)) - int64(len(mutation.LastValue))
		case hadOld && !hasNew:
			d.KeyCount--
			d.Bytes -= int64(len(mutation.Key)) + int64(len(mutation.LastValue))
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

// hashChunks distributes tasks across pool as independent, self-terminating
// units — one fold per chunk — then merges results as they arrive. A buffered
// result channel (capacity = task count) ensures workers never block on send, so
// a full pool queue only backpressures the submitter while already-running chunks
// drain. This is safe when several goroutines share one pool (the importer's
// per-DB workers all call through here). MixIn/addition are commutative, so merge
// order does not matter.
func hashChunks(pool threading.Pool, tasks []lthashTask) map[ModuleKey]*ModuleHashInfo {
	type result struct {
		key  ModuleKey
		info *ModuleHashInfo
	}
	// Buffer must be large enough for every task: we submit all work before
	// draining results, and Submit can block when the pool queue is full. If a
	// finished worker then blocked on an unbuffered send here, nothing would
	// free a queue slot and we'd deadlock.
	resultChan := make(chan result, len(tasks))
	for i := range tasks {
		task := tasks[i]
		pool.Submit(func() {
			resultChan <- result{key: task.key, info: hashChunk(task.mutations)}
		})
	}

	merged := make(map[ModuleKey]*ModuleHashInfo)
	for range tasks {
		r := <-resultChan
		if acc := merged[r.key]; acc != nil {
			mergeDelta(acc, r.info)
		} else {
			merged[r.key] = r.info
		}
	}
	return merged
}

// BucketByModule groups mutations by their owning module, derived from each
// physical key via moduleOf. Used to decompose a per-DB root into additive
// per-module hashes without changing the root.
func BucketByModule(
	mutations []KeyMutation,
	moduleOf ModuleParser,
) (map[string][]KeyMutation, error) {
	byModule := make(map[string][]KeyMutation)
	for _, mutation := range mutations {
		module, err := moduleOf(mutation.Key)
		if err != nil {
			return nil, err
		}
		byModule[module] = append(byModule[module], mutation)
	}
	return byModule, nil
}

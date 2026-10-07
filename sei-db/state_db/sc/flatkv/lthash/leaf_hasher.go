package lthash

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
)

// The hash phase: turning a block's changed key-value pairs into one homomorphic delta per (database,
// module). Nothing here depends on any other block, which is what lets several blocks be hashed at once.

// leafHasher turns one block's mutations into leaf hashes, fanned out across the pool.
type leafHasher struct {
	// Computes the leaf hashes. Owned by the caller, and must stay open at least as long as this.
	pool threading.Pool

	// Derives the module a raw key belongs to, which is how a block's deltas are split by module.
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
func (h *leafHasher) submit(mutations []DatabaseMutations) leafHashes {
	tasks := buildTasks(mutations, h.chunkSize)

	pending := leafHashes{count: len(tasks), resultChan: make(chan *chunkResult, len(tasks))}
	for i := range tasks {
		task := tasks[i]
		h.pool.Submit(func() {
			pending.resultChan <- hashChunk(h.moduleParser, task)
		})
	}
	return pending
}

// ComputeModuleHashInfos splits each database's mutations into fixed-size chunks and distributes those
// chunks across pool to compute the per-(database, module) homomorphic hash delta and the accompanying
// key-count / byte deltas.
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
	tasks := buildTasks(mutations, chunkSize)
	if len(tasks) == 0 {
		return nil, nil
	}
	return hashChunks(pool, moduleOf, tasks)
}

// lthashTask is one unit of parallel work: a chunk of one database's mutations, which may span several
// modules.
type lthashTask struct {
	dbName    string
	mutations []view.Mutation
}

// buildTasks splits each database's mutations into tasks of at most chunkSize, as sub-slices.
func buildTasks(mutations []DatabaseMutations, chunkSize uint32) []lthashTask {
	size := int(chunkSize)
	var tasks []lthashTask
	for _, dbMutations := range mutations {
		for start := 0; start < len(dbMutations.Mutations); start += size {
			end := min(start+size, len(dbMutations.Mutations))
			tasks = append(tasks, lthashTask{
				dbName:    dbMutations.DBName,
				mutations: dbMutations.Mutations[start:end],
			})
		}
	}
	return tasks
}

// ComputeLtHash applies mutations to prev and returns the result. A nil prev starts from zero.
func ComputeLtHash(prev *LtHash, mutations []view.Mutation) *LtHash {
	result := New()
	if prev != nil {
		result = prev.Clone()
	}
	result.MixIn(hashMutations(mutations).Hash)
	return result
}

// hashChunk folds one task into a delta per module. Keys of one module are contiguous in a database's
// sorted mutations, so each run of one module is hashed as a unit; a module that reappears later in the
// chunk is merged into its earlier delta.
func hashChunk(moduleOf ModuleParser, task lthashTask) *chunkResult {
	result := &chunkResult{dbName: task.dbName}
	runStart := 0
	runModule := ""
	for i := range task.mutations {
		module, err := moduleOf(task.mutations[i].Key())
		if err != nil {
			return &chunkResult{
				dbName: task.dbName,
				err:    fmt.Errorf("find the module of a %s key: %w", task.dbName, err),
			}
		}
		if i > 0 && module != runModule {
			result.add(runModule, hashMutations(task.mutations[runStart:i]))
			runStart = i
		}
		runModule = module
	}
	if len(task.mutations) > 0 {
		result.add(runModule, hashMutations(task.mutations[runStart:]))
	}
	return result
}

// add merges info into the chunk's delta for module.
func (r *chunkResult) add(module string, info *ModuleHashInfo) {
	for i := range r.modules {
		if r.modules[i].module == module {
			mergeDelta(r.modules[i].info, info)
			return
		}
	}
	r.modules = append(r.modules, moduleDelta{module: module, info: info})
}

// hashMutations computes the homomorphic hash delta and the net key-count / byte
// deltas for a run of pairs. Key presence is defined exactly as the hash
// defines it: a prior value exists iff Previous is non-empty (an unmix), and a
// new value exists iff Value is non-empty (a mix). A deletion has a nil Value.
//   - add    (!old,  new): +1 key, + (len(key)+len(newVal)) bytes
//   - update ( old,  new):  0 keys, + (len(newVal)-len(oldVal)) bytes
//   - delete ( old, !new): -1 key, - (len(key)+len(oldVal)) bytes
//   - no-op  (!old, !new): unchanged (delete of an absent key)
func hashMutations(mutations []view.Mutation) *ModuleHashInfo {
	d := &ModuleHashInfo{Hash: New()}
	for i := range mutations {
		key := mutations[i].Key()
		value := mutations[i].Value()
		previous := mutations[i].Previous()

		// A member exists iff serializeKV would produce a non-nil buffer, i.e.
		// key and value are both non-empty. Keeping these predicates identical
		// to the mix conditions guarantees the stats track exactly the set the
		// hash represents.
		hadOld := len(key) > 0 && len(previous) > 0
		hasNew := len(key) > 0 && len(value) > 0
		if hadOld {
			h := hash(serializeKV(key, previous))
			d.Hash.MixOut(h)
			putLtHashToPool(h)
		}
		if hasNew {
			h := hash(serializeKV(key, value))
			d.Hash.MixIn(h)
			putLtHashToPool(h)
		}
		switch {
		case !hadOld && hasNew:
			d.KeyCount++
			d.Bytes += int64(len(key)) + int64(len(value))
		case hadOld && hasNew:
			d.Bytes += int64(len(value)) - int64(len(previous))
		case hadOld && !hasNew:
			d.KeyCount--
			d.Bytes -= int64(len(key)) + int64(len(previous))
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

// mergeChunkResult folds one chunk's per-module deltas into merged.
func mergeChunkResult(merged map[ModuleKey]*ModuleHashInfo, result *chunkResult) {
	for _, delta := range result.modules {
		key := ModuleKey{DBName: result.dbName, Module: delta.module}
		if acc := merged[key]; acc != nil {
			mergeDelta(acc, delta.info)
		} else {
			merged[key] = delta.info
		}
	}
}

// hashChunks distributes tasks across pool as independent, self-terminating
// units — one fold per chunk — then merges results as they arrive. A buffered
// result channel (capacity = task count) ensures workers never block on send, so
// a full pool queue only backpressures the submitter while already-running chunks
// drain. This is safe when several goroutines share one pool (the importer's
// per-DB workers all call through here). MixIn/addition are commutative, so merge
// order does not matter.
func hashChunks(
	pool threading.Pool,
	moduleOf ModuleParser,
	tasks []lthashTask,
) (map[ModuleKey]*ModuleHashInfo, error) {
	// Buffer must be large enough for every task: we submit all work before
	// draining results, and Submit can block when the pool queue is full. If a
	// finished worker then blocked on an unbuffered send here, nothing would
	// free a queue slot and we'd deadlock.
	resultChan := make(chan *chunkResult, len(tasks))
	for i := range tasks {
		task := tasks[i]
		pool.Submit(func() {
			resultChan <- hashChunk(moduleOf, task)
		})
	}

	merged := make(map[ModuleKey]*ModuleHashInfo)
	var firstErr error
	// Every result is drained even after a failure, so that no worker outlives this call.
	for range tasks {
		result := <-resultChan
		if result.err != nil {
			if firstErr == nil {
				firstErr = result.err
			}
			continue
		}
		mergeChunkResult(merged, result)
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return merged, nil
}

package lthash

import (
	"bytes"
	"runtime"
	"sync"
	"time"
)

var ltHashMetaPrefix = []byte("s/_lthash:")

// LtHashTimings is wall-clock timing breakdown for LtHash computation.
type LtHashTimings struct {
	TotalNs     int64 // Total wall-clock time for the entire computation
	Blake3Ns    int64 // Wall-clock time for Blake3 XOF phase
	SerializeNs int64 // Wall-clock time for serialization phase
	MixInOutNs  int64 // Wall-clock time for MixIn/MixOut phase
	MergeNs     int64 // Wall-clock time for merging worker results
}

// DefaultLtHashWorkers defaults to NumCPU.
var DefaultLtHashWorkers = runtime.NumCPU()

func getLtHashFromPool() *LtHash {
	lth := ltHashPool.Get().(*LtHash)
	lth.Identity()
	return lth
}

func putLtHashToPool(lth *LtHash) {
	if lth != nil {
		ltHashPool.Put(lth)
	}
}

// KVPairWithOldValue is a helper struct for parallel computation that includes the old value
type KVPairWithOldValue struct {
	Key            []byte
	Value          []byte
	LastFlushValue []byte
	Deleted        bool
}

// ComputeLtHashDeltaParallel computes LtHash delta for a changeset.
func ComputeLtHashDeltaParallel(dbName string, kvPairs []KVPairWithOldValue, numWorkers int) (*LtHash, *LtHashTimings) {
	totalStart := time.Now()

	if numWorkers <= 0 {
		numWorkers = DefaultLtHashWorkers
	}

	if len(kvPairs) == 0 {
		return NewEmptyLtHash(), &LtHashTimings{TotalNs: time.Since(totalStart).Nanoseconds()}
	}

	// For small changesets, use serial processing (overhead of goroutines not worth it)
	if len(kvPairs) < 100 {
		return computeLtHashDeltaSerial(dbName, kvPairs)
	}

	// Phase 1: Serialize all KV pairs
	serializeStart := time.Now()
	type serializedKV struct {
		oldSerialized []byte
		newSerialized []byte
	}
	serializedPairs := make([]serializedKV, len(kvPairs))
	for i, kv := range kvPairs {
		if isMetaKey(kv.Key) {
			continue
		}
		if len(kv.LastFlushValue) > 0 {
			serializedPairs[i].oldSerialized = SerializeForLtHash(dbName, kv.Key, kv.LastFlushValue)
		}
		if !kv.Deleted && len(kv.Value) > 0 {
			serializedPairs[i].newSerialized = SerializeForLtHash(dbName, kv.Key, kv.Value)
		}
	}
	serializeNs := time.Since(serializeStart).Nanoseconds()

	// Phase 2: Blake3 XOF (parallel) - convert serialized bytes to LtHash vectors
	blake3Start := time.Now()
	type lthashPair struct {
		oldLth *LtHash
		newLth *LtHash
	}
	lthashPairs := make([]lthashPair, len(kvPairs))

	chunkSize := (len(kvPairs) + numWorkers - 1) / numWorkers
	var wg sync.WaitGroup

	for w := 0; w < numWorkers; w++ {
		start := w * chunkSize
		if start >= len(kvPairs) {
			continue
		}
		end := start + chunkSize
		if end > len(kvPairs) {
			end = len(kvPairs)
		}

		wg.Add(1)
		go func(startIdx, endIdx int) {
			defer wg.Done()
			for i := startIdx; i < endIdx; i++ {
				skv := serializedPairs[i]
				if skv.oldSerialized != nil {
					lthashPairs[i].oldLth = FromBytes(skv.oldSerialized)
				}
				if skv.newSerialized != nil {
					lthashPairs[i].newLth = FromBytes(skv.newSerialized)
				}
			}
		}(start, end)
	}
	wg.Wait()
	blake3Ns := time.Since(blake3Start).Nanoseconds()

	// Phase 3: MixIn/MixOut (parallel) - combine vectors
	mixStart := time.Now()
	results := make([]*LtHash, numWorkers)

	for w := 0; w < numWorkers; w++ {
		start := w * chunkSize
		if start >= len(kvPairs) {
			continue
		}
		end := start + chunkSize
		if end > len(kvPairs) {
			end = len(kvPairs)
		}

		wg.Add(1)
		go func(workerID int, startIdx, endIdx int) {
			defer wg.Done()
			workerLth := getLtHashFromPool()
			for i := startIdx; i < endIdx; i++ {
				lp := lthashPairs[i]
				if lp.oldLth != nil {
					workerLth.MixOut(lp.oldLth)
					putLtHashToPool(lp.oldLth)
				}
				if lp.newLth != nil {
					workerLth.MixIn(lp.newLth)
					putLtHashToPool(lp.newLth)
				}
			}
			results[workerID] = workerLth
		}(w, start, end)
	}
	wg.Wait()
	mixNs := time.Since(mixStart).Nanoseconds()

	// Phase 4: Merge worker results
	mergeStart := time.Now()
	finalLth := NewEmptyLtHash()
	for _, r := range results {
		if r != nil {
			finalLth.MixIn(r)
			putLtHashToPool(r)
		}
	}
	mergeNs := time.Since(mergeStart).Nanoseconds()

	timings := &LtHashTimings{
		TotalNs:     time.Since(totalStart).Nanoseconds(),
		SerializeNs: serializeNs,
		Blake3Ns:    blake3Ns,
		MixInOutNs:  mixNs,
		MergeNs:     mergeNs,
	}

	return finalLth, timings
}

// computeLtHashDeltaSerial computes the LtHash delta serially (for small changesets).
func computeLtHashDeltaSerial(dbName string, kvPairs []KVPairWithOldValue) (*LtHash, *LtHashTimings) {
	totalStart := time.Now()
	result := NewEmptyLtHash()

	// Phase 1: Serialize
	serializeStart := time.Now()
	type serializedKV struct {
		oldSerialized []byte
		newSerialized []byte
	}
	serializedPairs := make([]serializedKV, 0, len(kvPairs))
	for _, kv := range kvPairs {
		if isMetaKey(kv.Key) {
			continue
		}
		skv := serializedKV{}
		if len(kv.LastFlushValue) > 0 {
			skv.oldSerialized = SerializeForLtHash(dbName, kv.Key, kv.LastFlushValue)
		}
		if !kv.Deleted && len(kv.Value) > 0 {
			skv.newSerialized = SerializeForLtHash(dbName, kv.Key, kv.Value)
		}
		serializedPairs = append(serializedPairs, skv)
	}
	serializeNs := time.Since(serializeStart).Nanoseconds()

	// Phase 2: Blake3 XOF
	blake3Start := time.Now()
	type lthashPair struct {
		oldLth *LtHash
		newLth *LtHash
	}
	lthashPairs := make([]lthashPair, len(serializedPairs))
	for i, skv := range serializedPairs {
		if skv.oldSerialized != nil {
			lthashPairs[i].oldLth = FromBytes(skv.oldSerialized)
		}
		if skv.newSerialized != nil {
			lthashPairs[i].newLth = FromBytes(skv.newSerialized)
		}
	}
	blake3Ns := time.Since(blake3Start).Nanoseconds()

	// Phase 3: MixIn/MixOut
	mixStart := time.Now()
	for _, lp := range lthashPairs {
		if lp.oldLth != nil {
			result.MixOut(lp.oldLth)
			putLtHashToPool(lp.oldLth)
		}
		if lp.newLth != nil {
			result.MixIn(lp.newLth)
			putLtHashToPool(lp.newLth)
		}
	}
	mixNs := time.Since(mixStart).Nanoseconds()

	timings := &LtHashTimings{
		TotalNs:     time.Since(totalStart).Nanoseconds(),
		SerializeNs: serializeNs,
		Blake3Ns:    blake3Ns,
		MixInOutNs:  mixNs,
		MergeNs:     0, // No merge for serial
	}

	return result, timings
}

// isMetaKey checks for the "s/_lthash:" prefix (LtHash metadata keys).
func isMetaKey(key []byte) bool {
	return bytes.HasPrefix(key, ltHashMetaPrefix)
}

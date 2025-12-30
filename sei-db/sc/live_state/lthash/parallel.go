package lthash

import (
	"runtime"
	"sync"
	"time"
)

// LtHashTimings holds wall-clock timing breakdown for LtHash computation.
type LtHashTimings struct {
	TotalNs     int64
	Blake3Ns    int64
	SerializeNs int64
	MixInOutNs  int64
	MergeNs     int64
}

// DefaultLtHashWorkers defaults to NumCPU.
var DefaultLtHashWorkers = runtime.NumCPU()

func getLtHashFromPool() *LtHash {
	lth := ltHashPool.Get().(*LtHash)
	lth.Reset()
	return lth
}

func putLtHashToPool(lth *LtHash) {
	if lth != nil {
		ltHashPool.Put(lth)
	}
}

// KVPairWithOldValue holds a KV change for incremental LtHash delta computation.
type KVPairWithOldValue struct {
	Key            []byte
	Value          []byte
	LastFlushValue []byte // Previous value (for MixOut)
	Deleted        bool   // If true, only MixOut
}

// ComputeLtHashDeltaParallel computes the LtHash delta for a changeset.
// For each KV: MixOut(old) if LastFlushValue set, MixIn(new) if not Deleted.
func ComputeLtHashDeltaParallel(dbName string, kvPairs []KVPairWithOldValue, numWorkers int) (*LtHash, *LtHashTimings) {
	totalStart := time.Now()

	if numWorkers <= 0 {
		numWorkers = DefaultLtHashWorkers
	}

	if len(kvPairs) == 0 {
		return New(), &LtHashTimings{TotalNs: time.Since(totalStart).Nanoseconds()}
	}

	// Small changesets: serial is faster
	if len(kvPairs) < 100 {
		return computeLtHashDeltaSerial(dbName, kvPairs)
	}

	// Phase 1: Serialize
	serializeStart := time.Now()
	type serializedKV struct {
		oldSerialized []byte
		newSerialized []byte
	}
	serializedPairs := make([]serializedKV, len(kvPairs))
	for i, kv := range kvPairs {
		if len(kv.LastFlushValue) > 0 {
			serializedPairs[i].oldSerialized = serializeKV(dbName, kv.Key, kv.LastFlushValue)
		}
		if !kv.Deleted && len(kv.Value) > 0 {
			serializedPairs[i].newSerialized = serializeKV(dbName, kv.Key, kv.Value)
		}
	}
	serializeNs := time.Since(serializeStart).Nanoseconds()

	// Phase 2: Hash (parallel)
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
					lthashPairs[i].oldLth = Hash(skv.oldSerialized)
				}
				if skv.newSerialized != nil {
					lthashPairs[i].newLth = Hash(skv.newSerialized)
				}
			}
		}(start, end)
	}
	wg.Wait()
	blake3Ns := time.Since(blake3Start).Nanoseconds()

	// Phase 3: MixIn/MixOut (parallel)
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

	// Phase 4: Merge
	mergeStart := time.Now()
	finalLth := New()
	for _, r := range results {
		if r != nil {
			finalLth.MixIn(r)
			putLtHashToPool(r)
		}
	}
	mergeNs := time.Since(mergeStart).Nanoseconds()

	return finalLth, &LtHashTimings{
		TotalNs:     time.Since(totalStart).Nanoseconds(),
		SerializeNs: serializeNs,
		Blake3Ns:    blake3Ns,
		MixInOutNs:  mixNs,
		MergeNs:     mergeNs,
	}
}

// computeLtHashDeltaSerial is the serial version for small changesets.
func computeLtHashDeltaSerial(dbName string, kvPairs []KVPairWithOldValue) (*LtHash, *LtHashTimings) {
	totalStart := time.Now()
	result := New()

	// Phase 1: Serialize
	serializeStart := time.Now()
	type serializedKV struct {
		oldSerialized []byte
		newSerialized []byte
	}
	serializedPairs := make([]serializedKV, 0, len(kvPairs))
	for _, kv := range kvPairs {
		skv := serializedKV{}
		if len(kv.LastFlushValue) > 0 {
			skv.oldSerialized = serializeKV(dbName, kv.Key, kv.LastFlushValue)
		}
		if !kv.Deleted && len(kv.Value) > 0 {
			skv.newSerialized = serializeKV(dbName, kv.Key, kv.Value)
		}
		serializedPairs = append(serializedPairs, skv)
	}
	serializeNs := time.Since(serializeStart).Nanoseconds()

	// Phase 2: Hash
	blake3Start := time.Now()
	type lthashPair struct {
		oldLth *LtHash
		newLth *LtHash
	}
	lthashPairs := make([]lthashPair, len(serializedPairs))
	for i, skv := range serializedPairs {
		if skv.oldSerialized != nil {
			lthashPairs[i].oldLth = Hash(skv.oldSerialized)
		}
		if skv.newSerialized != nil {
			lthashPairs[i].newLth = Hash(skv.newSerialized)
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

	return result, &LtHashTimings{
		TotalNs:     time.Since(totalStart).Nanoseconds(),
		SerializeNs: serializeNs,
		Blake3Ns:    blake3Ns,
		MixInOutNs:  mixNs,
		MergeNs:     0,
	}
}

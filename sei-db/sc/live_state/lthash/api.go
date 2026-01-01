package lthash

import (
	"runtime"
	"sync"
	"time"
)

// --- Public Types ---

// KVPairWithLastValue holds a KV change for LtHash computation.
type KVPairWithLastValue struct {
	Key       []byte
	Value     []byte
	LastValue []byte // Previous value (nil for new keys)
	Delete    bool   // If true, only remove last value
}

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

// --- Public API ---

// ComputeLtHash applies changes to prev LtHash and returns the result.
// For each KV: MixOut(LastValue) if set, MixIn(Value) if not Delete.
// If prev is nil, starts from zero.
func ComputeLtHash(prev *LtHash, kvPairs []KVPairWithLastValue) (*LtHash, *LtHashTimings) {
	delta, timings := computeDelta(kvPairs, DefaultLtHashWorkers)

	result := New()
	if prev != nil {
		result = prev.Clone()
	}
	result.MixIn(delta)
	putLtHashToPool(delta)

	return result, timings
}

// --- Internal computation ---

// serializedKV holds serialized key-value data for hashing.
type serializedKV struct {
	lastSerialized []byte
	newSerialized  []byte
}

// lthashPair holds computed LtHash values for a single KV change.
type lthashPair struct {
	lastLth *LtHash
	newLth  *LtHash
}

// computeDelta computes the LtHash delta for a changeset.
func computeDelta(kvPairs []KVPairWithLastValue, numWorkers int) (*LtHash, *LtHashTimings) {
	totalStart := time.Now()

	if numWorkers <= 0 {
		numWorkers = DefaultLtHashWorkers
	}

	if len(kvPairs) == 0 {
		return New(), &LtHashTimings{TotalNs: time.Since(totalStart).Nanoseconds()}
	}

	// Small changesets: serial is faster
	if len(kvPairs) < 100 {
		return computeDeltaSerial(kvPairs)
	}

	// Phase 1: Serialize
	serializeStart := time.Now()
	serializedPairs := make([]serializedKV, len(kvPairs))
	for i, kv := range kvPairs {
		if len(kv.LastValue) > 0 {
			serializedPairs[i].lastSerialized = serializeKV(kv.Key, kv.LastValue)
		}
		if !kv.Delete && len(kv.Value) > 0 {
			serializedPairs[i].newSerialized = serializeKV(kv.Key, kv.Value)
		}
	}
	serializeNs := time.Since(serializeStart).Nanoseconds()

	// Phase 2: Hash (parallel)
	blake3Start := time.Now()
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
				if skv.lastSerialized != nil {
					lthashPairs[i].lastLth = hash(skv.lastSerialized)
				}
				if skv.newSerialized != nil {
					lthashPairs[i].newLth = hash(skv.newSerialized)
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
				if lp.lastLth != nil {
					workerLth.MixOut(lp.lastLth)
					putLtHashToPool(lp.lastLth)
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

// computeDeltaSerial is the serial version for small changesets.
func computeDeltaSerial(kvPairs []KVPairWithLastValue) (*LtHash, *LtHashTimings) {
	totalStart := time.Now()
	result := New()

	// Phase 1: Serialize
	serializeStart := time.Now()
	serializedPairs := make([]serializedKV, 0, len(kvPairs))
	for _, kv := range kvPairs {
		skv := serializedKV{}
		if len(kv.LastValue) > 0 {
			skv.lastSerialized = serializeKV(kv.Key, kv.LastValue)
		}
		if !kv.Delete && len(kv.Value) > 0 {
			skv.newSerialized = serializeKV(kv.Key, kv.Value)
		}
		serializedPairs = append(serializedPairs, skv)
	}
	serializeNs := time.Since(serializeStart).Nanoseconds()

	// Phase 2: Hash
	blake3Start := time.Now()
	lthashPairs := make([]lthashPair, len(serializedPairs))
	for i, skv := range serializedPairs {
		if skv.lastSerialized != nil {
			lthashPairs[i].lastLth = hash(skv.lastSerialized)
		}
		if skv.newSerialized != nil {
			lthashPairs[i].newLth = hash(skv.newSerialized)
		}
	}
	blake3Ns := time.Since(blake3Start).Nanoseconds()

	// Phase 3: MixIn/MixOut
	mixStart := time.Now()
	for _, lp := range lthashPairs {
		if lp.lastLth != nil {
			result.MixOut(lp.lastLth)
			putLtHashToPool(lp.lastLth)
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

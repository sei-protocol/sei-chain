package cryptosim

import (
	"hash/maphash"
	"iter"
	"sync"
)

// batchShardCount is the number of independently locked partitions a blockBatch spreads keys
// across. Well above the executor count so concurrent writers rarely land on the same shard, and a
// power of two so a key's shard is a mask of its hash rather than a division.
const batchShardCount = 256

// blockBatch accumulates one block's writes, keyed by the raw key bytes.
//
// Put and Get are safe to call concurrently with each other. Iterator and Clear are not safe
// against either, which matches how the benchmark drives it: executors write while the main thread
// feeds them, and the main thread iterates and clears only after flushing the executors.
type blockBatch struct {
	// The partitions keys are spread across, indexed by the masked hash of the key.
	shards []batchShard

	// The seed for key hashing, randomized per batch so no fixed key set can skew the distribution.
	seed maphash.Seed

	// Masks a key's hash down to a shard index. Valid because batchShardCount is a power of two.
	mask uint64
}

// batchShard is one independently locked partition of a blockBatch.
type batchShard struct {
	// Guards data.
	lock sync.RWMutex

	// The keys assigned to this shard, each holding the most recent value written for it.
	data map[string][]byte
}

// newBlockBatch returns an empty blockBatch.
func newBlockBatch() *blockBatch {
	shards := make([]batchShard, batchShardCount)
	for i := range shards {
		shards[i].data = make(map[string][]byte)
	}
	return &blockBatch{
		shards: shards,
		seed:   maphash.MakeSeed(),
		mask:   batchShardCount - 1,
	}
}

// Put stores value under key, replacing any value already held for it.
func (b *blockBatch) Put(key []byte, value []byte) {
	shard := &b.shards[maphash.Bytes(b.seed, key)&b.mask]
	shard.lock.Lock()
	// This conversion allocates, and has to: the map keeps the key, so it cannot alias a caller's
	// buffer. Get avoids the copy because a conversion written inline in an index expression is
	// elided by the compiler, which is not available here.
	shard.data[string(key)] = value
	shard.lock.Unlock()
}

// Get returns the value held for key, and whether there was one.
func (b *blockBatch) Get(key []byte) ([]byte, bool) {
	shard := &b.shards[maphash.Bytes(b.seed, key)&b.mask]
	shard.lock.RLock()
	// Written as an index expression so the compiler elides the conversion rather than allocating a
	// string per lookup. Passing string(key) to a helper would allocate on every read.
	value, ok := shard.data[string(key)]
	shard.lock.RUnlock()
	return value, ok
}

// Len returns the number of keys held.
func (b *blockBatch) Len() int {
	total := 0
	for i := range b.shards {
		b.shards[i].lock.RLock()
		total += len(b.shards[i].data)
		b.shards[i].lock.RUnlock()
	}
	return total
}

// Iterator returns an iterator over every key-value pair held, for use with range.
func (b *blockBatch) Iterator() iter.Seq2[string, []byte] {
	return func(yield func(string, []byte) bool) {
		for i := range b.shards {
			for key, value := range b.shards[i].data {
				if !yield(key, value) {
					return
				}
			}
		}
	}
}

// Clear removes every key-value pair, keeping each shard's map capacity so the next block builds
// its contents without reallocating buckets.
func (b *blockBatch) Clear() {
	for i := range b.shards {
		clear(b.shards[i].data)
	}
}

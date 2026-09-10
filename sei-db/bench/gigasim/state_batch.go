package gigasim

import (
	"fmt"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// maxStagedKeyLen is the longest key the benchmark stages: a storage key, which carries an address and
// a slot after the one-byte EVM prefix.
const maxStagedKeyLen = 1 + storageKeyLen

// batchShards is how many independently locked maps the staged writes are spread across, which keeps the
// executor pool from serialising on a single lock. It must not exceed 256, the range of the key byte the
// shard is chosen by.
const batchShards = 256

// stagedKey is an EVM key held by value, so that using it as a map key costs no allocation.
type stagedKey struct {
	length uint8
	data   [maxStagedKeyLen]byte
}

// newStagedKey copies a key into a form that can be hashed and compared without allocating.
func newStagedKey(key []byte) stagedKey {
	if len(key) > maxStagedKeyLen {
		panic(fmt.Sprintf("key of %d bytes exceeds the %d bytes the benchmark stages",
			len(key), maxStagedKeyLen))
	}
	staged := stagedKey{length: uint8(len(key))} //nolint:gosec // guarded against overflow above
	copy(staged.data[:], key)
	return staged
}

// stagedWrite is one pending write. It retains the caller's key rather than copying it, which is safe
// because every key staged is a freshly built EVM key that nothing mutates.
type stagedWrite struct {
	key   []byte
	value []byte
}

// batchShard is one independently locked slice of the staged writes. It is padded out to a cache line
// so that an executor taking one shard's lock does not invalidate a neighbouring shard for another.
type batchShard struct {
	mu      sync.Mutex
	entries map[stagedKey]stagedWrite
	_       [40]byte
}

// stateBatch collects the writes of the block being executed, keyed so that a key written twice in one
// block commits once. The executors fill it concurrently and the main thread drains it at commit.
type stateBatch struct {
	shards [batchShards]batchShard
}

// newStateBatch returns an empty batch with every shard ready to accept writes.
func newStateBatch() *stateBatch {
	batch := &stateBatch{}
	for i := range batch.shards {
		batch.shards[i].entries = make(map[stagedKey]stagedWrite)
	}
	return batch
}

// shardFor picks a key's shard from its last byte, which holds random data for every key the benchmark
// stages and so spreads keys evenly.
func (b *stateBatch) shardFor(key []byte) *batchShard {
	return &b.shards[uint(key[len(key)-1])%batchShards]
}

// Put stages a write, replacing any earlier write to the same key.
//
// Safe to call concurrently with Put and Get, but not with drainToChangeSet.
func (b *stateBatch) Put(key []byte, value []byte) {
	shard := b.shardFor(key)
	shard.mu.Lock()
	shard.entries[newStagedKey(key)] = stagedWrite{key: key, value: value}
	shard.mu.Unlock()
}

// Get returns a staged write, reporting false when the block being executed has not written the key.
//
// Safe to call concurrently with Put and Get, but not with drainToChangeSet.
func (b *stateBatch) Get(key []byte) ([]byte, bool) {
	shard := b.shardFor(key)
	shard.mu.Lock()
	write, found := shard.entries[newStagedKey(key)]
	shard.mu.Unlock()
	return write.value, found
}

// drainToChangeSet empties the batch into a single changeset over the EVM store, appending the
// identifier counters that ride along with every block.
//
// Must not run concurrently with Put or Get.
func (b *stateBatch) drainToChangeSet(counters identifierCounters) []*proto.NamedChangeSet {
	total := b.count() + len(counterKeys)
	pairs := make([]proto.KVPair, total)
	pointers := make([]*proto.KVPair, total)

	next := 0
	stage := func(key []byte, value []byte) {
		pairs[next] = proto.KVPair{Key: key, Value: value}
		pointers[next] = &pairs[next]
		next++
	}

	for i := range b.shards {
		entries := b.shards[i].entries
		for _, write := range entries {
			stage(write.key, write.value)
		}
		clear(entries)
	}
	stage(counterKeys[0], encodeCounter(counters.nextAccountID))
	stage(counterKeys[1], encodeCounter(counters.nextErc20ContractID))

	return []*proto.NamedChangeSet{{
		Name:      keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: pointers},
	}}
}

// count returns how many distinct keys the block being executed has written.
func (b *stateBatch) count() int {
	total := 0
	for i := range b.shards {
		total += len(b.shards[i].entries)
	}
	return total
}

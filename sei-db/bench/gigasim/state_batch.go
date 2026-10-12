package gigasim

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// maxStagedKeyLen is the longest key the benchmark stages: a storage key, which carries an address and
// a slot after the one-byte EVM prefix.
const maxStagedKeyLen = 1 + storageKeyLen

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

// blockWrites is one block's state changes in the form the state DB takes, along with the volume of
// key and value bytes they carry.
type blockWrites struct {
	changeSets []*proto.NamedChangeSet

	// The bytes the state WAL, SC and SS each take in, counted while the changeset was assembled so
	// that the commit thread does not walk the pairs again to find out.
	bytes int64
}

// stateBatch collects the writes of one block, keyed so that a key written twice in one block commits
// once.
//
// It carries no synchronization, because it never has more than one writer: the generator stages the
// block it is building, and setup stages on the main thread. Nothing writes here during execution — a
// block's writes are known when it is generated, so they are staged then.
type stateBatch struct {
	entries map[stagedKey]stagedWrite
}

// newStateBatch returns an empty batch sized for the number of distinct keys one block writes.
func newStateBatch(expectedWrites int) *stateBatch {
	return &stateBatch{entries: make(map[stagedKey]stagedWrite, expectedWrites)}
}

// Put stages a write, replacing any earlier write to the same key.
func (b *stateBatch) Put(key []byte, value []byte) {
	b.entries[newStagedKey(key)] = stagedWrite{key: key, value: value}
}

// drainToChangeSet empties the batch into a single changeset over the EVM store, appending the
// identifier counters that ride along with every block.
func (b *stateBatch) drainToChangeSet(counters identifierCounters) blockWrites {
	total := b.count() + len(counterKeys)
	pairs := make([]proto.KVPair, total)
	pointers := make([]*proto.KVPair, total)

	next := 0
	var staged int64
	stage := func(key []byte, value []byte) {
		pairs[next] = proto.KVPair{Key: key, Value: value}
		pointers[next] = &pairs[next]
		staged += int64(len(key) + len(value))
		next++
	}

	for _, write := range b.entries {
		stage(write.key, write.value)
	}
	clear(b.entries)
	stage(counterKeys[0], encodeCounter(counters.nextAccountID))
	stage(counterKeys[1], encodeCounter(counters.nextErc20ContractID))

	return blockWrites{
		changeSets: []*proto.NamedChangeSet{{
			Name:      keys.EVMStoreKey,
			Changeset: proto.ChangeSet{Pairs: pointers},
		}},
		bytes: staged,
	}
}

// count returns how many distinct keys the batch holds.
func (b *stateBatch) count() int {
	return len(b.entries)
}

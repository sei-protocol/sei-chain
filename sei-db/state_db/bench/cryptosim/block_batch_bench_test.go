package cryptosim

import (
	"encoding/binary"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// One default block: 5000 transactions writing 5 keys and reading 6 (see TransactionsPerBlock and
// Execute). Four of the five writes are unique per transaction and the fifth is the shared fee
// collection account, so a block leaves 4*5000+1 distinct keys behind.
const (
	benchTransactions = 5000
	benchWritesPerTxn = 5
	benchReadsPerTxn  = 6
	benchDistinctKeys = benchTransactions*(benchWritesPerTxn-1) + 1
)

// Key and value widths taken from the vtype serializations: a prefixed account or slot key, and a
// storage value of version + block height + 32-byte word.
const (
	benchKeyLen   = 32
	benchValueLen = 41
)

// benchKeys returns the distinct keys a block touches, plus a value buffer to store under them.
func benchKeys(count int) ([][]byte, []byte) {
	keys := make([][]byte, count)
	for i := range keys {
		key := make([]byte, benchKeyLen)
		binary.BigEndian.PutUint64(key, uint64(i))
		keys[i] = key
	}
	return keys, make([]byte, benchValueLen)
}

// writeSequence is the key index each of a block's writes targets: four unique keys per
// transaction, then the shared fee collection key.
func writeSequence() []int {
	seq := make([]int, 0, benchTransactions*benchWritesPerTxn)
	for txn := 0; txn < benchTransactions; txn++ {
		for w := 0; w < benchWritesPerTxn-1; w++ {
			seq = append(seq, txn*(benchWritesPerTxn-1)+w)
		}
		seq = append(seq, benchDistinctKeys-1)
	}
	return seq
}

// readSequence is the key index each of a block's reads targets, cycling over the keys written.
func readSequence() []int {
	seq := make([]int, 0, benchTransactions*benchReadsPerTxn)
	for i := 0; i < benchTransactions*benchReadsPerTxn; i++ {
		seq = append(seq, i%benchDistinctKeys)
	}
	return seq
}

// BenchmarkBatchPut covers a block's writes, which land in the update_balances phase.
func BenchmarkBatchPut(b *testing.B) {
	keys, value := benchKeys(benchDistinctKeys)
	seq := writeSequence()
	batch := newBlockBatch()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, k := range seq {
			batch.Put(keys[k], value)
		}
		batch.Clear()
	}
}

var benchFound bool

// BenchmarkBatchGet covers a block's reads, which land in the read_* phases.
func BenchmarkBatchGet(b *testing.B) {
	keys, value := benchKeys(benchDistinctKeys)
	batch := newBlockBatch()
	for _, key := range keys {
		batch.Put(key, value)
	}
	seq := readSequence()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, k := range seq {
			_, found := batch.Get(keys[k])
			benchFound = found
		}
	}
}

var benchPairs []*proto.KVPair

// BenchmarkBatchFinalize covers the changeset construction that makes up the finalizing phase:
// ranging the batch and carving a KVPair per entry out of one backing array.
func BenchmarkBatchFinalize(b *testing.B) {
	keys, value := benchKeys(benchDistinctKeys)
	batch := newBlockBatch()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		for _, key := range keys {
			batch.Put(key, value)
		}
		b.StartTimer()

		count := batch.Len()
		pairs := make([]*proto.KVPair, 0, count+3)
		backing := make([]proto.KVPair, count)
		next := 0
		for key, val := range batch.Iterator() {
			backing[next] = proto.KVPair{Key: []byte(key), Value: val}
			pairs = append(pairs, &backing[next])
			next++
		}
		batch.Clear()
		benchPairs = pairs
	}
}

// BenchmarkBatchPutParallel covers the writes as they actually arrive: from many executor
// goroutines at once. The shard count has to hold up under that for the single-threaded numbers to
// mean anything.
func BenchmarkBatchPutParallel(b *testing.B) {
	keys, value := benchKeys(benchDistinctKeys)
	batch := newBlockBatch()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			batch.Put(keys[i%benchDistinctKeys], value)
			i++
		}
	})
}

package gigasim

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
)

// testAccountKey builds the shorter of the two key lengths the batch stages. The identifier goes at
// the end so that distinct identifiers give distinct keys and spread across the shards, which are
// chosen by a key's last byte.
func testAccountKey(id int) []byte {
	address := make([]byte, keys.AddressLen)
	binary.BigEndian.PutUint64(address[keys.AddressLen-8:], uint64(id))
	return keys.BuildEVMKey(accountKeyPrefix, address)
}

// testSlotKey builds the longer of the two key lengths the batch stages.
func testSlotKey(id int) []byte {
	slot := make([]byte, storageKeyLen)
	binary.BigEndian.PutUint64(slot[storageKeyLen-8:], uint64(id))
	return keys.BuildEVMKey(keys.EVMKeyStorage, slot)
}

// stagedPairs indexes a drained changeset by key, which is the form the assertions are written in. It
// also asserts the changeset's shape: one entry, over the EVM store.
func stagedPairs(t *testing.T, batch *stateBatch, counters identifierCounters) map[string][]byte {
	t.Helper()
	changeSets := batch.drainToChangeSet(counters)
	require.Len(t, changeSets, 1)
	require.Equal(t, keys.EVMStoreKey, changeSets[0].Name)

	staged := map[string][]byte{}
	for _, pair := range changeSets[0].Changeset.Pairs {
		_, duplicate := staged[string(pair.Key)]
		require.False(t, duplicate, "key %x was committed twice in one block", pair.Key)
		staged[string(pair.Key)] = pair.Value
	}
	return staged
}

// A read of a key the block has written is served from the batch, and a read of one it has not is
// reported as missing so that the caller falls through to the committed view.
func TestBatchServesWhatTheBlockHasWritten(t *testing.T) {
	batch := newStateBatch()

	account, slot := testAccountKey(1), testSlotKey(1)
	batch.Put(account, []byte("account value"))
	batch.Put(slot, []byte("slot value"))

	value, found := batch.Get(account)
	require.True(t, found)
	require.Equal(t, []byte("account value"), value)

	value, found = batch.Get(slot)
	require.True(t, found)
	require.Equal(t, []byte("slot value"), value)

	_, found = batch.Get(testAccountKey(2))
	require.False(t, found)
}

// Keys are held by value in a fixed-width array, so two keys that share a prefix and differ only in
// length have to stay distinct rather than colliding on the padding.
func TestBatchKeepsKeysOfDifferentLengthsApart(t *testing.T) {
	batch := newStateBatch()

	short := keys.BuildEVMKey(accountKeyPrefix, make([]byte, keys.AddressLen))
	long := keys.BuildEVMKey(keys.EVMKeyStorage, make([]byte, storageKeyLen))

	batch.Put(short, []byte("short"))
	batch.Put(long, []byte("long"))

	value, found := batch.Get(short)
	require.True(t, found)
	require.Equal(t, []byte("short"), value)

	value, found = batch.Get(long)
	require.True(t, found)
	require.Equal(t, []byte("long"), value)
	require.Equal(t, 2, batch.count())
}

// A key written more than once in a block commits once, holding the last value written.
func TestBatchCommitsARewrittenKeyOnce(t *testing.T) {
	batch := newStateBatch()

	account := testAccountKey(1)
	batch.Put(account, []byte("first"))
	batch.Put(account, []byte("second"))
	require.Equal(t, 1, batch.count())

	staged := stagedPairs(t, batch, identifierCounters{})
	require.Equal(t, []byte("second"), staged[string(account)])
}

// Draining commits every staged write together with the identifier counters, and leaves the batch
// empty for the next block.
func TestDrainCommitsEveryWriteAndEmptiesTheBatch(t *testing.T) {
	batch := newStateBatch()

	const written = 500
	for i := range written {
		batch.Put(testAccountKey(i), []byte(fmt.Sprintf("account %d", i)))
		batch.Put(testSlotKey(i), []byte(fmt.Sprintf("slot %d", i)))
	}

	staged := stagedPairs(t, batch, identifierCounters{nextAccountID: 7, nextErc20ContractID: 9})
	require.Len(t, staged, 2*written+len(counterKeys))
	for i := range written {
		require.Equal(t, []byte(fmt.Sprintf("account %d", i)), staged[string(testAccountKey(i))])
		require.Equal(t, []byte(fmt.Sprintf("slot %d", i)), staged[string(testSlotKey(i))])
	}
	require.Equal(t, encodeCounter(7), staged[string(counterKeys[0])])
	require.Equal(t, encodeCounter(9), staged[string(counterKeys[1])])

	require.Equal(t, 0, batch.count())
	require.Len(t, stagedPairs(t, batch, identifierCounters{}), len(counterKeys))
}

// The executors write to the batch concurrently, so every write made by the pool has to survive into
// the commit regardless of which shard it landed on.
func TestBatchKeepsEveryConcurrentWrite(t *testing.T) {
	batch := newStateBatch()

	const (
		workers        = 16
		writesPerWorer = 200
	)
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range writesPerWorer {
				id := worker*writesPerWorer + i
				batch.Put(testAccountKey(id), []byte(fmt.Sprintf("%d", id)))
				batch.Get(testAccountKey(id))
			}
		}()
	}
	wg.Wait()

	staged := stagedPairs(t, batch, identifierCounters{})
	require.Len(t, staged, workers*writesPerWorer+len(counterKeys))
	for id := range workers * writesPerWorer {
		require.Equal(t, []byte(fmt.Sprintf("%d", id)), staged[string(testAccountKey(id))])
	}
}

// BenchmarkStateBatch drives the batch the way a block does: the pool executes the block's
// transactions concurrently, then a single drain. It reports the cost the benchmark harness adds to
// every block, which is the part of a measurement that is not the storage engine.
func BenchmarkStateBatch(b *testing.B) {
	const (
		transactionsPerBlock = 500
		readsPerTransaction  = 6
		writesPerTransaction = 5
		hotAccounts          = 100
		population           = 4096
	)

	accountKeys := make([][]byte, population)
	slotKeys := make([][]byte, population)
	for i := range population {
		accountKeys[i] = testAccountKey(i)
		slotKeys[i] = testSlotKey(i)
	}
	value := make([]byte, accountRecordLen)

	// Half of every transaction's accounts come from a small hot set, matching the account
	// distribution the benchmark generates.
	pick := func(list [][]byte, n int) []byte {
		if n%2 == 0 {
			return list[n%hotAccounts]
		}
		return list[n%len(list)]
	}

	batch := newStateBatch()
	workers := max(1, runtime.NumCPU()*2)
	share := transactionsPerBlock / workers
	var wg sync.WaitGroup

	b.ReportAllocs()
	b.ResetTimer()
	for block := 0; block < b.N; block++ {
		for worker := range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for t := range share {
					n := block*transactionsPerBlock + worker*share + t
					src, dst := pick(accountKeys, n), pick(accountKeys, n+1)
					srcSlot, dstSlot := pick(slotKeys, n), pick(slotKeys, n+1)

					for range readsPerTransaction {
						batch.Get(src)
					}
					batch.Put(src, value)
					batch.Put(dst, value)
					batch.Put(srcSlot, value)
					batch.Put(dstSlot, value)
					batch.Put(pick(accountKeys, n+2), value)
				}
			}()
		}
		wg.Wait()
		batch.drainToChangeSet(identifierCounters{})
	}
}

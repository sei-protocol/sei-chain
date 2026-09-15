package gigasim

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
)

// testAccountKey builds the shorter of the two key lengths the batch stages. The identifier goes at
// the end so that distinct identifiers give distinct keys.
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
	writes := batch.drainToChangeSet(counters)
	require.Len(t, writes.changeSets, 1)
	require.Equal(t, keys.EVMStoreKey, writes.changeSets[0].Name)

	staged := map[string][]byte{}
	for _, pair := range writes.changeSets[0].Changeset.Pairs {
		_, duplicate := staged[string(pair.Key)]
		require.False(t, duplicate, "key %x was committed twice in one block", pair.Key)
		staged[string(pair.Key)] = pair.Value
	}
	return staged
}

// Keys are held by value in a fixed-width array, so two keys that share a prefix and differ only in
// length have to stay distinct rather than colliding on the padding.
func TestBatchKeepsKeysOfDifferentLengthsApart(t *testing.T) {
	t.Parallel()

	batch := newStateBatch(2)

	short := keys.BuildEVMKey(accountKeyPrefix, make([]byte, keys.AddressLen))
	long := keys.BuildEVMKey(keys.EVMKeyStorage, make([]byte, storageKeyLen))

	batch.Put(short, []byte("short"))
	batch.Put(long, []byte("long"))
	require.Equal(t, 2, batch.count())

	staged := stagedPairs(t, batch, identifierCounters{})
	require.Equal(t, []byte("short"), staged[string(short)])
	require.Equal(t, []byte("long"), staged[string(long)])
}

// A key written more than once in a block commits once, holding the last value written.
func TestBatchCommitsARewrittenKeyOnce(t *testing.T) {
	t.Parallel()

	batch := newStateBatch(1)

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
	t.Parallel()

	const written = 500
	batch := newStateBatch(2 * written)

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

// The volume a block commits is counted as its changeset is assembled, which is what keeps the commit
// thread from walking the pairs again to find it. It has to be the whole block: every key and every
// value, the identifier counters included.
func TestDrainCountsEveryByteItStaged(t *testing.T) {
	t.Parallel()

	batch := newStateBatch(2)
	account, slot := testAccountKey(1), testSlotKey(1)
	batch.Put(account, []byte("account value"))
	batch.Put(slot, []byte("slot value"))

	writes := batch.drainToChangeSet(identifierCounters{})

	var expected int64
	for _, pair := range writes.changeSets[0].Changeset.Pairs {
		expected += int64(len(pair.Key) + len(pair.Value))
	}
	require.Equal(t, expected, writes.bytes)
}

// BenchmarkStateBatch drives the batch the way a block does: the generator stages every write as it
// builds the block, then drains it once. It reports the cost the benchmark harness adds to every
// block, which is the part of a measurement that is not the storage engine.
func BenchmarkStateBatch(b *testing.B) {
	const (
		transactionsPerBlock = 500
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

	batch := newStateBatch(writesPerTransaction*transactionsPerBlock + 1)

	b.ReportAllocs()
	b.ResetTimer()
	for block := 0; block < b.N; block++ {
		for t := range transactionsPerBlock {
			n := block*transactionsPerBlock + t
			batch.Put(pick(accountKeys, n), value)
			batch.Put(pick(accountKeys, n+1), value)
			batch.Put(pick(slotKeys, n), value)
			batch.Put(pick(slotKeys, n+1), value)
		}
		// The fee account is one write per block, whatever the transaction count.
		batch.Put(pick(accountKeys, 0), value)
		batch.drainToChangeSet(identifierCounters{})
	}
}

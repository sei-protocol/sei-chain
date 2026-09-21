package pebbledb

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// sortedKeys orders a batch's collected writes the way the pipeline does and names the result.
func sortedKeys(t *testing.T, batch types.Batch) []string {
	t.Helper()

	pb, ok := batch.(*pebbleBatch)
	require.True(t, ok, "NewBatch must return *pebbleBatch")

	handle := &commitHandle{entries: pb.entries, sorted: make(chan struct{})}
	sortHandle(handle)

	keys := make([]string, 0, len(handle.entries))
	for _, entry := range handle.entries {
		keys = append(keys, entry.key)
	}
	return keys
}

// A write set must land exactly as the equivalent byte-keyed calls would, including the distinction
// between a key present with an empty value and a key deleted, which the callers above rely on.
func TestSetAllWritesWhatIndividualCallsWrite(t *testing.T) {
	cfg := DefaultTestConfig(t)
	db := openDB(t, &cfg)

	require.NoError(t, db.Set([]byte("doomed"), []byte("v"), types.WriteOptions{Sync: false}))

	batch := db.NewBatch()

	require.NoError(t, batch.SetAll(map[string][]byte{
		"plain":  []byte("value"),
		"empty":  {},
		"doomed": nil,
	}))
	require.NoError(t, types.CommitAndWait(batch, types.WriteOptions{Sync: false}))

	value, err := db.Get([]byte("plain"))
	require.NoError(t, err)
	require.Equal(t, []byte("value"), value)

	empty, err := db.Get([]byte("empty"))
	require.NoError(t, err, "a key written with an empty value is present, not absent")
	require.Empty(t, empty)

	_, err = db.Get([]byte("doomed"))
	require.True(t, errorutils.IsNotFound(err), "a nil value must remove the key, got %v", err)
}

// A nil value means a delete only in SetAll, where it is the caller's write-map convention. Set is
// byte-oriented and a nil value there is an empty value, which callers write to mark key presence.
func TestSetWithANilValueIsNotADelete(t *testing.T) {
	cfg := DefaultTestConfig(t)
	db := openDB(t, &cfg)

	batch := db.NewBatch()

	require.NoError(t, batch.Set([]byte("marker"), nil))
	require.NoError(t, types.CommitAndWait(batch, types.WriteOptions{Sync: false}))

	value, err := db.Get([]byte("marker"))
	require.NoError(t, err, "a key set with a nil value is present, not absent")
	require.Empty(t, value)
}

// Ordering is a property of the batch, not of the method that filled it: writes reach pebble
// ascending whether they arrived through Set, Delete, or SetAll.
func TestBatchOrdersEveryWriteHoweverItArrived(t *testing.T) {
	cfg := DefaultTestConfig(t)
	db := openDB(t, &cfg)

	batch := db.NewBatch()

	// Deliberately neither sorted nor reverse sorted, and mixing all three entry points.
	require.NoError(t, batch.Set([]byte("mango"), []byte("v")))
	require.NoError(t, batch.Delete([]byte("zucchini")))
	require.NoError(t, batch.SetAll(map[string][]byte{
		"cherry": []byte("v"), "apple": []byte("v"), "banana": nil, "quince": []byte("v"),
	}))
	require.NoError(t, batch.Set([]byte("fig"), []byte("v")))

	keys := sortedKeys(t, batch)
	require.Len(t, keys, 7)
	require.True(t, slices.IsSorted(keys), "keys must reach pebble ascending, got %v", keys)
}

// Two writes to one key resolve the way they would outside a batch: the later one wins. Ordering the
// batch must not disturb that, which is why the sort is stable.
func TestLaterWriteToAKeyWins(t *testing.T) {
	cfg := DefaultTestConfig(t)
	db := openDB(t, &cfg)

	batch := db.NewBatch()

	require.NoError(t, batch.SetAll(map[string][]byte{"shared": []byte("older"), "zebra": []byte("v")}))
	require.NoError(t, batch.SetAll(map[string][]byte{"shared": []byte("newer"), "aardvark": nil}))
	require.NoError(t, types.CommitAndWait(batch, types.WriteOptions{Sync: false}))

	value, err := db.Get([]byte("shared"))
	require.NoError(t, err)
	require.Equal(t, "newer", string(value), "the later write must win")
}

// Batches reach pebble in the order they were committed, however their sorts interleave. The keys
// overlap so that only the commit order decides the surviving value.
func TestBatchesAreWrittenInCommitOrder(t *testing.T) {
	cfg := DefaultTestConfig(t)
	db := openDB(t, &cfg)

	var last types.CommitHandle
	for i := range 32 {
		batch := db.NewBatch()
		require.NoError(t, batch.SetAll(map[string][]byte{
			"contested": {byte(i)},
			// Padding, so the batches are unequal in size and their sorts finish out of order.
			string(rune('a' + i)): make([]byte, i*64),
		}))
		handle, err := batch.Commit(types.WriteOptions{Sync: false})
		require.NoError(t, err)
		last = handle
	}
	require.NoError(t, last.Wait())

	value, err := db.Get([]byte("contested"))
	require.NoError(t, err)
	require.Equal(t, []byte{31}, value, "the last batch committed must be the last one written")
}

// Len reports the size the collected writes will occupy encoded, which is what sizes a flush.
func TestLenTracksTheCollectedWrites(t *testing.T) {
	cfg := DefaultTestConfig(t)
	db := openDB(t, &cfg)

	batch := db.NewBatch()

	empty := batch.Len()
	require.NoError(t, batch.Set([]byte("key"), []byte("value")))
	require.Equal(t, empty+1+1+len("key")+1+len("value"), batch.Len())

	require.NoError(t, batch.Delete([]byte("gone")))
	require.Equal(t, empty+1+1+len("key")+1+len("value")+1+1+len("gone"), batch.Len(),
		"a delete carries no value length")
}

// A direct write must be ordered against a batch still in flight rather than racing past it, which
// is why it goes through the same pipeline. The batch is deliberately left unwaited.
func TestDirectWriteIsOrderedAgainstAnInFlightBatch(t *testing.T) {
	cfg := DefaultTestConfig(t)
	db := openDB(t, &cfg)

	batch := db.NewBatch()
	writes := map[string][]byte{"contested": []byte("from-batch")}
	for i := range 64 {
		// Padding, so the batch takes long enough to sort that an unordered write could overtake it.
		writes[string(rune('a'+i))] = make([]byte, 4096)
	}
	require.NoError(t, batch.SetAll(writes))
	_, err := batch.Commit(types.WriteOptions{Sync: false})
	require.NoError(t, err)

	require.NoError(t, db.Set([]byte("contested"), []byte("from-set"), types.WriteOptions{Sync: false}))

	value, err := db.Get([]byte("contested"))
	require.NoError(t, err)
	require.Equal(t, "from-set", string(value), "the later direct write must win")
}

// A committed batch is spent. Accepting a write afterwards would quietly open a second batch rather
// than joining the one the caller believes it is filling.
func TestCommittedBatchRefusesFurtherUse(t *testing.T) {
	cfg := DefaultTestConfig(t)
	db := openDB(t, &cfg)

	batch := db.NewBatch()
	require.NoError(t, batch.Set([]byte("key"), []byte("value")))
	require.NoError(t, types.CommitAndWait(batch, types.WriteOptions{Sync: false}))

	require.Error(t, batch.Set([]byte("late"), []byte("v")))
	require.Error(t, batch.Delete([]byte("late")))
	require.Error(t, batch.SetAll(map[string][]byte{"late": []byte("v")}))

	_, err := batch.Commit(types.WriteOptions{Sync: false})
	require.Error(t, err, "a second commit must be refused")

	_, err = db.Get([]byte("late"))
	require.True(t, errorutils.IsNotFound(err), "a refused write must not reach the database")
}

// Len reports what the batch holds, which committing does not change.
func TestLenIsUnchangedByCommitting(t *testing.T) {
	cfg := DefaultTestConfig(t)
	db := openDB(t, &cfg)

	batch := db.NewBatch()
	require.NoError(t, batch.Set([]byte("key"), []byte("value")))
	before := batch.Len()

	require.NoError(t, types.CommitAndWait(batch, types.WriteOptions{Sync: false}))
	require.Equal(t, before, batch.Len())
}

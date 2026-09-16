package pebbledb

import (
	"testing"

	"github.com/stretchr/testify/require"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// pebbleBatchHeaderLen is the fixed header pebble puts at the front of a batch's encoding, ahead of the
// records. Append moves records only, so it is the difference between the two batches' sizes and the
// size of the result.
const pebbleBatchHeaderLen = 12

// The string-keyed writes must be indistinguishable from the byte-keyed ones, including the distinction
// between a key present with an empty value and a key deleted, which the callers above rely on.
func TestBatchStringKeysWriteWhatByteKeysWrite(t *testing.T) {
	cfg := DefaultTestConfig(t)
	db := openDB(t, &cfg)

	require.NoError(t, db.Set([]byte("doomed"), []byte("v"), types.WriteOptions{Sync: false}))

	batch := db.NewBatch()
	t.Cleanup(func() { require.NoError(t, batch.Close()) })

	require.NoError(t, batch.SetString("plain", []byte("value")))
	require.NoError(t, batch.SetString("empty", []byte{}))
	require.NoError(t, batch.DeleteString("doomed"))
	require.NoError(t, batch.Commit(types.WriteOptions{Sync: false}))

	value, err := db.Get([]byte("plain"))
	require.NoError(t, err)
	require.Equal(t, []byte("value"), value)

	empty, err := db.Get([]byte("empty"))
	require.NoError(t, err, "a key written with an empty value is present, not absent")
	require.Empty(t, empty)

	_, err = db.Get([]byte("doomed"))
	require.True(t, errorutils.IsNotFound(err), "DeleteString must remove the key, got %v", err)
}

// Append has to put the absorbed batch's records after the receiver's own, because the order they sit in
// is the order pebble assigns sequence numbers in, and that is what decides which write to a key wins.
func TestBatchAppendKeepsTheAbsorbedRecordsLast(t *testing.T) {
	cfg := DefaultTestConfig(t)
	db := openDB(t, &cfg)

	older := db.NewBatch()
	t.Cleanup(func() { require.NoError(t, older.Close()) })
	require.NoError(t, older.SetString("shared", []byte("older")))
	require.NoError(t, older.SetString("only-older", []byte("v")))

	newer := db.NewBatch()
	t.Cleanup(func() { require.NoError(t, newer.Close()) })
	require.NoError(t, newer.SetString("shared", []byte("newer")))
	require.NoError(t, newer.SetString("only-newer", []byte("v")))

	olderLen, newerLen := older.Len(), newer.Len()
	require.NoError(t, older.Append(newer))

	require.Equal(t, olderLen+newerLen-pebbleBatchHeaderLen, older.Len(),
		"the absorbed records must count toward the size the flush batches by")
	require.Equal(t, newerLen, newer.Len(), "the absorbed batch must be left unchanged")

	require.NoError(t, older.Commit(types.WriteOptions{Sync: false}))

	for key, want := range map[string]string{
		"shared":     "newer",
		"only-older": "v",
		"only-newer": "v",
	} {
		value, err := db.Get([]byte(key))
		require.NoError(t, err, "key %q", key)
		require.Equal(t, want, string(value), "key %q", key)
	}
}

// foreignBatch stands in for a types.Batch from another engine. Its embedded interface is nil, which is
// safe because Append must reject it before calling anything on it.
type foreignBatch struct {
	types.Batch
}

func TestBatchAppendRejectsAForeignBatch(t *testing.T) {
	cfg := DefaultTestConfig(t)
	db := openDB(t, &cfg)

	batch := db.NewBatch()
	t.Cleanup(func() { require.NoError(t, batch.Close()) })

	err := batch.Append(&foreignBatch{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot append")
}

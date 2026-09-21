package pebbledb

import (
	"testing"

	"github.com/stretchr/testify/require"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

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

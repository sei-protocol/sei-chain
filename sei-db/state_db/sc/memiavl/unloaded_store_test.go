package memiavl

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCommitStore_IsLoaded covers the pre-load window that arises during
// state-sync: NewCommitStore returns a *CommitStore whose underlying *DB is
// nil until LoadVersion (or Rollback) opens it. Callers downstream of the
// migration router rely on IsLoaded to distinguish that transient state from
// a missing store name.
func TestCommitStore_IsLoaded(t *testing.T) {
	t.Run("nil receiver is not loaded", func(t *testing.T) {
		var cs *CommitStore
		require.False(t, cs.IsLoaded())
	})

	t.Run("freshly constructed store is not loaded", func(t *testing.T) {
		cs := NewCommitStore(t.TempDir(), DefaultConfig())
		require.False(t, cs.IsLoaded())
	})

	t.Run("LoadVersion marks the store loaded", func(t *testing.T) {
		cs := NewCommitStore(t.TempDir(), DefaultConfig())
		require.NoError(t, cs.Initialize([]string{"params"}))
		_, err := cs.LoadVersion(0, false)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, cs.Close()) })

		require.True(t, cs.IsLoaded())
	})
}

// TestCommitStore_GetChildStoreByName_BeforeLoad asserts that reads issued
// against an un-opened CommitStore return a nil interface rather than
// panicking on cs.db.TreeByName. The mempool reactor exercises this code
// path while state-sync is still applying chunks (see the panic stack in
// sei-tendermint/internal/mempool/reactor.go:139).
func TestCommitStore_GetChildStoreByName_BeforeLoad(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var cs *CommitStore
		require.Nil(t, cs.GetChildStoreByName("params"))
	})

	t.Run("constructed but not loaded", func(t *testing.T) {
		cs := NewCommitStore(t.TempDir(), DefaultConfig())
		require.Nil(t, cs.GetChildStoreByName("params"))
	})
}

// TestDB_TreeByName_NilReceiver guards the lowest layer: even if a caller
// somehow hands TreeByName a nil *DB (e.g. via a typed-nil field that
// escaped initialisation), the call must return nil instead of nil-derefing
// db.mtx.
func TestDB_TreeByName_NilReceiver(t *testing.T) {
	var db *DB
	require.Nil(t, db.TreeByName("params"))
}

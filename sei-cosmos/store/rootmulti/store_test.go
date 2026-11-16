package rootmulti

import (
	"testing"

	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
)

func TestLastCommitVersionMatchesLastCommitID(t *testing.T) {
	db := dbm.NewMemDB()
	key := types.NewKVStoreKey("store")
	newStore := func() *Store {
		s := NewStore(db)
		s.MountStoreWithDB(key, types.StoreTypeIAVL, nil)
		return s
	}

	store := newStore()
	require.NoError(t, store.LoadLatestVersion())
	require.Equal(t, store.LastCommitID().Version, store.LastCommitVersion())

	const blocks = 3
	for i := range blocks {
		store.GetKVStore(key).Set([]byte("k"), []byte{byte(i)})
		store.Commit(true)
		require.Equal(t, store.LastCommitID().Version, store.LastCommitVersion())
	}

	// An unloaded store has no commit info and falls back to the latest flushed version.
	reopened := newStore()
	require.Nil(t, reopened.LastCommitInfo())
	require.Equal(t, int64(blocks), reopened.LastCommitVersion())
	require.Equal(t, reopened.LastCommitID().Version, reopened.LastCommitVersion())
}

package commitment

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/storev2/query"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/stretchr/testify/require"
)

func TestLastCommitID(t *testing.T) {
	tree := memiavl.New(0)
	store := NewStore(tree, query.Limits{})
	require.Equal(t, types.CommitID{Hash: tree.RootHash()}, store.LastCommitID())
}

func TestQuerySubspace_EmptyPrefixRejected(t *testing.T) {
	tree := memiavl.New(0)
	store := NewStore(tree, query.Limits{})

	resp := store.Query(t.Context(), abci.RequestQuery{Path: "/subspace"})
	require.NotEqualValues(t, 0, resp.Code)
	require.Contains(t, resp.Log, "subspace prefix must not be empty")
}

func TestQuerySubspace_NarrowPrefixSucceeds(t *testing.T) {
	tree := memiavl.New(0)
	tree.Set([]byte("ab1"), []byte("v1"))
	tree.Set([]byte("ab2"), []byte("v2"))
	tree.Set([]byte("xy1"), []byte("v3"))
	store := NewStore(tree, query.Limits{MaxPairs: 10, MaxBytes: query.DefaultMaxSubspaceBytes})

	resp := store.Query(t.Context(), abci.RequestQuery{
		Path: "/subspace",
		Data: []byte("ab"),
	})
	require.EqualValues(t, 0, resp.Code)
	require.NotEmpty(t, resp.Value)
}

func TestQueryKey_UnaffectedBySubspaceLimits(t *testing.T) {
	tree := memiavl.New(0)
	key := []byte("k")
	tree.Set(key, []byte("v"))
	store := NewStore(tree, query.Limits{MaxPairs: 1, MaxBytes: 1})

	resp := store.Query(t.Context(), abci.RequestQuery{
		Path: "/key",
		Data: key,
	})
	require.EqualValues(t, 0, resp.Code)
	require.Equal(t, []byte("v"), resp.Value)
}

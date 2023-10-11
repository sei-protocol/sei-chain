package memiavlstore

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/store/types"
	"github.com/sei-protocol/sei-db/memiavl/db"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
)

func TestLastCommitID(t *testing.T) {
	tree := memiavl.New(100)
	store := New(tree, log.NewNopLogger())
	require.Equal(t, types.CommitID{Hash: tree.RootHash()}, store.LastCommitID())
}

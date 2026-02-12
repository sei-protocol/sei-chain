package commitment

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	"github.com/stretchr/testify/require"
)

func TestLastCommitID(t *testing.T) {
	tree := memiavl.New(100)
	store := NewStore(tree, log.NewNopLogger())
	require.Equal(t, types.CommitID{Hash: tree.RootHash()}, store.LastCommitID())
}

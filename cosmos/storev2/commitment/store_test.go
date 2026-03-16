package commitment

import (
	"testing"

	"github.com/sei-protocol/sei-chain/cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/stretchr/testify/require"
)

func TestLastCommitID(t *testing.T) {
	tree := memiavl.New(100)
	store := NewStore(tree)
	require.Equal(t, types.CommitID{Hash: tree.RootHash()}, store.LastCommitID())
}

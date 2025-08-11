package commitment

import (
	"testing"

	"github.com/sei-protocol/sei-chain/cosmos-sdk/store/types"
	"github.com/sei-protocol/sei-chain/sei-db/sc/memiavl"
	"github.com/stretchr/testify/require"
	"github.com/sei-protocol/sei-chain/tendermint/libs/log"
)

func TestLastCommitID(t *testing.T) {
	tree := memiavl.New(100)
	store := NewStore(tree, log.NewNopLogger())
	require.Equal(t, types.CommitID{Hash: tree.RootHash()}, store.LastCommitID())
}

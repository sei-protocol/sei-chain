package rootmulti

import (
	memiavl "github.com/sei-protocol/sei-db/sc/memiavl/db"
	"testing"

	"github.com/cosmos/cosmos-sdk/store/types"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
)

func TestLastCommitID(t *testing.T) {
	store := NewStore(t.TempDir(), log.NewNopLogger(), memiavl.Options{})
	require.Equal(t, types.CommitID{}, store.LastCommitID())
}

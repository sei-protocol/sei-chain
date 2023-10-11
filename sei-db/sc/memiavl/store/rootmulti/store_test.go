package rootmulti

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/store/types"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
)

func TestLastCommitID(t *testing.T) {
	store := NewStore(t.TempDir(), log.NewNopLogger(), false, false)
	require.Equal(t, types.CommitID{}, store.LastCommitID())
}

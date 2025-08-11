package rootmulti

import (
	"testing"

	"github.com/sei-protocol/sei-chain/cosmos-sdk/store/types"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/stretchr/testify/require"
	"github.com/sei-protocol/sei-chain/tendermint/libs/log"
)

func TestLastCommitID(t *testing.T) {
	store := NewStore(t.TempDir(), log.NewNopLogger(), config.StateCommitConfig{}, config.StateStoreConfig{}, false)
	require.Equal(t, types.CommitID{}, store.LastCommitID())
}

package migrations_test

import (
	"testing"

	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/migrations"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
	tmtypes "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestRemoveTxHashes(t *testing.T) {
	k := testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.NewContext(false, tmtypes.Header{})
	store := ctx.KVStore(k.GetStoreKey())
	store.Set(types.TxHashesKey(1), []byte{1})
	store.Set(types.TxHashesKey(2), []byte{2})
	require.Equal(t, []byte{1}, store.Get(types.TxHashesKey(1)))
	require.Equal(t, []byte{2}, store.Get(types.TxHashesKey(2)))
	require.NoError(t, migrations.RemoveTxHashes(ctx, &k))
	require.Nil(t, store.Get(types.TxHashesKey(1)))
	require.Nil(t, store.Get(types.TxHashesKey(2)))
}

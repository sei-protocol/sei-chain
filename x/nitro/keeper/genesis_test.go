package keeper_test

import (
	"encoding/hex"
	"testing"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/nitro/types"
	"github.com/stretchr/testify/require"
)

func TestInitGenesis(t *testing.T) {
	keeper, ctx := keepertest.NitroKeeper(t)
	genState := types.GenesisState{}
	keeper.InitGenesis(ctx, genState)
	require.Equal(t, 0, len(keeper.GetParams(ctx).WhitelistedTxSenders))

	genState.Params = types.Params{WhitelistedTxSenders: []string{"sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m"}}
	keeper.InitGenesis(ctx, genState)
	require.Equal(t, 1, len(keeper.GetParams(ctx).WhitelistedTxSenders))

	genState.Sender = "someone"
	genState.Slot = 5
	genState.StateRoot = "1234"
	genState.Txs = []string{"5678"}
	keeper.InitGenesis(ctx, genState)
	sender, exists := keeper.GetSender(ctx, 5)
	require.True(t, exists)
	require.Equal(t, "someone", sender)
	rootbz, err := keeper.GetStateRoot(ctx, 5)
	require.Nil(t, err)
	require.Equal(t, "1234", hex.EncodeToString(rootbz))
	txsbz, err := keeper.GetTransactionData(ctx, 5)
	require.Nil(t, err)
	require.Equal(t, "5678", hex.EncodeToString(txsbz[0]))
}

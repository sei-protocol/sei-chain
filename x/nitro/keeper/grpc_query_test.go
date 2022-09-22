package keeper_test

import (
	"encoding/hex"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/nitro/types"
	"github.com/stretchr/testify/require"
)

func TestParams(t *testing.T) {
	keeper, ctx := keepertest.NitroKeeper(t)
	keeper.SetParams(ctx, types.Params{WhitelistedTxSenders: []string{"sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m"}})
	res, err := keeper.Params(sdk.WrapSDKContext(ctx), &types.QueryParamsRequest{})
	require.Nil(t, err)
	require.Equal(t, 1, len(res.Params.WhitelistedTxSenders))
	require.Equal(t, "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m", res.Params.WhitelistedTxSenders[0])
}

func TestRecordedTransactionData(t *testing.T) {
	keeper, ctx := keepertest.NitroKeeper(t)
	decoded, _ := hex.DecodeString("1234")
	keeper.SetTransactionData(ctx, 1, [][]byte{decoded})
	res, err := keeper.RecordedTransactionData(sdk.WrapSDKContext(ctx), &types.QueryRecordedTransactionDataRequest{Slot: 1})
	require.Nil(t, err)
	require.Equal(t, 1, len(res.Txs))
	require.Equal(t, "1234", res.Txs[0])
}

func TestStateRoot(t *testing.T) {
	keeper, ctx := keepertest.NitroKeeper(t)
	decoded, _ := hex.DecodeString("1234")
	keeper.SetStateRoot(ctx, 1, decoded)
	res, err := keeper.StateRoot(sdk.WrapSDKContext(ctx), &types.QueryStateRootRequest{Slot: 1})
	require.Nil(t, err)
	require.Equal(t, "1234", res.Root)
}

func TestSender(t *testing.T) {
	keeper, ctx := keepertest.NitroKeeper(t)
	keeper.SetSender(ctx, 1, "someone")
	res, err := keeper.Sender(sdk.WrapSDKContext(ctx), &types.QuerySenderRequest{Slot: 1})
	require.Nil(t, err)
	require.Equal(t, "someone", res.Sender)
}

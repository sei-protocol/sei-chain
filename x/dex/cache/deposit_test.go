package dex_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	dex "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestDepositAdd(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	deposits := dex.NewMemState(keeper.GetMemStoreKey()).GetDepositInfo(ctx, types.ContractAddress(keepertest.TestContract))
	deposit := types.DepositInfoEntry{
		Creator: "abc",
		Amount:  sdk.MustNewDecFromStr("1.2"),
	}
	deposits.Add(&deposit)
	depositsState := deposits.Get()
	require.Equal(t, 1, len(depositsState))
	require.Equal(t, sdk.MustNewDecFromStr("1.2"), depositsState[0].Amount)

	deposit = types.DepositInfoEntry{
		Creator: "abc",
		Amount:  sdk.MustNewDecFromStr("1.3"),
	}
	deposits.Add(&deposit)
	depositsState = deposits.Get()
	require.Equal(t, 1, len(depositsState))
	require.Equal(t, sdk.MustNewDecFromStr("2.5"), depositsState[0].Amount)

	deposit = types.DepositInfoEntry{
		Creator: "def",
		Amount:  sdk.MustNewDecFromStr("1.1"),
	}
	deposits.Add(&deposit)
	depositsState = deposits.Get()
	require.Equal(t, 2, len(depositsState))
	if depositsState[0].Creator == "abc" {
		require.Equal(t, sdk.MustNewDecFromStr("2.5"), depositsState[0].Amount)
		require.Equal(t, sdk.MustNewDecFromStr("1.1"), depositsState[1].Amount)
	} else {
		require.Equal(t, sdk.MustNewDecFromStr("2.5"), depositsState[1].Amount)
		require.Equal(t, sdk.MustNewDecFromStr("1.1"), depositsState[0].Amount)
	}
}

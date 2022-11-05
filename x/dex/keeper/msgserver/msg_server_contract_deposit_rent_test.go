package msgserver_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/msgserver"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/require"
)

func TestDepositRent(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	testAccount, _ := sdk.AccAddressFromBech32("sei1yezq49upxhunjjhudql2fnj5dgvcwjj87pn2wx")
	amounts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10000000)))
	bankkeeper := keeper.BankKeeper
	bankkeeper.MintCoins(ctx, minttypes.ModuleName, amounts)
	bankkeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, testAccount, amounts)

	server := msgserver.NewMsgServerImpl(*keeper)
	contract := types.ContractInfoV2{
		CodeId:       1,
		ContractAddr: TestContractA,
		Creator:      testAccount.String(),
		RentBalance:  1000000,
	}
	_, err := server.RegisterContract(wctx, &types.MsgRegisterContract{
		Creator:  testAccount.String(),
		Contract: &contract,
	})
	require.NoError(t, err)
	_, err = keeper.GetContract(ctx, TestContractA)
	require.NoError(t, err)
	balance := keeper.BankKeeper.GetBalance(ctx, testAccount, "usei")
	require.Equal(t, int64(9000000), balance.Amount.Int64())
	_, err = server.ContractDepositRent(wctx, &types.MsgContractDepositRent{
		Sender:       testAccount.String(),
		ContractAddr: TestContractA,
		Amount:       1000000,
	})
	require.NoError(t, err)
	_, err = keeper.GetContract(ctx, TestContractA)
	require.NoError(t, err)
	balance = keeper.BankKeeper.GetBalance(ctx, testAccount, "usei")
	require.Equal(t, int64(8000000), balance.Amount.Int64())
}

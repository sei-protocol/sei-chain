package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/bank/keeper"
	"github.com/cosmos/cosmos-sdk/x/bank/types"
)

func (suite *IntegrationTestSuite) TestExportGenesis() {
	app, ctx := suite.app, suite.ctx

	expectedMetadata := suite.getTestMetadata()
	expectedBalances, totalSupply := suite.getTestBalancesAndSupply()
	for i := range []int{1, 2} {
		app.BankKeeper.SetDenomMetaData(ctx, expectedMetadata[i])
		accAddr, err1 := sdk.AccAddressFromBech32(expectedBalances[i].Address)
		if err1 != nil {
			panic(err1)
		}
		// set balances via mint and send
		suite.
			Require().
			NoError(app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, expectedBalances[i].Coins))
		suite.
			Require().
			NoError(app.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, accAddr, expectedBalances[i].Coins))
	}
	suite.
		Require().
		NoError(
			app.BankKeeper.SendCoinsAndWei(ctx, expectedBalances[0].GetAddress(), expectedBalances[1].GetAddress(), sdk.ZeroInt(), sdk.OneInt()))
	app.BankKeeper.SetParams(ctx, types.DefaultParams())

	exportGenesis := app.BankKeeper.ExportGenesis(ctx)

	suite.Require().Len(exportGenesis.Params.SendEnabled, 0)
	suite.Require().Equal(types.DefaultParams().DefaultSendEnabled, exportGenesis.Params.DefaultSendEnabled)
	suite.Require().Equal(totalSupply, exportGenesis.Supply)
	expectedBalances[0].Coins = expectedBalances[0].Coins.Sub(sdk.NewCoins(sdk.NewCoin(sdk.MustGetBaseDenom(), sdk.OneInt())))
	expectedWeiBalances := []types.WeiBalance{
		{Amount: keeper.OneUseiInWei.Sub(sdk.OneInt()), Address: expectedBalances[0].Address},
		{Amount: sdk.OneInt(), Address: expectedBalances[1].Address},
	}
	suite.Require().Equal(expectedBalances, exportGenesis.Balances)
	suite.Require().Equal(expectedMetadata, exportGenesis.DenomMetadata)
	suite.Require().Equal(expectedWeiBalances, exportGenesis.WeiBalances)
}

package keeper_test

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/query"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/keeper"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
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

func (suite *IntegrationTestSuite) getTestBalancesAndSupply() ([]types.Balance, sdk.Coins) {
	addr1, _ := sdk.AccAddressFromBech32("sei10xwrnrezdg227cgt82az7f7j47q3zklvu5ax6k")
	addr2, _ := sdk.AccAddressFromBech32("sei1rs8v2232uv5nw8c88ruvyjy08mmxfx25pur3pl")
	addr1Balance := sdk.Coins{sdk.NewInt64Coin("testcoin3", 10)}
	addr2Balance := sdk.Coins{sdk.NewInt64Coin("testcoin1", 32), sdk.NewInt64Coin("testcoin2", 34), sdk.NewInt64Coin(sdk.DefaultBondDenom, 2)}

	totalSupply := addr1Balance
	totalSupply = totalSupply.Add(addr2Balance...)

	return []types.Balance{
		{Address: addr2.String(), Coins: addr2Balance},
		{Address: addr1.String(), Coins: addr1Balance},
	}, totalSupply
}

func (suite *IntegrationTestSuite) TestInitGenesis() {
	m := types.Metadata{Description: sdk.DefaultBondDenom, Base: sdk.DefaultBondDenom, Display: sdk.DefaultBondDenom}
	g := types.DefaultGenesisState()
	g.DenomMetadata = []types.Metadata{m}
	bk := suite.app.BankKeeper
	bk.InitGenesis(suite.ctx, g)

	m2, found := bk.GetDenomMetaData(suite.ctx, m.Base)
	suite.Require().True(found)
	suite.Require().Equal(m, m2)
}

func (suite *IntegrationTestSuite) TestTotalSupply() {
	// Prepare some test data.
	defaultGenesis := types.DefaultGenesisState()
	balances := []types.Balance{
		{Coins: sdk.NewCoins(sdk.NewCoin("foocoin", sdk.NewInt(1))), Address: "sei10xwrnrezdg227cgt82az7f7j47q3zklvu5ax6k"},
		{Coins: sdk.NewCoins(sdk.NewCoin("barcoin", sdk.NewInt(1))), Address: "sei1l976cvcndrr6hnuyzn93azaxx8sc2xre5crtpz"},
		{Coins: sdk.NewCoins(sdk.NewCoin("foocoin", sdk.NewInt(10)), sdk.NewCoin("barcoin", sdk.NewInt(20))), Address: "sei1rs8v2232uv5nw8c88ruvyjy08mmxfx25pur3pl"},
	}
	weiBalances := []types.WeiBalance{
		{Amount: sdk.OneInt(), Address: "sei10xwrnrezdg227cgt82az7f7j47q3zklvu5ax6k"},
		{Amount: keeper.OneUseiInWei.Sub(sdk.OneInt()), Address: "sei1rs8v2232uv5nw8c88ruvyjy08mmxfx25pur3pl"},
	}
	totalSupply := sdk.NewCoins(sdk.NewCoin("foocoin", sdk.NewInt(11)), sdk.NewCoin("barcoin", sdk.NewInt(21)), sdk.NewCoin(sdk.DefaultBondDenom, sdk.OneInt()))

	testcases := []struct {
		name        string
		genesis     *types.GenesisState
		expSupply   sdk.Coins
		expPanic    bool
		expPanicMsg string
	}{
		{
			"calculation NOT matching genesis Supply field",
			types.NewGenesisState(defaultGenesis.Params, balances, sdk.NewCoins(sdk.NewCoin("wrongcoin", sdk.NewInt(1))), defaultGenesis.DenomMetadata, weiBalances),
			nil, true, "genesis supply is incorrect, expected 1wrongcoin, got 21barcoin,11foocoin,1usei",
		},
		{
			"calculation matches genesis Supply field",
			types.NewGenesisState(defaultGenesis.Params, balances, totalSupply, defaultGenesis.DenomMetadata, weiBalances),
			totalSupply, false, "",
		},
		{
			"calculation is correct, empty genesis Supply field",
			types.NewGenesisState(defaultGenesis.Params, balances, nil, defaultGenesis.DenomMetadata, weiBalances),
			totalSupply, false, "",
		},
	}

	for _, tc := range testcases {
		tc := tc
		suite.Run(tc.name, func() {
			if tc.expPanic {
				suite.PanicsWithError(tc.expPanicMsg, func() { suite.app.BankKeeper.InitGenesis(suite.ctx, tc.genesis) })
			} else {
				suite.app.BankKeeper.InitGenesis(suite.ctx, tc.genesis)
				totalSupply, _, err := suite.app.BankKeeper.GetPaginatedTotalSupply(suite.ctx, &query.PageRequest{Limit: query.MaxLimit})
				suite.Require().NoError(err)
				suite.Require().Equal(tc.expSupply, totalSupply)
			}
		})
	}
}

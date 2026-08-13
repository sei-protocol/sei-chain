package keeper_test

import (
	"github.com/cosmos/cosmos-sdk/simapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	"github.com/cosmos/cosmos-sdk/x/bank/types"
)

func (suite *IntegrationTestSuite) TestDeferredCacheUpsertBalances() {
	// add module accounts to supply keeper
	ctx := suite.ctx
	authKeeper, keeper := suite.initKeepersWithmAccPerms(make(map[string]bool))
	authKeeper.SetModuleAccount(ctx, multiPermAcc)
	app := suite.app
	app.BankKeeper = keeper

	addr1 := sdk.AccAddress("addr1_______________")
	acc1 := authKeeper.NewAccountWithAddress(ctx, addr1)
	authKeeper.SetAccount(ctx, acc1)

	bankBalances := sdk.NewCoins(newFooCoin(20), newBarCoin(30))
	// set up bank balances
	suite.Require().NoError(simapp.FundAccount(app.BankKeeper, ctx, addr1, bankBalances))
	suite.Require().NoError(simapp.FundAccount(app.BankKeeper, ctx, multiPermAcc.GetAddress(), bankBalances))
	// we initialize a deferrred cache to test functions directly as opposed to via bankkeeper functionality
	deferredCache := bankkeeper.NewDeferredCache(app.AppCodec(), app.GetMemKey(types.DeferredCacheStoreKey))

	// get deferred balance for a denom - should be zero
	coin := deferredCache.GetBalance(ctx, multiPermAcc.GetAddress(), 2, fooDenom)
	suite.Require().Equal(sdk.NewCoin(fooDenom, sdk.ZeroInt()), coin)
	// perform upsert - should have a balance now of 10foo
	err := deferredCache.UpsertBalances(ctx, multiPermAcc.GetAddress(), 2, sdk.NewCoins(newFooCoin(10)))
	suite.Require().NoError(err)
	suite.Require().Equal(newFooCoin(10), deferredCache.GetBalance(ctx, multiPermAcc.GetAddress(), 2, fooDenom))
	// Add foo coin on a different tx index - should have same balance of 10 for old tx index AND an entry for new tx index
	err = deferredCache.UpsertBalances(ctx, multiPermAcc.GetAddress(), 1, sdk.NewCoins(newFooCoin(15)))
	suite.Require().NoError(err)
	suite.Require().Equal(newFooCoin(10), deferredCache.GetBalance(ctx, multiPermAcc.GetAddress(), 2, fooDenom))
	suite.Require().Equal(newFooCoin(15), deferredCache.GetBalance(ctx, multiPermAcc.GetAddress(), 1, fooDenom))
	// upsert on an existing index with multiple coins-> should increment the value
	err = deferredCache.UpsertBalances(ctx, multiPermAcc.GetAddress(), 2, sdk.NewCoins(newFooCoin(10), newBarCoin(5)))
	suite.Require().NoError(err)
	suite.Require().Equal(newFooCoin(20), deferredCache.GetBalance(ctx, multiPermAcc.GetAddress(), 2, fooDenom))
	suite.Require().Equal(newBarCoin(5), deferredCache.GetBalance(ctx, multiPermAcc.GetAddress(), 2, barDenom))
	suite.Require().Equal(newFooCoin(15), deferredCache.GetBalance(ctx, multiPermAcc.GetAddress(), 1, fooDenom))

	// upsert on a different module acc
	err = deferredCache.UpsertBalances(ctx, randomPermAcc.GetAddress(), 2, sdk.NewCoins(newFooCoin(7)))
	suite.Require().NoError(err)
	suite.Require().Equal(newFooCoin(20), deferredCache.GetBalance(ctx, multiPermAcc.GetAddress(), 2, fooDenom))
	suite.Require().Equal(newFooCoin(7), deferredCache.GetBalance(ctx, randomPermAcc.GetAddress(), 2, fooDenom))

	// upsert with invalid balance should fail
	suite.Require().Error(deferredCache.UpsertBalances(ctx, randomPermAcc.GetAddress(), 2, []sdk.Coin{{Denom: fooDenom, Amount: sdk.NewInt(-5)}}))

	count := 0
	// iterate and count entries
	deferredCache.IterateDeferredBalances(ctx, func(moduleAddr sdk.AccAddress, balance sdk.Coin) bool {
		count += 1
		return false
	})
	suite.Require().Equal(4, count)

	// write deferred balances should increase foo by 25 for multiPermAcc
	app.BankKeeper.WriteDeferredBalances(ctx)

	count = 0
	// iterate and count should have no entries after writing balances (since it clears)
	deferredCache.IterateDeferredBalances(ctx, func(moduleAddr sdk.AccAddress, balance sdk.Coin) bool {
		count += 1
		return false
	})
	suite.Require().Equal(0, count)

	// assert module balances correct
	expectedBankBalances := sdk.NewCoins(newFooCoin(55), newBarCoin(35))
	bals := app.BankKeeper.GetAllBalances(ctx, multiPermAcc.GetAddress())
	suite.Require().Equal(expectedBankBalances, bals)

	// assert module balances correct for other module acc
	expectedBankBalances = sdk.NewCoins(newFooCoin(7))
	bals = app.BankKeeper.GetAllBalances(ctx, randomPermAcc.GetAddress())
	suite.Require().Equal(expectedBankBalances, bals)
}

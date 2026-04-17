package keeper_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/apptesting"
	"github.com/sei-protocol/sei-chain/giga/deps/xbank/keeper"
	"github.com/sei-protocol/sei-chain/giga/deps/xbank/types"
	"github.com/sei-protocol/sei-chain/occ_tests/utils"
	"github.com/sei-protocol/sei-chain/sei-cosmos/baseapp"
	tmtime "github.com/sei-protocol/sei-chain/sei-cosmos/std"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/keeper"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	vesting "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/vesting/types"
	cosmosbanktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/suite"
)

const (
	fooDenom           = "foo"
	barDenom           = "bar"
	factoryDenomPrefix = "factory"
	initialPower       = int64(100)
	holder             = "holder"
	multiPerm          = "multiple permissions account"
	randomPerm         = "random permission"
)

var (
	holderAcc     = authtypes.NewEmptyModuleAccount(holder)
	burnerAcc     = authtypes.NewEmptyModuleAccount(authtypes.Burner, authtypes.Burner)
	minterAcc     = authtypes.NewEmptyModuleAccount(authtypes.Minter, authtypes.Minter)
	multiPermAcc  = authtypes.NewEmptyModuleAccount(multiPerm, authtypes.Burner, authtypes.Minter, authtypes.Staking)
	randomPermAcc = authtypes.NewEmptyModuleAccount(randomPerm, "random")

	// The default power validators are initialized to have within tests
	initTokens = sdk.TokensFromConsensusPower(initialPower, sdk.DefaultPowerReduction)
	initCoins  = sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, initTokens))
)

func newFooCoin(amt int64) sdk.Coin {
	return sdk.NewInt64Coin(fooDenom, amt)
}

func newFactoryFooCoin(address sdk.AccAddress, amt int64) sdk.Coin {
	return sdk.NewInt64Coin(fmt.Sprintf("%s/%s/%s", factoryDenomPrefix, address, fooDenom), amt)
}

func newBarCoin(amt int64) sdk.Coin {
	return sdk.NewInt64Coin(barDenom, amt)
}

type IntegrationTestSuite struct {
	suite.Suite

	app *app.App
	ctx sdk.Context
}

func (suite *IntegrationTestSuite) initKeepersWithmAccPerms(blockedAddrs map[string]bool) (authkeeper.AccountKeeper, keeper.BaseKeeper) {
	a := suite.app
	maccPerms := app.GetMaccPerms()
	appCodec := app.MakeEncodingConfig().Marshaler

	maccPerms[holder] = nil
	maccPerms[authtypes.Burner] = []string{authtypes.Burner}
	maccPerms[authtypes.Minter] = []string{authtypes.Minter}
	maccPerms[multiPerm] = []string{authtypes.Burner, authtypes.Minter, authtypes.Staking}
	maccPerms[randomPerm] = []string{"random"}
	authKeeper := authkeeper.NewAccountKeeper(
		appCodec, a.GetKey(types.StoreKey), a.GetSubspace(types.ModuleName),
		authtypes.ProtoBaseAccount, maccPerms,
	)
	keeper := keeper.NewBaseKeeperWithDeferredCache(
		appCodec, a.GetKey(types.StoreKey), authKeeper,
		a.GetSubspace(types.ModuleName), blockedAddrs, a.GetMemKey(types.DeferredCacheStoreKey),
	)

	return authKeeper, keeper
}

func (suite *IntegrationTestSuite) SetupTest() {
	accts := utils.NewTestAccounts(1)
	sdk.RegisterDenom(sdk.DefaultBondDenom, sdk.OneDec())
	blockTime := time.Now()
	wrapper := app.NewGigaTestWrapper(suite.T(), blockTime, accts[0].PublicKey, true, false, func(ba *baseapp.BaseApp) {
		ba.SetOccEnabled(false)
		ba.SetConcurrencyWorkers(1)
	})
	a := wrapper.App
	ctx := wrapper.Ctx
	ctx = ctx.WithBlockHeader(tmproto.Header{
		Height:  ctx.BlockHeader().Height,
		ChainID: ctx.BlockHeader().ChainID,
		Time:    blockTime,
	})
	ctx = ctx.WithMultiStore(ctx.MultiStore().CacheMultiStore())

	a.AccountKeeper.SetParams(ctx, authtypes.DefaultParams())
	a.GigaBankKeeper.SetParams(ctx, cosmosbanktypes.DefaultParams())

	suite.app = a
	suite.ctx = ctx
}

func (suite *IntegrationTestSuite) TestSendCoinsAndWei() {
	ctx := suite.ctx
	require := suite.Require()
	sdk.RegisterDenom(sdk.DefaultBondDenom, sdk.OneDec())
	_, keeper := suite.initKeepersWithmAccPerms(make(map[string]bool))
	amt := sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, sdk.NewInt(100)))
	require.NoError(keeper.MintCoins(ctx, authtypes.Minter, amt))
	addr1 := sdk.AccAddress([]byte("addr1_______________"))
	addr2 := sdk.AccAddress([]byte("addr2_______________"))
	addr3 := sdk.AccAddress([]byte("addr3_______________"))
	require.NoError(keeper.SendCoinsFromModuleToAccount(ctx, authtypes.Minter, addr1, amt))
	// should no-op if sending zero
	require.NoError(keeper.SendCoinsAndWei(ctx, addr1, addr2, sdk.ZeroInt(), sdk.ZeroInt()))
	require.Equal(sdk.ZeroInt(), keeper.GetWeiBalance(ctx, addr1))
	require.Equal(sdk.ZeroInt(), keeper.GetWeiBalance(ctx, addr2))
	require.Equal(sdk.NewInt(100), keeper.GetBalance(ctx, addr1, sdk.DefaultBondDenom).Amount)
	require.Equal(sdk.ZeroInt(), keeper.GetBalance(ctx, addr2, sdk.DefaultBondDenom).Amount)
	// should just do usei send if wei is zero
	require.NoError(keeper.SendCoinsAndWei(ctx, addr1, addr3, sdk.NewInt(50), sdk.ZeroInt()))
	require.Equal(sdk.ZeroInt(), keeper.GetWeiBalance(ctx, addr1))
	require.Equal(sdk.ZeroInt(), keeper.GetWeiBalance(ctx, addr3))
	require.Equal(sdk.NewInt(50), keeper.GetBalance(ctx, addr1, sdk.DefaultBondDenom).Amount)
	require.Equal(sdk.NewInt(50), keeper.GetBalance(ctx, addr3, sdk.DefaultBondDenom).Amount)
	// sender gets escrowed one usei, recipient does not get redeemed
	require.NoError(keeper.SendCoinsAndWei(ctx, addr1, addr2, sdk.NewInt(1), sdk.NewInt(1)))
	require.Equal(sdk.NewInt(999_999_999_999), keeper.GetWeiBalance(ctx, addr1))
	require.Equal(sdk.OneInt(), keeper.GetWeiBalance(ctx, addr2))
	require.Equal(sdk.NewInt(48), keeper.GetBalance(ctx, addr1, sdk.DefaultBondDenom).Amount)
	require.Equal(sdk.OneInt(), keeper.GetBalance(ctx, addr2, sdk.DefaultBondDenom).Amount)
	// sender does not get escrowed due to sufficient wei balance, recipient does not get redeemed
	require.NoError(keeper.SendCoinsAndWei(ctx, addr1, addr3, sdk.NewInt(1), sdk.NewInt(999_999_999_999)))
	require.Equal(sdk.ZeroInt(), keeper.GetWeiBalance(ctx, addr1))
	require.Equal(sdk.NewInt(999_999_999_999), keeper.GetWeiBalance(ctx, addr3))
	require.Equal(sdk.NewInt(47), keeper.GetBalance(ctx, addr1, sdk.DefaultBondDenom).Amount)
	require.Equal(sdk.NewInt(51), keeper.GetBalance(ctx, addr3, sdk.DefaultBondDenom).Amount)
	// sender gets escrowed and recipient gets redeemed
	require.NoError(keeper.SendCoinsAndWei(ctx, addr1, addr3, sdk.NewInt(1), sdk.NewInt(2)))
	require.Equal(sdk.NewInt(999_999_999_998), keeper.GetWeiBalance(ctx, addr1))
	require.Equal(sdk.NewInt(1), keeper.GetWeiBalance(ctx, addr3))
	require.Equal(sdk.NewInt(45), keeper.GetBalance(ctx, addr1, sdk.DefaultBondDenom).Amount)
	require.Equal(sdk.NewInt(53), keeper.GetBalance(ctx, addr3, sdk.DefaultBondDenom).Amount)
}

func (suite *IntegrationTestSuite) TestSendCoinsFromModuleToAccount_Blocklist() {
	ctx := suite.ctx

	// add module accounts to supply keeper
	addr1 := sdk.AccAddress([]byte("addr1_______________"))
	_, keeper := suite.initKeepersWithmAccPerms(map[string]bool{addr1.String(): true})

	suite.Require().NoError(keeper.MintCoins(ctx, minttypes.ModuleName, initCoins))
	suite.Require().Error(keeper.SendCoinsFromModuleToAccount(
		ctx, minttypes.ModuleName, addr1, initCoins,
	))
}

func (suite *IntegrationTestSuite) TestSupply_SendCoins() {
	ctx := suite.ctx

	// add module accounts to supply keeper
	authKeeper, keeper := suite.initKeepersWithmAccPerms(make(map[string]bool))

	baseAcc := authKeeper.NewAccountWithAddress(ctx, authtypes.NewModuleAddress("baseAcc"))

	// set initial balances
	suite.
		Require().
		NoError(keeper.MintCoins(ctx, minttypes.ModuleName, initCoins))

	suite.
		Require().
		NoError(keeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, holderAcc.GetAddress(), initCoins))

	authKeeper.SetModuleAccount(ctx, holderAcc)
	authKeeper.SetModuleAccount(ctx, burnerAcc)
	authKeeper.SetAccount(ctx, baseAcc)

	suite.Require().Panics(func() {
		_ = keeper.SendCoinsFromModuleToModule(ctx, "", holderAcc.GetName(), initCoins) // nolint:errcheck
	})

	suite.Require().Panics(func() {
		_ = keeper.SendCoinsFromModuleToModule(ctx, authtypes.Burner, "", initCoins) // nolint:errcheck
	})

	suite.Require().Panics(func() {
		_ = keeper.SendCoinsFromModuleToAccount(ctx, "", baseAcc.GetAddress(), initCoins) // nolint:errcheck
	})

	suite.Require().Error(
		keeper.SendCoinsFromModuleToAccount(ctx, holderAcc.GetName(), baseAcc.GetAddress(), initCoins.Add(initCoins...)),
	)

	suite.Require().NoError(
		keeper.SendCoinsFromModuleToModule(ctx, holderAcc.GetName(), authtypes.Burner, initCoins),
	)
}

func (suite *IntegrationTestSuite) TestSendCoinsNewAccount() {
	app, ctx := suite.app, suite.ctx
	balances := sdk.NewCoins(newFooCoin(100), newBarCoin(50))

	addr1 := sdk.AccAddress([]byte("addr1_______________"))
	acc1 := app.AccountKeeper.NewAccountWithAddress(ctx, addr1)
	app.AccountKeeper.SetAccount(ctx, acc1)
	suite.Require().NoError(apptesting.FundAccount(app.GigaBankKeeper, ctx, addr1, balances))

	acc1BalanceFoo := app.GigaBankKeeper.GetBalance(ctx, addr1, "foo")
	suite.Require().Equal(sdk.NewInt(100), acc1BalanceFoo.Amount)
	acc1BalanceBar := app.GigaBankKeeper.GetBalance(ctx, addr1, "bar")
	suite.Require().Equal(sdk.NewInt(50), acc1BalanceBar.Amount)

	addr2 := sdk.AccAddress([]byte("addr2_______________"))

	suite.Require().Nil(app.AccountKeeper.GetAccount(ctx, addr2))
	acc2BalanceFoo := app.GigaBankKeeper.GetBalance(ctx, addr2, "foo")
	suite.Require().Equal(sdk.NewInt(0), acc2BalanceFoo.Amount)
	acc2BalanceBar := app.GigaBankKeeper.GetBalance(ctx, addr2, "bar")
	suite.Require().Equal(sdk.NewInt(0), acc2BalanceBar.Amount)
}

func (suite *IntegrationTestSuite) TestInputOutputCoins() {
	app, ctx := suite.app, suite.ctx
	balances := sdk.NewCoins(newFooCoin(90), newBarCoin(30))

	addr1 := sdk.AccAddress([]byte("addr1_______________"))
	acc1 := app.AccountKeeper.NewAccountWithAddress(ctx, addr1)
	app.AccountKeeper.SetAccount(ctx, acc1)

	addr2 := sdk.AccAddress([]byte("addr2_______________"))
	acc2 := app.AccountKeeper.NewAccountWithAddress(ctx, addr2)
	app.AccountKeeper.SetAccount(ctx, acc2)

	addr3 := sdk.AccAddress([]byte("addr3_______________"))
	acc3 := app.AccountKeeper.NewAccountWithAddress(ctx, addr3)
	app.AccountKeeper.SetAccount(ctx, acc3)

	inputs := []types.Input{
		{Address: addr1.String(), Coins: sdk.NewCoins(newFooCoin(30), newBarCoin(10))},
		{Address: addr1.String(), Coins: sdk.NewCoins(newFooCoin(30), newBarCoin(10))},
	}
	outputs := []types.Output{
		{Address: addr2.String(), Coins: sdk.NewCoins(newFooCoin(30), newBarCoin(10))},
		{Address: addr3.String(), Coins: sdk.NewCoins(newFooCoin(30), newBarCoin(10))},
	}

	suite.Require().Error(app.GigaBankKeeper.InputOutputCoins(ctx, inputs, []types.Output{}))
	suite.Require().Error(app.GigaBankKeeper.InputOutputCoins(ctx, inputs, outputs))

	suite.Require().NoError(apptesting.FundAccount(app.GigaBankKeeper, ctx, addr1, balances))

	insufficientInputs := []types.Input{
		{Address: addr1.String(), Coins: sdk.NewCoins(newFooCoin(300), newBarCoin(100))},
		{Address: addr1.String(), Coins: sdk.NewCoins(newFooCoin(300), newBarCoin(100))},
	}
	insufficientOutputs := []types.Output{
		{Address: addr2.String(), Coins: sdk.NewCoins(newFooCoin(300), newBarCoin(100))},
		{Address: addr3.String(), Coins: sdk.NewCoins(newFooCoin(300), newBarCoin(100))},
	}
	suite.Require().Error(app.GigaBankKeeper.InputOutputCoins(ctx, insufficientInputs, insufficientOutputs))
	suite.Require().NoError(app.GigaBankKeeper.InputOutputCoins(ctx, inputs, outputs))
}

func (suite *IntegrationTestSuite) TestSendCoins() {
	app, ctx := suite.app, suite.ctx
	balances := sdk.NewCoins(newFooCoin(100), newBarCoin(50))

	addr1 := sdk.AccAddress("addr1_______________")
	acc1 := app.AccountKeeper.NewAccountWithAddress(ctx, addr1)
	app.AccountKeeper.SetAccount(ctx, acc1)

	addr2 := sdk.AccAddress("addr2_______________")
	acc2 := app.AccountKeeper.NewAccountWithAddress(ctx, addr2)
	app.AccountKeeper.SetAccount(ctx, acc2)
	suite.Require().NoError(apptesting.FundAccount(app.GigaBankKeeper, ctx, addr2, balances))

	sendAmt := sdk.NewCoins(newFooCoin(50), newBarCoin(25))
	suite.Require().Error(app.GigaBankKeeper.SendCoins(ctx, addr1, addr2, sendAmt))

	suite.Require().NoError(apptesting.FundAccount(app.GigaBankKeeper, ctx, addr1, balances))
	suite.Require().NoError(app.GigaBankKeeper.SendCoins(ctx, addr1, addr2, sendAmt))
}

func (suite *IntegrationTestSuite) TestSendCoinsWithAllowList() {
	app, ctx := suite.app, suite.ctx
	addr1 := sdk.AccAddress("addr1_______________")
	acc1 := app.AccountKeeper.NewAccountWithAddress(ctx, addr1)
	app.AccountKeeper.SetAccount(ctx, acc1)
	factoryCoin := newFactoryFooCoin(addr1, 100)
	balances := sdk.NewCoins(factoryCoin, newBarCoin(50))
	suite.Require().NoError(apptesting.FundAccount(app.GigaBankKeeper, ctx, addr1, balances))

	addr2 := sdk.AccAddress("addr2_______________")
	acc2 := app.AccountKeeper.NewAccountWithAddress(ctx, addr2)
	app.AccountKeeper.SetAccount(ctx, acc2)

	app.GigaBankKeeper.SetDenomAllowList(ctx, factoryCoin.Denom,
		types.AllowList{Addresses: []string{addr1.String(), addr2.String()}})

	sendAmt := sdk.NewCoins(newFactoryFooCoin(addr1, 40), newBarCoin(25))
	suite.Require().NoError(app.GigaBankKeeper.SendCoins(ctx, addr1, addr2, sendAmt))
}

func (suite *IntegrationTestSuite) TestSendCoinsModuleToAccount() {
	// add module accounts to supply keeper
	ctx := suite.ctx
	authKeeper, keeper := suite.initKeepersWithmAccPerms(make(map[string]bool))
	authKeeper.SetModuleAccount(ctx, multiPermAcc)
	app := suite.app
	app.GigaBankKeeper = &keeper

	addr1 := sdk.AccAddress("addr1_______________")
	acc1 := authKeeper.NewAccountWithAddress(ctx, addr1)
	authKeeper.SetAccount(ctx, acc1)

	bankBalances := sdk.NewCoins(newFooCoin(20), newBarCoin(30))
	// set up bank balances
	suite.Require().NoError(apptesting.FundAccount(app.GigaBankKeeper, ctx, multiPermAcc.GetAddress(), bankBalances))

	// send foo with spillover, bar fully backed by deferred balances - no error
	sendCoins := sdk.NewCoins(newFooCoin(20), newBarCoin(20))
	// perform send from module to account
	suite.Require().NoError(app.GigaBankKeeper.SendCoinsFromModuleToAccount(ctx, multiPerm, addr1, sendCoins))
	expectedBankBalances := sdk.NewCoins(newBarCoin(10))
	// assert module balances correct
	bals := app.GigaBankKeeper.GetBalance(ctx, multiPermAcc.GetAddress(), "bar")
	suite.Require().Equal(expectedBankBalances[0], bals)
	// assert receiver balances correct
	userBals := app.GigaBankKeeper.GetBalance(ctx, addr1, "bar")
	suite.Require().Equal(newBarCoin(20), userBals)
}

func (suite *IntegrationTestSuite) TestSendEnabled() {
	app, ctx := suite.app, suite.ctx
	enabled := true
	params := cosmosbanktypes.DefaultParams()
	suite.Require().Equal(enabled, params.DefaultSendEnabled)

	app.GigaBankKeeper.SetParams(ctx, params)

	bondCoin := sdk.NewCoin(sdk.DefaultBondDenom, sdk.OneInt())
	fooCoin := sdk.NewCoin("foocoin", sdk.OneInt())
	barCoin := sdk.NewCoin("barcoin", sdk.OneInt())

	// assert with default (all denom) send enabled both Bar and Bond Denom are enabled
	suite.Require().Equal(enabled, app.GigaBankKeeper.IsSendEnabledCoin(ctx, barCoin))
	suite.Require().Equal(enabled, app.GigaBankKeeper.IsSendEnabledCoin(ctx, bondCoin))

	// Both coins should be send enabled.
	err := app.GigaBankKeeper.IsSendEnabledCoins(ctx, fooCoin, bondCoin)
	suite.Require().NoError(err)

	// Set default send_enabled to !enabled, add a foodenom that overrides default as enabled
	params.DefaultSendEnabled = !enabled
	params = params.SetSendEnabledParam(fooCoin.Denom, enabled)
	app.GigaBankKeeper.SetParams(ctx, params)

	// Expect our specific override to be enabled, others to be !enabled.
	suite.Require().Equal(enabled, app.GigaBankKeeper.IsSendEnabledCoin(ctx, fooCoin))
	suite.Require().Equal(!enabled, app.GigaBankKeeper.IsSendEnabledCoin(ctx, barCoin))
	suite.Require().Equal(!enabled, app.GigaBankKeeper.IsSendEnabledCoin(ctx, bondCoin))

	// Foo coin should be send enabled.
	err = app.GigaBankKeeper.IsSendEnabledCoins(ctx, fooCoin)
	suite.Require().NoError(err)

	// Expect an error when one coin is not send enabled.
	err = app.GigaBankKeeper.IsSendEnabledCoins(ctx, fooCoin, bondCoin)
	suite.Require().Error(err)

	// Expect an error when all coins are not send enabled.
	err = app.GigaBankKeeper.IsSendEnabledCoins(ctx, bondCoin, barCoin)
	suite.Require().Error(err)
}

func (suite *IntegrationTestSuite) TestHasBalance() {
	app, ctx := suite.app, suite.ctx
	addr := sdk.AccAddress([]byte("addr1_______________"))

	acc := app.AccountKeeper.NewAccountWithAddress(ctx, addr)
	app.AccountKeeper.SetAccount(ctx, acc)

	balances := sdk.NewCoins(newFooCoin(100))
	suite.Require().False(app.GigaBankKeeper.HasBalance(ctx, addr, newFooCoin(99)))

	suite.Require().NoError(apptesting.FundAccount(app.GigaBankKeeper, ctx, addr, balances))
	suite.Require().False(app.GigaBankKeeper.HasBalance(ctx, addr, newFooCoin(101)))
	suite.Require().True(app.GigaBankKeeper.HasBalance(ctx, addr, newFooCoin(100)))
	suite.Require().True(app.GigaBankKeeper.HasBalance(ctx, addr, newFooCoin(1)))
}

func (suite *IntegrationTestSuite) TestSpendableCoins() {
	app, ctx := suite.app, suite.ctx
	now := tmtime.Now()
	ctx = ctx.WithBlockHeader(tmproto.Header{Time: now})
	endTime := now.Add(24 * time.Hour)

	origCoins := sdk.NewCoins(sdk.NewInt64Coin("usei", 100))
	delCoins := sdk.NewCoins(sdk.NewInt64Coin("usei", 50))

	addr1 := sdk.AccAddress([]byte("addr1_______________"))
	addr2 := sdk.AccAddress([]byte("addr2_______________"))
	addrModule := sdk.AccAddress([]byte("moduleAcc___________"))

	macc := app.AccountKeeper.NewAccountWithAddress(ctx, addrModule)
	bacc := authtypes.NewBaseAccountWithAddress(addr1)
	vacc := vesting.NewContinuousVestingAccount(bacc, origCoins, ctx.BlockHeader().Time.Unix(), endTime.Unix(), nil)
	acc := app.AccountKeeper.NewAccountWithAddress(ctx, addr2)

	app.AccountKeeper.SetAccount(ctx, macc)
	app.AccountKeeper.SetAccount(ctx, vacc)
	app.AccountKeeper.SetAccount(ctx, acc)
	suite.Require().NoError(apptesting.FundAccount(app.GigaBankKeeper, ctx, addr1, origCoins))
	suite.Require().NoError(apptesting.FundAccount(app.GigaBankKeeper, ctx, addr2, origCoins))

	suite.Require().Equal(origCoins, app.GigaBankKeeper.SpendableCoins(ctx, addr2))

	ctx = ctx.WithBlockTime(now.Add(12 * time.Hour))
	suite.Require().NoError(app.GigaBankKeeper.DelegateCoins(ctx, addr2, addrModule, delCoins))
	suite.Require().Equal(origCoins.Sub(delCoins), app.GigaBankKeeper.SpendableCoins(ctx, addr1))
}

func (suite *IntegrationTestSuite) TestVestingAccountSend() {
	app, ctx := suite.app, suite.ctx
	now := tmtime.Now()
	ctx = ctx.WithBlockHeader(tmproto.Header{Time: now})
	endTime := now.Add(24 * time.Hour)

	origCoins := sdk.NewCoins(sdk.NewInt64Coin("usei", 100))
	sendCoins := sdk.NewCoins(sdk.NewInt64Coin("usei", 50))

	addr1 := sdk.AccAddress([]byte("addr1_______________"))
	addr2 := sdk.AccAddress([]byte("addr2_______________"))

	bacc := authtypes.NewBaseAccountWithAddress(addr1)
	vacc := vesting.NewContinuousVestingAccount(bacc, origCoins, now.Unix(), endTime.Unix(), nil)

	app.AccountKeeper.SetAccount(ctx, vacc)
	suite.Require().NoError(apptesting.FundAccount(app.GigaBankKeeper, ctx, addr1, origCoins))

	// require that no coins be sendable at the beginning of the vesting schedule
	suite.Require().Error(app.GigaBankKeeper.SendCoins(ctx, addr1, addr2, sendCoins))

	// receive some coins
	suite.Require().NoError(apptesting.FundAccount(app.GigaBankKeeper, ctx, addr1, sendCoins))
	// require that all vested coins are spendable plus any received
	ctx = ctx.WithBlockTime(now.Add(12 * time.Hour))
	suite.Require().NoError(app.GigaBankKeeper.SendCoins(ctx, addr1, addr2, sendCoins))
}

func (suite *IntegrationTestSuite) TestPeriodicVestingAccountSend() {
	app, ctx := suite.app, suite.ctx
	now := tmtime.Now()
	ctx = ctx.WithBlockHeader(tmproto.Header{Time: now})
	origCoins := sdk.NewCoins(sdk.NewInt64Coin("usei", 100))
	sendCoins := sdk.NewCoins(sdk.NewInt64Coin("usei", 50))

	addr1 := sdk.AccAddress([]byte("addr1_______________"))
	addr2 := sdk.AccAddress([]byte("addr2_______________"))
	periods := vesting.Periods{
		vesting.Period{Length: int64(12 * 60 * 60), Amount: sdk.Coins{sdk.NewInt64Coin("usei", 50)}},
		vesting.Period{Length: int64(6 * 60 * 60), Amount: sdk.Coins{sdk.NewInt64Coin("usei", 25)}},
		vesting.Period{Length: int64(6 * 60 * 60), Amount: sdk.Coins{sdk.NewInt64Coin("usei", 25)}},
	}

	bacc := authtypes.NewBaseAccountWithAddress(addr1)
	vacc := vesting.NewPeriodicVestingAccount(bacc, origCoins, ctx.BlockHeader().Time.Unix(), periods, nil)

	app.AccountKeeper.SetAccount(ctx, vacc)
	suite.Require().NoError(apptesting.FundAccount(app.GigaBankKeeper, ctx, addr1, origCoins))

	// require that no coins be sendable at the beginning of the vesting schedule
	suite.Require().Error(app.GigaBankKeeper.SendCoins(ctx, addr1, addr2, sendCoins))

	// receive some coins
	suite.Require().NoError(apptesting.FundAccount(app.GigaBankKeeper, ctx, addr1, sendCoins))

	// require that all vested coins are spendable plus any received
	ctx = ctx.WithBlockTime(now.Add(12 * time.Hour))
	suite.Require().NoError(app.GigaBankKeeper.SendCoins(ctx, addr1, addr2, sendCoins))
}

func (suite *IntegrationTestSuite) TestSetDenomMetaData() {
	app, ctx := suite.app, suite.ctx

	metadata := suite.getTestMetadata()

	for i := range []int{1, 2} {
		app.GigaBankKeeper.SetDenomMetaData(ctx, metadata[i])
	}

	actualMetadata, found := app.GigaBankKeeper.GetDenomMetaData(ctx, metadata[1].Base)
	suite.Require().True(found)
	suite.Require().Equal(metadata[1].GetBase(), actualMetadata.GetBase())
	suite.Require().Equal(metadata[1].GetDisplay(), actualMetadata.GetDisplay())
	suite.Require().Equal(metadata[1].GetDescription(), actualMetadata.GetDescription())
	suite.Require().Equal(metadata[1].GetDenomUnits()[1].GetDenom(), actualMetadata.GetDenomUnits()[1].GetDenom())
	suite.Require().Equal(metadata[1].GetDenomUnits()[1].GetExponent(), actualMetadata.GetDenomUnits()[1].GetExponent())
	suite.Require().Equal(metadata[1].GetDenomUnits()[1].GetAliases(), actualMetadata.GetDenomUnits()[1].GetAliases())
}

func (suite *IntegrationTestSuite) TestSetAllowList() {
	app, ctx := suite.app, suite.ctx

	allowList := types.AllowList{Addresses: []string{"addr1", "addr2"}}
	ownerAddress := sdk.AccAddress("owner")
	denom := "factory/" + ownerAddress.String() + "/Test"

	app.GigaBankKeeper.SetDenomAllowList(ctx, denom, allowList)

	actualAllowList := app.GigaBankKeeper.GetDenomAllowList(ctx, denom)
	suite.Require().Equal(allowList, actualAllowList)
}

func (suite *IntegrationTestSuite) TestBaseKeeper_IsAllowedToSendCoins() {
	type CoinToAllowList struct {
		coin      sdk.Coin
		allowList types.AllowList
	}
	type args struct {
		addr             sdk.AccAddress
		coinsToAllowList []CoinToAllowList
	}
	tests := []struct {
		name      string
		args      args
		isAllowed bool
	}{
		{
			name: "Allowed for for empty coins",
			args: args{
				addr: sdk.AccAddress{},
				coinsToAllowList: []CoinToAllowList{
					{coin: sdk.Coin{}},
				},
			},
			isAllowed: true,
		},
		{
			name: "allowed for to transfer with a non-factory coin",
			args: args{
				addr: sdk.AccAddress("from"),
				coinsToAllowList: []CoinToAllowList{
					{
						coin: sdk.NewInt64Coin("test", 100),
					},
				},
			},
			isAllowed: true,
		},
		{
			name: "allowed with a factory coin with no allow list",
			args: args{
				addr: sdk.AccAddress("from"),
				coinsToAllowList: []CoinToAllowList{
					{
						coin: sdk.NewInt64Coin(
							fmt.Sprintf("factory/%s/test", sdk.AccAddress("from")), 100),
					},
				},
			},
			isAllowed: true,
		},
		{
			name: "allowed with a factory coin with empty allow list",
			args: args{
				addr: sdk.AccAddress("from"),
				coinsToAllowList: []CoinToAllowList{
					{
						coin: sdk.NewInt64Coin(
							fmt.Sprintf("factory/%s/test", sdk.AccAddress("from")), 100),
						allowList: types.AllowList{},
					},
				},
			},
			isAllowed: true,
		},
		{
			name: "not allowed to transfer coins for denom if not in allowlist",
			args: args{
				addr: sdk.AccAddress("from"),
				coinsToAllowList: []CoinToAllowList{
					{
						coin: sdk.NewInt64Coin(fmt.Sprintf("factory/%s/test", sdk.AccAddress("from")), 100),
						allowList: types.AllowList{
							Addresses: []string{sdk.AccAddress("other").String()},
						},
					},
				},
			},
			isAllowed: false,
		},
		{
			name: "allowed to transfer for denom",
			args: args{
				addr: sdk.AccAddress("from"),
				coinsToAllowList: []CoinToAllowList{
					{
						coin: sdk.NewInt64Coin(fmt.Sprintf("factory/%s/test", sdk.AccAddress("from")), 100),
						allowList: types.AllowList{
							Addresses: []string{sdk.AccAddress("from").String(), sdk.AccAddress("to").String()},
						},
					},
				},
			},
			isAllowed: true,
		},
		{
			name: "allowed for one coin but not allowed for another coin",
			args: args{
				addr: sdk.AccAddress("from"),
				coinsToAllowList: []CoinToAllowList{
					{
						coin: sdk.NewInt64Coin(fmt.Sprintf("factory/%s/test", sdk.AccAddress("from")), 100),
						allowList: types.AllowList{
							Addresses: []string{sdk.AccAddress("from").String(), sdk.AccAddress("to").String()},
						},
					},
					{
						coin: sdk.NewInt64Coin(fmt.Sprintf("factory/%s/test2", sdk.AccAddress("from")), 100),
						allowList: types.AllowList{
							Addresses: []string{sdk.AccAddress("other").String(), sdk.AccAddress("yetanother").String()},
						},
					},
				},
			},
			isAllowed: false,
		},
		{
			name: "not allowed for first coin but allowed for another coin",
			args: args{
				addr: sdk.AccAddress("from"),
				coinsToAllowList: []CoinToAllowList{
					{
						coin: sdk.NewInt64Coin(fmt.Sprintf("factory/%s/test", sdk.AccAddress("from")), 100),
						allowList: types.AllowList{
							Addresses: []string{sdk.AccAddress("other").String(), sdk.AccAddress("yetanother").String()},
						},
					},
					{
						coin: sdk.NewInt64Coin(fmt.Sprintf("factory/%s/test2", sdk.AccAddress("from")), 100),
						allowList: types.AllowList{
							Addresses: []string{sdk.AccAddress("from").String(), sdk.AccAddress("to").String()},
						},
					},
				},
			},
			isAllowed: false,
		},
	}

	for _, tt := range tests {
		suite.Run(tt.name, func() {
			app, ctx := suite.app, suite.ctx
			coins := sdk.NewCoins()
			for _, coinToAllowList := range tt.args.coinsToAllowList {
				if coinToAllowList.coin.Denom != "" {
					coins = coins.Add(coinToAllowList.coin)
					if coinToAllowList.allowList.Addresses != nil {
						app.GigaBankKeeper.SetDenomAllowList(ctx, coinToAllowList.coin.Denom, coinToAllowList.allowList)
					}
				}
			}
			denomToAllowedAddressesCache := make(map[string]keeper.AllowedAddresses)
			isAllowed :=
				app.GigaBankKeeper.IsInDenomAllowList(ctx, tt.args.addr, coins, denomToAllowedAddressesCache)

			// Use suite.Require to assert the results
			suite.Require().Equal(tt.isAllowed, isAllowed,
				fmt.Errorf("IsInDenomAllowList() isAllowed = %v, want %v",
					isAllowed, tt.isAllowed))
		})
	}
}

func (suite *IntegrationTestSuite) TestCanSendTo() {
	app, ctx := suite.app, suite.ctx
	badAddr := sdk.AccAddress([]byte("addr1_______________"))
	goodAddr := sdk.AccAddress([]byte("addr2_______________"))
	sourceAddr := sdk.AccAddress([]byte("addr3_______________"))
	app.AccountKeeper.SetAccount(ctx, app.AccountKeeper.NewAccountWithAddress(ctx, badAddr))
	app.AccountKeeper.SetAccount(ctx, app.AccountKeeper.NewAccountWithAddress(ctx, goodAddr))
	app.AccountKeeper.SetAccount(ctx, app.AccountKeeper.NewAccountWithAddress(ctx, sourceAddr))
	suite.Require().NoError(apptesting.FundAccount(app.GigaBankKeeper, ctx, sourceAddr, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100)))))
	checker := func(_ sdk.Context, addr sdk.AccAddress) bool { return !addr.Equals(badAddr) }
	app.GigaBankKeeper.RegisterRecipientChecker(checker)
	amt := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10)))
	suite.Require().Nil(app.GigaBankKeeper.SendCoins(ctx, sourceAddr, goodAddr, amt))
	suite.Require().NotNil(app.GigaBankKeeper.SendCoins(ctx, sourceAddr, badAddr, amt))
	suite.Require().Nil(app.GigaBankKeeper.SendCoinsAndWei(ctx, sourceAddr, goodAddr, sdk.OneInt(), sdk.ZeroInt()))
	suite.Require().NotNil(app.GigaBankKeeper.SendCoinsAndWei(ctx, sourceAddr, badAddr, sdk.OneInt(), sdk.ZeroInt()))
}

func (suite *IntegrationTestSuite) getTestMetadata() []types.Metadata {
	return []types.Metadata{{
		Name:        "Cosmos Hub Atom",
		Symbol:      "ATOM",
		Description: "The native staking token of the Cosmos Hub.",
		DenomUnits: []*types.DenomUnit{
			{Denom: "uatom", Exponent: uint32(0), Aliases: []string{"microatom"}},
			{Denom: "matom", Exponent: uint32(3), Aliases: []string{"milliatom"}},
			{Denom: "atom", Exponent: uint32(6), Aliases: nil},
		},
		Base:    "uatom",
		Display: "atom",
	},
		{
			Name:        "Token",
			Symbol:      "TOKEN",
			Description: "The native staking token of the Token Hub.",
			DenomUnits: []*types.DenomUnit{
				{Denom: "1token", Exponent: uint32(5), Aliases: []string{"decitoken"}},
				{Denom: "2token", Exponent: uint32(4), Aliases: []string{"centitoken"}},
				{Denom: "3token", Exponent: uint32(7), Aliases: []string{"dekatoken"}},
			},
			Base:    "utoken",
			Display: "token",
		},
	}
}

func (suite *IntegrationTestSuite) TestMintCoinRestrictions() {
	type BankMintingRestrictionFn func(ctx sdk.Context, coins sdk.Coins) error

	maccPerms := app.GetMaccPerms()
	maccPerms[multiPerm] = []string{authtypes.Burner, authtypes.Minter, authtypes.Staking}

	suite.app.AccountKeeper = authkeeper.NewAccountKeeper(
		suite.app.AppCodec(), suite.app.GetKey(authtypes.StoreKey), suite.app.GetSubspace(authtypes.ModuleName),
		authtypes.ProtoBaseAccount, maccPerms,
	)
	suite.app.AccountKeeper.SetModuleAccount(suite.ctx, multiPermAcc)

	type testCase struct {
		coinsToTry sdk.Coin
		expectPass bool
	}

	tests := []struct {
		name          string
		restrictionFn BankMintingRestrictionFn
		testCases     []testCase
	}{
		{
			"restriction",
			func(ctx sdk.Context, coins sdk.Coins) error {
				for _, coin := range coins {
					if coin.Denom != fooDenom {
						return fmt.Errorf("Module %s only has perms for minting %s coins, tried minting %s coins", types.ModuleName, fooDenom, coin.Denom)
					}
				}
				return nil
			},
			[]testCase{
				{
					coinsToTry: newFooCoin(100),
					expectPass: true,
				},
				{
					coinsToTry: newBarCoin(100),
					expectPass: false,
				},
			},
		},
	}

	for _, test := range tests {
		bk := keeper.NewBaseKeeperWithDeferredCache(suite.app.AppCodec(), suite.app.GetKey(types.StoreKey),
			suite.app.AccountKeeper, suite.app.GetSubspace(types.ModuleName), nil, suite.app.GetKey(types.DeferredCacheStoreKey)).WithMintCoinsRestriction(keeper.MintingRestrictionFn(test.restrictionFn))
		suite.app.GigaBankKeeper = &bk
		for _, testCase := range test.testCases {
			if testCase.expectPass {
				suite.Require().NoError(
					suite.app.GigaBankKeeper.MintCoins(
						suite.ctx,
						multiPermAcc.Name,
						sdk.NewCoins(testCase.coinsToTry),
					),
				)
			} else {
				suite.Require().Error(
					suite.app.GigaBankKeeper.MintCoins(
						suite.ctx,
						multiPermAcc.Name,
						sdk.NewCoins(testCase.coinsToTry),
					),
				)
			}
		}
	}
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(IntegrationTestSuite))
}

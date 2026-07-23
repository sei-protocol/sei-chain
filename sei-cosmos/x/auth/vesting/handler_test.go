package vesting_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/apptesting"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/suite"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/vesting"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/vesting/types"
)

type HandlerTestSuite struct {
	suite.Suite

	handler sdk.Handler
	app     *app.App
}

func (suite *HandlerTestSuite) SetupTest() {
	checkTx := false
	app := app.Setup(suite.T(), checkTx, false, false)

	suite.handler = vesting.NewHandler(app.AccountKeeper, app.BankKeeper, app.UpgradeKeeper)
	suite.app = app
}

func (suite *HandlerTestSuite) fundedContext(chainID string) (sdk.Context, sdk.AccAddress, sdk.Coins) {
	ctx := suite.app.BaseApp.NewContext(false, tmproto.Header{Height: suite.app.LastBlockHeight() + 1, ChainID: chainID})

	balances := sdk.NewCoins(sdk.NewInt64Coin("test", 1000))
	funder := sdk.AccAddress([]byte("addr1_______________"))
	acc := suite.app.AccountKeeper.NewAccountWithAddress(ctx, funder)
	suite.app.AccountKeeper.SetAccount(ctx, acc)
	suite.Require().NoError(apptesting.FundAccount(suite.app.BankKeeper, ctx, funder, balances))

	return ctx, funder, balances
}

// TestMsgCreateVestingAccountDeprecated verifies that on chains without
// pre-deprecation history (any chain-id outside the allowlist) the deprecated
// vesting module rejects account creation without mutating any state.
func (suite *HandlerTestSuite) TestMsgCreateVestingAccountDeprecated() {
	ctx, addr1, balances := suite.fundedContext("")
	addr2 := sdk.AccAddress([]byte("addr2_______________"))

	testCases := []struct {
		name string
		msg  *types.MsgCreateVestingAccount
	}{
		{
			name: "delayed vesting account",
			msg:  types.NewMsgCreateVestingAccount(addr1, addr2, sdk.NewCoins(sdk.NewInt64Coin("test", 100)), ctx.BlockTime().Unix()+10000, true, nil),
		},
		{
			name: "continuous vesting account",
			msg:  types.NewMsgCreateVestingAccount(addr1, addr2, sdk.NewCoins(sdk.NewInt64Coin("test", 100)), ctx.BlockTime().Unix()+10000, false, nil),
		},
		{
			name: "delayed vesting account with admin",
			msg:  types.NewMsgCreateVestingAccount(addr1, addr2, sdk.NewCoins(sdk.NewInt64Coin("test", 100)), ctx.BlockTime().Unix()+10000, true, addr1),
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			res, err := suite.handler(ctx, tc.msg)
			suite.Require().ErrorIs(err, types.ErrVestingDeprecated)
			suite.Require().Nil(res)

			// no account is created and no funds move
			suite.Require().Nil(suite.app.AccountKeeper.GetAccount(ctx, addr2))
			suite.Require().Equal(balances, suite.app.BankKeeper.GetAllBalances(ctx, addr1))
		})
	}
}

// TestMsgCreateVestingAccountPostUpgrade verifies that on chains with
// pre-deprecation history the gate activates once the deprecation upgrade has
// executed.
func (suite *HandlerTestSuite) TestMsgCreateVestingAccountPostUpgrade() {
	ctx, addr1, balances := suite.fundedContext("pacific-1")
	addr2 := sdk.AccAddress([]byte("addr2_______________"))

	suite.app.UpgradeKeeper.SetDone(ctx, vesting.DeprecationUpgradeName)

	msg := types.NewMsgCreateVestingAccount(addr1, addr2, sdk.NewCoins(sdk.NewInt64Coin("test", 100)), ctx.BlockTime().Unix()+10000, true, nil)
	res, err := suite.handler(ctx, msg)
	suite.Require().ErrorIs(err, types.ErrVestingDeprecated)
	suite.Require().Nil(res)

	// no account is created and no funds move
	suite.Require().Nil(suite.app.AccountKeeper.GetAccount(ctx, addr2))
	suite.Require().Equal(balances, suite.app.BankKeeper.GetAllBalances(ctx, addr1))
}

// TestMsgCreateVestingAccountPreUpgradeHistory verifies that on chains with
// pre-deprecation history the original behavior is preserved below the
// deprecation upgrade height, so replaying historical blocks yields identical
// state.
func (suite *HandlerTestSuite) TestMsgCreateVestingAccountPreUpgradeHistory() {
	ctx, addr1, _ := suite.fundedContext("pacific-1")

	addr2 := sdk.AccAddress([]byte("addr2_______________"))
	addr3 := sdk.AccAddress([]byte("addr3_______________"))
	addr4 := sdk.AccAddress([]byte("addr4_______________"))
	addr5 := sdk.AccAddress([]byte("addr5_______________"))
	addr6 := sdk.AccAddress([]byte("addr6_______________"))
	addr7 := sdk.AccAddress([]byte("addr7_______________"))
	addr8 := sdk.AccAddress([]byte("addr8_______________"))

	balances := sdk.NewCoins(sdk.NewInt64Coin("test", 1000))
	acc4 := suite.app.AccountKeeper.NewAccountWithAddress(ctx, addr4)
	suite.app.AccountKeeper.SetAccount(ctx, acc4)
	suite.Require().NoError(apptesting.FundAccount(suite.app.BankKeeper, ctx, addr4, balances))

	testCases := []struct {
		name      string
		msg       *types.MsgCreateVestingAccount
		expectErr bool
	}{
		{
			name:      "create delayed vesting account",
			msg:       types.NewMsgCreateVestingAccount(addr1, addr2, sdk.NewCoins(sdk.NewInt64Coin("test", 100)), ctx.BlockTime().Unix()+10000, true, nil),
			expectErr: false,
		},
		{
			name:      "create continuous vesting account",
			msg:       types.NewMsgCreateVestingAccount(addr1, addr3, sdk.NewCoins(sdk.NewInt64Coin("test", 100)), ctx.BlockTime().Unix()+10000, false, nil),
			expectErr: false,
		},
		{
			name:      "create delayed vesting account with admin",
			msg:       types.NewMsgCreateVestingAccount(addr1, addr5, sdk.NewCoins(sdk.NewInt64Coin("test", 100)), ctx.BlockTime().Unix()+10000, true, addr4),
			expectErr: false,
		},
		{
			name:      "create continuous vesting account with admin",
			msg:       types.NewMsgCreateVestingAccount(addr1, addr6, sdk.NewCoins(sdk.NewInt64Coin("test", 100)), ctx.BlockTime().Unix()+10000, false, addr4),
			expectErr: false,
		},
		{
			name:      "create continuous vesting account with non-existing admin",
			msg:       types.NewMsgCreateVestingAccount(addr1, addr7, sdk.NewCoins(sdk.NewInt64Coin("test", 100)), ctx.BlockTime().Unix()+10000, false, addr8),
			expectErr: true,
		},
		{
			name:      "continuous vesting account already exists",
			msg:       types.NewMsgCreateVestingAccount(addr1, addr3, sdk.NewCoins(sdk.NewInt64Coin("test", 100)), ctx.BlockTime().Unix()+10000, false, nil),
			expectErr: true,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			res, err := suite.handler(ctx, tc.msg)
			if tc.expectErr {
				suite.Require().Error(err)
			} else {
				suite.Require().NoError(err)
				suite.Require().NotNil(res)

				toAddr, err := sdk.AccAddressFromBech32(tc.msg.ToAddress)
				suite.Require().NoError(err)
				accI := suite.app.AccountKeeper.GetAccount(ctx, toAddr)
				suite.Require().NotNil(accI)

				if tc.msg.Delayed {
					acc, ok := accI.(*types.DelayedVestingAccount)
					suite.Require().True(ok)
					suite.Require().Equal(tc.msg.Amount, acc.GetVestingCoins(ctx.BlockTime()))
				} else {
					acc, ok := accI.(*types.ContinuousVestingAccount)
					suite.Require().True(ok)
					suite.Require().Equal(tc.msg.Amount, acc.GetVestingCoins(ctx.BlockTime()))
				}
			}
		})
	}
}

// TestExistingVestingAccountsStillSupported verifies that vesting accounts
// already in state remain decodable and keep vesting after the module's
// deprecation.
func (suite *HandlerTestSuite) TestExistingVestingAccountsStillSupported() {
	ctx := suite.app.BaseApp.NewContext(false, tmproto.Header{Height: suite.app.LastBlockHeight() + 1})

	addr := sdk.AccAddress([]byte("vestacct____________"))
	origVesting := sdk.NewCoins(sdk.NewInt64Coin("test", 100))
	endTime := ctx.BlockTime().Unix() + 10000

	baseAcc, ok := suite.app.AccountKeeper.NewAccountWithAddress(ctx, addr).(*authtypes.BaseAccount)
	suite.Require().True(ok)

	vestingAcc := types.NewDelayedVestingAccountRaw(types.NewBaseVestingAccount(baseAcc, origVesting, endTime, nil))
	suite.app.AccountKeeper.SetAccount(ctx, vestingAcc)
	suite.Require().NoError(apptesting.FundAccount(suite.app.BankKeeper, ctx, addr, origVesting))

	accI := suite.app.AccountKeeper.GetAccount(ctx, addr)
	suite.Require().NotNil(accI)

	acc, ok := accI.(*types.DelayedVestingAccount)
	suite.Require().True(ok)
	suite.Require().Equal(origVesting, acc.GetVestingCoins(ctx.BlockTime()))
	suite.Require().True(suite.app.BankKeeper.SpendableCoins(ctx, addr).IsZero())
}

func TestHandlerTestSuite(t *testing.T) {
	suite.Run(t, new(HandlerTestSuite))
}

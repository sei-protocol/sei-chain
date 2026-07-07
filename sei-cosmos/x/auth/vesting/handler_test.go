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

	suite.handler = vesting.NewHandler()
	suite.app = app
}

// TestMsgCreateVestingAccountDeprecated verifies that the deprecated vesting
// module rejects account creation without mutating any state.
func (suite *HandlerTestSuite) TestMsgCreateVestingAccountDeprecated() {
	ctx := suite.app.BaseApp.NewContext(false, tmproto.Header{Height: suite.app.LastBlockHeight() + 1})

	balances := sdk.NewCoins(sdk.NewInt64Coin("test", 1000))
	addr1 := sdk.AccAddress([]byte("addr1_______________"))
	addr2 := sdk.AccAddress([]byte("addr2_______________"))

	acc1 := suite.app.AccountKeeper.NewAccountWithAddress(ctx, addr1)
	suite.app.AccountKeeper.SetAccount(ctx, acc1)
	suite.Require().NoError(apptesting.FundAccount(suite.app.BankKeeper, ctx, addr1, balances))

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
